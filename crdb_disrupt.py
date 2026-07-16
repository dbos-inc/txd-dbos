#!/usr/bin/env python3
"""Disrupt / restore / inspect a CockroachDB Cloud cluster's regions.

Uses the CockroachDB Cloud disruption API to simulate a whole-region failure
against a multi-region cluster, restore it, and list active disruptions. Handy
for validating that a DBOS workload survives a region outage.

Requires only the Python standard library. Auth comes from the CRDB_API_KEY
environment variable (same as the curl examples).

Examples:
    export CRDB_API_KEY=...
    ./crdb_disrupt.py list                 # show active disruptions
    ./crdb_disrupt.py nodes                # list cluster nodes
    ./crdb_disrupt.py disrupt us-east-1    # simulate a full region outage
    ./crdb_disrupt.py restore              # clear all disruptions
"""

import argparse
import json
import os
import sys
import urllib.error
import urllib.request

# The Mastercard/DBOS demo cluster from the runbook. Override with --cluster-id
# or the CRDB_CLUSTER_ID env var.
DEFAULT_CLUSTER_ID = "d4f0a251-c841-4a3e-bb9f-9131e090e508"
BASE_URL = "https://cockroachlabs.cloud/api/v1"


def _request(method, url, api_key, body=None):
    """Make a JSON request and return the decoded response, or raise SystemExit."""
    data = json.dumps(body).encode("utf-8") if body is not None else None
    req = urllib.request.Request(url, data=data, method=method)
    req.add_header("Authorization", f"Bearer {api_key}")
    if data is not None:
        req.add_header("Content-Type", "application/json")
    try:
        with urllib.request.urlopen(req) as resp:
            raw = resp.read().decode("utf-8")
    except urllib.error.HTTPError as e:
        detail = e.read().decode("utf-8", "replace")
        sys.exit(f"error: {method} {url} -> HTTP {e.code} {e.reason}\n{detail}")
    except urllib.error.URLError as e:
        sys.exit(f"error: could not reach {url}: {e.reason}")
    return json.loads(raw) if raw.strip() else {}


def _cluster_url(cluster_id, path):
    return f"{BASE_URL}/clusters/{cluster_id}/{path}"


def _active_specs(cluster_id, api_key):
    """Return the list of active regional disruptor specifications."""
    resp = _request("GET", _cluster_url(cluster_id, "disrupt"), api_key)
    return resp.get("regional_disruptor_specifications", []) or []


def cmd_list(args):
    specs = _active_specs(args.cluster_id, args.api_key)
    if not specs:
        print("No active disruptions. Cluster is in a normal state.")
        return
    print(f"Active disruptions ({len(specs)}):")
    for spec in specs:
        region = spec.get("region_code", "<unknown>")
        scope = "whole region" if spec.get("is_whole_region") else "partial"
        print(f"  - {region} ({scope})")


def cmd_nodes(args):
    resp = _request("GET", _cluster_url(args.cluster_id, "nodes"), args.api_key)
    nodes = resp.get("nodes", resp) if isinstance(resp, dict) else resp
    if not nodes:
        print("No nodes returned.")
        return
    print(f"Nodes ({len(nodes)}):")
    for n in nodes:
        node_id = n.get("node_id", n.get("id", "?"))
        region = n.get("region_code", n.get("region", "?"))
        status = n.get("status", n.get("liveness", "?"))
        print(f"  - node {node_id:>3}  region={region:<14} status={status}")


def cmd_disrupt(args):
    region = args.region
    existing = _active_specs(args.cluster_id, args.api_key)

    # This is a 3-region cluster: never take down more than one region at a time.
    other = [s for s in existing if s.get("region_code") != region]
    if other and not args.force:
        names = ", ".join(s.get("region_code", "?") for s in other)
        sys.exit(
            f"error: {len(other)} other region(s) already disrupted ({names}).\n"
            "Refusing to disrupt a second region on a multi-region cluster.\n"
            "Run 'restore' first, or pass --force to override."
        )
    if any(s.get("region_code") == region for s in existing):
        print(f"Region {region} is already disrupted. Nothing to do.")
        return

    spec = {"is_whole_region": args.whole_region, "region_code": region}
    body = {"regional_disruptor_specifications": [spec]}
    _request("PUT", _cluster_url(args.cluster_id, "disrupt"), args.api_key, body)

    scope = "whole region" if args.whole_region else "partial"
    print(f"Disruption applied: {region} ({scope}).")
    print("Verify node status in the Admin Console, then confirm the workload keeps running.")
    cmd_list(args)


def cmd_restore(args):
    if not _active_specs(args.cluster_id, args.api_key):
        print("No active disruptions; cluster already in a normal state.")
        return
    body = {"regional_disruptor_specifications": []}
    _request("PUT", _cluster_url(args.cluster_id, "disrupt"), args.api_key, body)
    print("Disruptions cleared. Cluster restored to a normal state.")


def build_parser():
    parser = argparse.ArgumentParser(
        description="Disrupt, restore, and inspect a CockroachDB Cloud cluster.",
        formatter_class=argparse.RawDescriptionHelpFormatter,
    )
    parser.add_argument(
        "--cluster-id",
        default=os.environ.get("CRDB_CLUSTER_ID", DEFAULT_CLUSTER_ID),
        help="Cluster UUID (default: $CRDB_CLUSTER_ID or the demo cluster).",
    )
    parser.add_argument(
        "--api-key",
        default=os.environ.get("CRDB_API_KEY"),
        help="Cloud API key (default: $CRDB_API_KEY).",
    )

    sub = parser.add_subparsers(dest="command", required=True)

    sub.add_parser("list", help="List active disruptions.")
    sub.add_parser("nodes", help="List cluster nodes and their status.")
    sub.add_parser("restore", help="Remove all disruptions.")

    p_disrupt = sub.add_parser("disrupt", help="Simulate a region failure.")
    p_disrupt.add_argument("region", help="Region code to disrupt, e.g. us-east-1.")
    p_disrupt.add_argument(
        "--partial",
        dest="whole_region",
        action="store_false",
        help="Disrupt part of the region instead of the whole region.",
    )
    p_disrupt.add_argument(
        "--force",
        action="store_true",
        help="Allow disrupting a region even if another is already disrupted.",
    )
    p_disrupt.set_defaults(whole_region=True)

    return parser


def main(argv=None):
    parser = build_parser()
    args = parser.parse_args(argv)

    if not args.api_key:
        sys.exit("error: no API key. Set CRDB_API_KEY or pass --api-key.")

    handlers = {
        "list": cmd_list,
        "nodes": cmd_nodes,
        "disrupt": cmd_disrupt,
        "restore": cmd_restore,
    }
    handlers[args.command](args)


if __name__ == "__main__":
    main()
