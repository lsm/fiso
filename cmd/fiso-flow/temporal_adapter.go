package main

import (
	"context"
	"fmt"

	temporalsink "github.com/lsm/fiso/internal/sink/temporal"
	"go.temporal.io/sdk/client"
)

// sdkWorkflowRun wraps the Temporal SDK WorkflowRun to satisfy the temporal.WorkflowRun interface.
type sdkWorkflowRun struct {
	run client.WorkflowRun
}

func (r *sdkWorkflowRun) GetID() string    { return r.run.GetID() }
func (r *sdkWorkflowRun) GetRunID() string { return r.run.GetRunID() }

// sdkAdapter wraps a Temporal SDK client to satisfy the temporal.WorkflowClient interface.
type sdkAdapter struct {
	client client.Client
}

func newTemporalSDKClient(cfg temporalsink.Config) (*sdkAdapter, error) {
	hostPort := cfg.HostPort
	if hostPort == "" {
		hostPort = "localhost:7233"
	}
	namespace := cfg.Namespace
	if namespace == "" {
		namespace = "default"
	}

	c, err := client.Dial(client.Options{
		HostPort:  hostPort,
		Namespace: namespace,
	})
	if err != nil {
		return nil, fmt.Errorf("dial temporal at %s: %w", hostPort, err)
	}

	return &sdkAdapter{client: c}, nil
}

func (a *sdkAdapter) ExecuteWorkflow(ctx context.Context, opts temporalsink.StartWorkflowOptions, workflow string, args ...interface{}) (temporalsink.WorkflowRun, error) {
	run, err := a.client.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:        opts.ID,
		TaskQueue: opts.TaskQueue,
	}, workflow, args...)
	if err != nil {
		return nil, err
	}
	return &sdkWorkflowRun{run: run}, nil
}

func (a *sdkAdapter) SignalWorkflow(ctx context.Context, workflowID, runID, signalName string, arg interface{}) error {
	return a.client.SignalWorkflow(ctx, workflowID, runID, signalName, arg)
}

func (a *sdkAdapter) Close() {
	a.client.Close()
}
