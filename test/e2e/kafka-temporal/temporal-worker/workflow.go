package main

import (
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

// ProcessOrder is a Temporal workflow that processes an order event.
// It receives the CloudEvent-wrapped payload from fiso-flow and calls
// an external service via fiso-link.
func ProcessOrder(ctx workflow.Context, event []byte) (string, error) {
	opts := workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 3,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, opts)

	var result string
	err := workflow.ExecuteActivity(ctx, (*Activities).CallExternalService, event).Get(ctx, &result)
	if err != nil {
		return "", err
	}

	return result, nil
}
