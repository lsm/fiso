package async

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	"github.com/google/uuid"
	"github.com/lsm/fiso/internal/dlq"
)

// Publisher wraps events in CloudEvents and publishes to a broker.
type Publisher struct {
	broker dlq.Publisher
	topic  string
}

// NewPublisher creates a new async publisher. The broker implements
// the dlq.Publisher interface (same as used for DLQ publishing).
func NewPublisher(broker dlq.Publisher, topic string) *Publisher {
	return &Publisher{broker: broker, topic: topic}
}

// Publish wraps the payload in a CloudEvent with a correlation ID and
// publishes to the configured broker topic.
func (p *Publisher) Publish(ctx context.Context, eventType string, payload []byte, correlationID string) error {
	// Create CloudEvent using SDK
	event := cloudevents.NewEvent()
	event.SetID(uuid.New().String())
	event.SetSource("fiso-link")
	event.SetType(eventType)
	event.SetTime(time.Now().UTC())
	if err := event.SetData(cloudevents.ApplicationJSON, json.RawMessage(payload)); err != nil {
		return fmt.Errorf("set event data: %w", err)
	}

	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal cloudevent: %w", err)
	}

	// Generate correlation ID if not provided
	if correlationID == "" {
		correlationID = event.ID()
	}

	headers := map[string]string{
		"content-type":        "application/cloudevents+json",
		"fiso-correlation-id": correlationID,
	}

	return p.broker.Publish(ctx, p.topic, nil, data, headers)
}

// Close closes the underlying broker.
func (p *Publisher) Close() error {
	return p.broker.Close()
}
