# Transaction Director with DBOS

A small reference implementation of Transaction Director's autoscaling control
plane built on [DBOS Transact (Go)](https://github.com/dbos-inc/dbos-transact-golang).

The app runs a durable scheduled workflow that periodically checks the health
of every managed client, decommissions the unhealthy ones, and provisions
replacements. Workflow state is persisted to Postgres so the loop is
**resilient to crashes and restarts** — interrupted runs resume from the last
completed step.

## Autoscaling Workflow

```go
// Main DBOS workflow for autoscaling. Scheduled to run every minute,
// and also exposed as a manual trigger from the UI.
func autoscalingWorkflow(ctx dbos.DBOSContext, scheduledTime time.Time) (string, error) {
    handle, _ := dbos.RunWorkflow(ctx, checkUnhealthyClients, managedClients)
    unhealthy, _ := handle.GetResult()

    for _, client := range unhealthy {
        h, _ := dbos.RunWorkflow(ctx, replaceClient, client)
        h.GetResult()
    }
    return ..., nil
}

dbos.RegisterWorkflow(dbosCtx, autoscalingWorkflow,
    dbos.WithSchedule("0 * * * * *")) // every minute
```

The `checkUnhealthyClients` sub-workflow queries each client's congestion signal
and returns those that are congested:

```go
func checkUnhealthyClients(ctx dbos.DBOSContext, clients []Client) ([]Client, error) {
    var unhealthy []Client
    for _, c := range clients {
        isCongested, _ := dbos.RunAsStep(ctx, func(stepCtx context.Context) (bool, error) {
            return getCongestionSignal(stepCtx, c)
        })
        if isCongested {
            unhealthy = append(unhealthy, c)
        }
    }
    return unhealthy, nil
}
```

Congestion signals are fetched in a DBOS step. Steps are ordinary Go functions
— any external interaction should happen inside one:

```go
func getCongestionSignal(ctx context.Context, client Client) (bool, error) {
    // ... call out to monitoring system ...
    return isCongested, nil
}
```

The `replaceClient` sub-workflow decommissions the unhealthy client and brings
up a replacement:

```go
func replaceClient(ctx dbos.DBOSContext, client Client) (string, error) {
    dbos.RunAsStep(ctx, func(c context.Context) (string, error)  { return decommissionClient(c, client) })
    machine,   _ := dbos.RunAsStep(ctx, func(c context.Context) (Machine, error) { return startNewMachine(c, client) })
    newClient, _ := dbos.RunAsStep(ctx, func(c context.Context) (Client, error)  { return runStartupScript(c, machine) })
    dbos.RunAsStep(ctx, func(c context.Context) (string, error)  { return registerNewClient(c, newClient) })
    return newClient.ID, nil
}
```

In this demo, the four operations (`getCongestionSignal`, `decommissionClient`,
`startNewMachine`, `runStartupScript`, `registerNewClient`) are all stubs that
sleep briefly and print a line — swap them for real implementations to wire the
loop into your fleet.

## Setup

1. Install dependencies (requires Go ≥ 1.26):
   ```bash
   go mod tidy
   ```

2. Install the DBOS Go CLI and start Postgres in a local Docker container:
   ```bash
   go install github.com/dbos-inc/dbos-transact-golang/cmd/dbos@latest
   dbos postgres start
   ```

3. Point DBOS at the local database:
   ```bash
   export DBOS_SYSTEM_DATABASE_URL="postgres://postgres:dbos@localhost:5432/txd_dbos"
   ```

4. (Optional) Connect this app to [DBOS Conductor](https://docs.dbos.dev/production/self-hosting/conductor)
   for hosted observability and remote control. Grab an API key from the Conductor
   dashboard and export it before launching:
   ```bash
   export DBOS_CONDUCTOR_KEY="<your-conductor-api-key>"
   ```
   When `DBOS_CONDUCTOR_KEY` is unset the app runs fully standalone.

## Running the App

```bash
go run main.go
```

Open `http://localhost:8080` to watch the loop. The workflow runs automatically
every minute on its cron schedule (`0 * * * * *`); you can also trigger an
ad-hoc run from the **Launch the autoscaling loop** button.

To see DBOS recovery in action, hit **Crash the application** while a run is in
flight. Restart the process — the workflow resumes from the last completed step
and finishes the remaining client replacements.
