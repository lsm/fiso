package main

import (
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

// ProcessOrder is a Temporal workflow that processes an order event.
// It receives the CloudEvent-wrapped payload from fiso-flow and calls
// an external service via fiso-link.
// The event is received as a structured CloudEvent map (JSON object),
// making it compatible with Java/Kotlin SDK workflows.
func ProcessOrder(ctx workflow.Context, event map[string]interface{}) (string, error) {
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
