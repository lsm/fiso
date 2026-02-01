package async

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"time"

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
	if correlationID == "" {
		correlationID = generateID()
	}

	ce := map[string]interface{}{
		"specversion": "1.0",
		"id":          generateID(),
		"source":      "fiso-link",
		"type":        eventType,
		"time":        time.Now().UTC().Format(time.RFC3339),
		"data":        json.RawMessage(payload),
	}

	data, err := json.Marshal(ce)
	if err != nil {
		return fmt.Errorf("marshal cloudevent: %w", err)
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

func generateID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}
