package main

import (
	"context"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"time"

	"github.com/dbos-inc/dbos-transact-golang/dbos"
	"github.com/gin-gonic/gin"
)

const PROGRESS_EVENT = "progress_event"

/*****************************/
/**** TYPES ******************/
/*****************************/

type Client struct {
	ID string `json:"id"`
}

type Machine struct {
	ID string `json:"id"`
}

type Progress struct {
	Phase          string `json:"phase"` // "checking" | "replacing" | "done"
	TotalClients   int    `json:"totalClients"`
	UnhealthyCount int    `json:"unhealthyCount"`
	ReplacedCount  int    `json:"replacedCount"`
	CurrentClient  string `json:"currentClient,omitempty"`
}

var dbosCtx dbos.DBOSContext

// The fleet of clients managed by Transaction Director.
var managedClients = []Client{
	{ID: "client-1"},
	{ID: "client-2"},
	{ID: "client-3"},
	{ID: "client-4"},
	{ID: "client-5"},
}

/*****************************/
/**** WORKFLOWS AND STEPS ****/
/*****************************/

// autoscalingWorkflow is a scheduled workflow that checks the health of every
// managed client, decommissions the unhealthy ones, and provisions replacements.
func autoscalingWorkflow(ctx dbos.DBOSContext, scheduledTime time.Time) (string, error) {
	fmt.Printf("[autoscaling] run started at %s\n", scheduledTime.Format(time.RFC3339))

	if err := dbos.SetEvent(ctx, PROGRESS_EVENT, Progress{
		Phase:        "checking",
		TotalClients: len(managedClients),
	}); err != nil {
		return "", err
	}

	handle, err := dbos.RunWorkflow(ctx, checkUnhealthyClients, managedClients)
	if err != nil {
		return "", err
	}
	unhealthy, err := handle.GetResult()
	if err != nil {
		return "", err
	}

	if err := dbos.SetEvent(ctx, PROGRESS_EVENT, Progress{
		Phase:          "replacing",
		TotalClients:   len(managedClients),
		UnhealthyCount: len(unhealthy),
	}); err != nil {
		return "", err
	}

	for i, client := range unhealthy {
		if err := dbos.SetEvent(ctx, PROGRESS_EVENT, Progress{
			Phase:          "replacing",
			TotalClients:   len(managedClients),
			UnhealthyCount: len(unhealthy),
			ReplacedCount:  i,
			CurrentClient:  client.ID,
		}); err != nil {
			return "", err
		}

		rh, err := dbos.RunWorkflow(ctx, replaceClient, client)
		if err != nil {
			return "", err
		}
		if _, err := rh.GetResult(); err != nil {
			return "", err
		}
	}

	if err := dbos.SetEvent(ctx, PROGRESS_EVENT, Progress{
		Phase:          "done",
		TotalClients:   len(managedClients),
		UnhealthyCount: len(unhealthy),
		ReplacedCount:  len(unhealthy),
	}); err != nil {
		return "", err
	}

	return fmt.Sprintf("replaced %d/%d clients", len(unhealthy), len(managedClients)), nil
}

// checkUnhealthyClients queries each client's congestion signal in a step and
// returns the subset that is congested.
func checkUnhealthyClients(ctx dbos.DBOSContext, clients []Client) ([]Client, error) {
	var unhealthy []Client
	for _, c := range clients {
		client := c
		isCongested, err := dbos.RunAsStep(ctx, func(stepCtx context.Context) (bool, error) {
			return getCongestionSignal(stepCtx, client)
		}, dbos.WithStepName("getCongestionSignal"))
		if err != nil {
			return nil, err
		}
		if isCongested {
			unhealthy = append(unhealthy, client)
		}
	}
	return unhealthy, nil
}

