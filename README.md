# Transaction Director with DBOS

This repo contains a PoC implementation of Transaction Director's control plane using DBOS.

## Autoscaling Workflow

This DBOS workflow periodically checks the health of Transaction Director's clients, decommissioning unhealthy ones and provisioning replacements.

```go
// Main DBOS workflow for autoscaling
func autoscalingWorkflow(clients) {
    // Run the sub-workflow to collect unhealthy clients
    unhealthyClients := dbos.runWorkflow(checkUnhealthyClients, clients)

    // Replace each unhealthy client with a new one
    for client := range unhealthyClients {
        dbos.runWorkflow(replaceClient, client)
    }
}

// Run it on a schedule
dbos.RegisterWorkflow(autoscalingWorkflow,
        dbos.WithSchedule("0 * * * * *")) // Run every minute
```

The `checkUnhealthyClients` sub-workflow queries each client's congestion signal and returns those that are congested:

```go
func checkUnhealthyClients(clients) {
    for client := range clients {
        isCongested := dbos.runStep(getCongestionSignal, client)
        if isCongested {
            unhealthyClients = append(unhealthyClients, client)
        }
    }
    return unhealthyClients
}
```

Congestion signals are fetched in a DBOS step. Steps are ordinary functions — any external interaction should happen inside one:

```go
func getCongestionSignal(client) {
    ...
    return isCongested
}
```

The `replaceClient` workflow decommissions the unhealthy client and brings up a replacement:

```go
func replaceClient(client) {
    dbos.runStep(decommissionClient, client)

    machine := dbos.runStep(startNewMachine, client)

    // Trigger startup script (e.g., build JumpFire client)
    newClient := dbos.runStep(runStartupScript, machine)

    dbos.runStep(registerNewClient, newClient)
}
```
