package main

import (
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

// WaitForSignal is a Temporal workflow that blocks until it receives
// the "order-update" signal. Once signaled, it processes the event
// data by calling an external service via fiso-link.
func WaitForSignal(ctx workflow.Context) (string, error) {
	// Wait for the "order-update" signal from fiso-flow
	var signalData []byte
	signalCh := workflow.GetSignalChannel(ctx, "order-update")
	signalCh.Receive(ctx, &signalData)

	opts := workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 3,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, opts)

	var result string
	err := workflow.ExecuteActivity(ctx, (*Activities).ProcessSignalData, signalData).Get(ctx, &result)
	if err != nil {
		return "", err
	}

	return result, nil
}
