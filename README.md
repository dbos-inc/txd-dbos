# txd-dbos
Transaction director with DBOS


# Autoscaling workflow

This DBOS workflow periodically check the healthiness of Transcation Director's clients, decommisioning unhealthy clients and provisioning new ones.


```go
@DBOS.worflow()
func autoscalingWorkflow {
    for {
        // Check the healthiness of clients
        unhealthyClients := DBOS.runStep(checkHealthiness)

        // Replace unhealthy clients with new ones
        foreach unhealthyClients {
            DBOS.runStep(decommissionClient)
            DBOS.runStep(provisionNewClient)
        }
    }
}
```


Checking clients healthiness:

```go
@DBOS.step()
func checkHealthiness() {
    foreach clients {
        // Use OpenTelemetry backends to obtain congestion signal
        isCongested := DBOS.runStep(getCongestionSignal)
        if isCongested {
            unhealthyClients = append(unhealthyClients, client)
        }
    }
    return unhealthyClients
}
```


Provisioning new clients:

```go
@DBOS.step()
func provisionNewClient() {
    // Start machine
    // Trigger startup script (build JumpFire client, IBM ODM)
}
```


Obtaining congestion signals from OTLP backend

```go
@DBOS.step()
func getCongestionSignal(client) {
    // Query OTLP backend for congestion signal
    // Return congestion signal value
}
```
