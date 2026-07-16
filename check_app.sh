#!/usr/bin/env bash
#
# check_app.sh - Exercise the txd-dbos app's HTTP API on localhost.
#
# Checks liveness, kicks off an autoscaling workflow, and polls until the
# workflow reports "done" (or times out). Intended to run while a region
# disruption is active, to prove the workload keeps completing.
#
# Endpoints (see main.go):
#   GET  /                  static page  -> liveness signal
#   GET  /workflow/:taskid  start the autoscaling workflow
#   GET  /progress/:taskid  Progress JSON, phase: checking|replacing|done
#
# Usage:
#   ./check_app.sh                       # one liveness+workflow cycle
#   ./check_app.sh -n 5                  # run 5 cycles
#   ./check_app.sh -u http://host:8080   # target a different host
#   ./check_app.sh -t 120                # per-workflow timeout seconds
#
# Env overrides: APP_URL, WORKFLOW_TIMEOUT, POLL_INTERVAL
set -uo pipefail

BASE_URL="${APP_URL:-http://localhost:8080}"
TIMEOUT="${WORKFLOW_TIMEOUT:-90}"       # max seconds to wait for a workflow
POLL_INTERVAL="${POLL_INTERVAL:-2}"     # seconds between progress polls
CYCLES=1

usage() { grep '^#' "$0" | sed 's/^# \{0,1\}//'; exit "${1:-0}"; }

while getopts ":u:t:n:i:h" opt; do
  case "$opt" in
    u) BASE_URL="$OPTARG" ;;
    t) TIMEOUT="$OPTARG" ;;
    n) CYCLES="$OPTARG" ;;
    i) POLL_INTERVAL="$OPTARG" ;;
    h) usage 0 ;;
    *) echo "unknown option -$OPTARG" >&2; usage 1 ;;
  esac
done

# --- helpers ----------------------------------------------------------------

# curl_code URL -> prints the HTTP status code (000 if unreachable)
curl_code() {
  curl -s -o /dev/null -w '%{http_code}' --max-time 5 "$1"
}

# extract_json KEY < json  -> value of a top-level "KEY":... field
extract_json() {
  sed -n 's/.*"'"$1"'":"\{0,1\}\([^",}]*\)"\{0,1\}.*/\1/p'
}

check_liveness() {
  local code
  code="$(curl_code "$BASE_URL/")"
  if [[ "$code" == "200" ]]; then
    echo "  liveness: OK (GET / -> 200)"
    return 0
  fi
  echo "  liveness: FAIL (GET / -> $code, app unreachable at $BASE_URL)"
  return 1
}

# run one workflow to completion; returns 0 on "done", non-zero otherwise
run_workflow() {
  local task_id start_code body phase progress
  task_id="check-$(date +%s)-$RANDOM"
  echo "  workflow: task_id=$task_id"

  start_code="$(curl_code "$BASE_URL/workflow/$task_id")"
  if [[ "$start_code" != "200" ]]; then
    echo "  workflow: FAIL to start (GET /workflow -> $start_code)"
    return 1
  fi
  echo "  workflow: started, polling /progress (timeout ${TIMEOUT}s)..."

  # Brief grace so the workflow can set its first "checking" event before we
  # poll. Without it the first GET /progress races ahead of SetEvent and the
  # app returns a (harmless) "getEvent timed out: no event found".
  sleep "$POLL_INTERVAL"

  local deadline=$(( $(date +%s) + TIMEOUT ))
  while (( $(date +%s) < deadline )); do
    body="$(curl -s --max-time 5 "$BASE_URL/progress/$task_id")"
    phase="$(printf '%s' "$body" | extract_json phase)"

    case "$phase" in
      done)
        echo "  workflow: DONE  $(printf '%s' "$body")"
        return 0 ;;
      checking|replacing)
        progress="$(printf '%s' "$body" | extract_json currentClient)"
        echo "    phase=$phase ${progress:+client=$progress}" ;;
      *)
        # Event not set yet (workflow just starting) or a transient read error:
        # both are expected during startup, so keep polling.
        if printf '%s' "$body" | grep -q 'no event found\|TimeoutError'; then
          echo "    waiting for first progress event..."
        else
          echo "    waiting... ${body:0:80}"
        fi ;;
    esac
    sleep "$POLL_INTERVAL"
  done

  echo "  workflow: TIMEOUT after ${TIMEOUT}s (last phase='${phase:-none}')"
  return 1
}

# --- main -------------------------------------------------------------------

echo "Target: $BASE_URL"
overall=0
for (( c=1; c<=CYCLES; c++ )); do
  echo "=== cycle $c/$CYCLES ==="
  if ! check_liveness; then
    overall=1
    continue
  fi
  if ! run_workflow; then
    overall=1
  fi
done

echo "==========================="
if [[ "$overall" == 0 ]]; then
  echo "RESULT: PASS - app live and workflow(s) completed"
else
  echo "RESULT: FAIL - see output above"
fi
exit "$overall"
