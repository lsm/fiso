package main

import (
	"log"
	"os"

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

	log.Printf("connecting to temporal at %s", hostPort)

	c, err := client.Dial(client.Options{
		HostPort:  hostPort,
		Namespace: "default",
	})
	if err != nil {
		log.Fatalf("temporal dial: %v", err)
	}
	defer c.Close()

	log.Println("connected to temporal, starting worker on queue=oidc-test-queue")

	w := worker.New(c, "oidc-test-queue", worker.Options{})
	w.RegisterWorkflow(ProcessOrder)
	w.RegisterActivity(&Activities{LinkAddr: linkAddr})

	if err := w.Run(worker.InterruptCh()); err != nil {
		log.Fatalf("worker run: %v", err)
	}
}