// replaceClient decommissions an unhealthy client and brings up a replacement.
func replaceClient(ctx dbos.DBOSContext, client Client) (string, error) {
	if _, err := dbos.RunAsStep(ctx, func(stepCtx context.Context) (string, error) {
		return decommissionClient(stepCtx, client)
	}, dbos.WithStepName("decommissionClient")); err != nil {
		return "", err
	}

	machine, err := dbos.RunAsStep(ctx, func(stepCtx context.Context) (Machine, error) {
		return startNewMachine(stepCtx, client)
	}, dbos.WithStepName("startNewMachine"))
	if err != nil {
		return "", err
	}

	newClient, err := dbos.RunAsStep(ctx, func(stepCtx context.Context) (Client, error) {
		return runStartupScript(stepCtx, machine)
	}, dbos.WithStepName("runStartupScript"))
	if err != nil {
		return "", err
	}

	if _, err := dbos.RunAsStep(ctx, func(stepCtx context.Context) (string, error) {
		return registerNewClient(stepCtx, newClient)
	}, dbos.WithStepName("registerNewClient")); err != nil {
		return "", err
	}

	return newClient.ID, nil
}

/*****************************/
/**** MOCKED EXTERNAL CALLS **/
/*****************************/

func getCongestionSignal(_ context.Context, client Client) (bool, error) {
	time.Sleep(500 * time.Millisecond)
	congested := rand.Intn(3) == 0
	fmt.Printf("[step] getCongestionSignal client=%s congested=%v\n", client.ID, congested)
	return congested, nil
}

func decommissionClient(_ context.Context, client Client) (string, error) {
	time.Sleep(1 * time.Second)
	fmt.Printf("[step] decommissionClient client=%s\n", client.ID)
	return client.ID, nil
}

func startNewMachine(_ context.Context, client Client) (Machine, error) {
	time.Sleep(2 * time.Second)
	m := Machine{ID: fmt.Sprintf("machine-for-%s", client.ID)}
	fmt.Printf("[step] startNewMachine client=%s -> %s\n", client.ID, m.ID)
	return m, nil
}

func runStartupScript(_ context.Context, machine Machine) (Client, error) {
	time.Sleep(2 * time.Second)
	c := Client{ID: fmt.Sprintf("client-on-%s", machine.ID)}
	fmt.Printf("[step] runStartupScript machine=%s -> %s\n", machine.ID, c.ID)
	return c, nil
}

func registerNewClient(_ context.Context, client Client) (string, error) {
	time.Sleep(500 * time.Millisecond)
	fmt.Printf("[step] registerNewClient client=%s\n", client.ID)
	return client.ID, nil
}

/*****************************/
/**** Main Function **********/
/*****************************/

func main() {
	var err error
	dbosCtx, err = dbos.NewDBOSContext(context.Background(), dbos.Config{
		DatabaseURL:        os.Getenv("DBOS_SYSTEM_DATABASE_URL"),
		AppName:            "txd-dbos",
		ApplicationVersion: "0.1.0",
		AdminServer:        true, // required by DBOS Cloud (port 3001)
		ConductorAPIKey:    os.Getenv("DBOS_CONDUCTOR_KEY"),
	})
	if err != nil {
		panic(err)
	}

	dbos.RegisterWorkflow(dbosCtx, autoscalingWorkflow,
		dbos.WithSchedule("0 * * * * *")) // Run every minute
	dbos.RegisterWorkflow(dbosCtx, checkUnhealthyClients)
	dbos.RegisterWorkflow(dbosCtx, replaceClient)

	if err := dbosCtx.Launch(); err != nil {
		panic(err)
	}
	defer dbosCtx.Shutdown(10 * time.Second)

	router := gin.Default()
	router.StaticFile("/", "./html/app.html")
	router.GET("/workflow/:taskid", workflowHandler)
	router.GET("/progress/:taskid", progressHandler)
	router.POST("/crash", crashHandler)

	fmt.Println("Server starting on http://localhost:8080")
	if err := router.Run(":8080"); err != nil {
		fmt.Printf("Error starting server: %s\n", err)
	}
}

/*****************************/
/**** HTTP HANDLERS **********/
/*****************************/

func workflowHandler(c *gin.Context) {
	taskID := c.Param("taskid")
	if taskID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Task ID is required"})
		return
	}

	_, err := dbos.RunWorkflow(dbosCtx, autoscalingWorkflow, time.Now(), dbos.WithWorkflowID(taskID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
}

func progressHandler(c *gin.Context) {
	taskID := c.Param("taskid")
	if taskID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Task ID is required"})
		return
	}

	progress, err := dbos.GetEvent[Progress](dbosCtx, taskID, PROGRESS_EVENT, 0)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, progress)
}

// crashHandler hard-exits the process to demonstrate workflow recovery.
func crashHandler(c *gin.Context) {
	os.Exit(1)
}
