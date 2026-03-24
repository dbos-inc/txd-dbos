# txd-dbos
Transaction director with DBOS


# Autoscaling workflow

This DBOS workflow periodically check the healthiness of Transcation Director's clients, decommisioning unhealthy clients and provisioning new ones.


```go
for {
    // Check the healthiness of clients
    unhealthyClients := checkHealthiness()

    // Decommission unhealthy clients
    for _, client := range unhealthyClients {
        decommissionClient(client)
    }

    // Provision new clients if needed
    for i := 0; i < len(unhealthyClients); i++ {
        provisionNewClient()
    }
}
```


Checking clients healthiness:

```go
func checkHealthiness() []Client {
    unhealthyClients := []Client{}
    for client := range clients {
        // Use telemetry services to obtain congestion signal
        congestionSignal := getCongestionSignal(client)
        if congestionSignal > isCongested {
            unhealthyClients = append(unhealthyClients, client)
        }
    }
    return unhealthyClients
}
```


Provisioning new clients:

```go
func provisionNewClient() {
    // Start machine
    // Trigger startup script (build JumpFire client, IBM ODM)
}
```

