package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
)

func main() {
	hostPort := os.Getenv("TEMPORAL_HOST")
	if hostPort == "" {
		hostPort = "localhost:7233"
	}

	linkAddr := os.Getenv("FISO_LINK_ADDR")
	if linkAddr == "" {
		linkAddr = "http://fiso-link:3500"
	}

	workflowID := os.Getenv("WORKFLOW_ID")
	if workflowID == "" {
		workflowID = "order-signal-e2e"
	}

	log.Printf("connecting to temporal at %s", hostPort)

	c, err := client.Dial(client.Options{
		HostPort:  hostPort,
		Namespace: "default",
	})
	if err != nil {
		log.Fatalf("temporal dial: %v", err)
	}
	defer c.Close()

	log.Println("connected to temporal, starting worker on queue=signal-processing")

	w := worker.New(c, "signal-processing", worker.Options{})
	w.RegisterWorkflow(WaitForSignal)
	w.RegisterActivity(&Activities{LinkAddr: linkAddr})

	// Start worker (non-blocking) so we can also start the workflow
	if err := w.Start(); err != nil {
		log.Fatalf("worker start: %v", err)
	}
	defer w.Stop()

	// Start a long-running workflow that waits for a signal.
	// This simulates a real scenario where a workflow is already running
	// and fiso-flow signals it with new data.
	log.Printf("starting workflow %s that waits for signal...", workflowID)
	run, err := c.ExecuteWorkflow(context.Background(), client.StartWorkflowOptions{
		ID:        workflowID,
		TaskQueue: "signal-processing",
	}, WaitForSignal)
	if err != nil {
		log.Fatalf("start workflow: %v", err)
	}
	log.Printf("WORKFLOW_STARTED workflow_id=%s run_id=%s", run.GetID(), run.GetRunID())

	// Block until interrupted
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	<-ch
	log.Println("shutting down")
}
