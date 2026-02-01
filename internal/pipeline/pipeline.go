package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/lsm/fiso/internal/dlq"
	"github.com/lsm/fiso/internal/sink"
	"github.com/lsm/fiso/internal/source"
	"github.com/lsm/fiso/internal/transform"

	"crypto/rand"
)

// Config holds pipeline configuration.
type Config struct {
	FlowName  string
	EventType string // CloudEvent type (e.g., "order.created")
}

// Pipeline orchestrates the source → transform → sink flow.
type Pipeline struct {
	config      Config
	source      source.Source
	transformer transform.Transformer
	sink        sink.Sink
	dlq         *dlq.Handler
	logger      *slog.Logger
}

// New creates a new Pipeline. If transformer is nil, events pass through untransformed.
func New(cfg Config, src source.Source, tr transform.Transformer, sk sink.Sink, dlqHandler *dlq.Handler) *Pipeline {
	return &Pipeline{
		config:      cfg,
		source:      src,
		transformer: tr,
		sink:        sk,
		dlq:         dlqHandler,
		logger:      slog.Default(),
	}
}

// Run starts the pipeline. Blocks until ctx is cancelled.
func (p *Pipeline) Run(ctx context.Context) error {
	p.logger.Info("starting pipeline", "flow", p.config.FlowName)

	return p.source.Start(ctx, func(ctx context.Context, evt source.Event) error {
		if err := p.processEvent(ctx, evt); err != nil {
			p.logger.Error("event processing failed, sending to DLQ",
				"flow", p.config.FlowName,
				"topic", evt.Topic,
				"offset", evt.Offset,
				"error", err,
			)
		}
		// Always return nil to the source — errors are handled via DLQ.
		return nil
	})
}

func (p *Pipeline) processEvent(ctx context.Context, evt source.Event) error {
	payload := evt.Value

	// Transform
	if p.transformer != nil {
		transformed, err := p.transformer.Transform(ctx, payload)
		if err != nil {
			p.sendToDLQ(ctx, evt, "TRANSFORM_FAILED", err.Error())
			return err
		}
		payload = transformed
	}

	// Wrap in CloudEvent
	wrapped, err := p.wrapCloudEvent(payload)
	if err != nil {
		p.sendToDLQ(ctx, evt, "CLOUDEVENT_WRAP_FAILED", err.Error())
		return err
	}

	// Deliver to sink
	headers := map[string]string{
		"Content-Type": "application/cloudevents+json",
	}
	if err := p.sink.Deliver(ctx, wrapped, headers); err != nil {
		p.sendToDLQ(ctx, evt, "SINK_DELIVERY_FAILED", err.Error())
		return err
	}

	return nil
}

func (p *Pipeline) wrapCloudEvent(data []byte) ([]byte, error) {
	eventType := p.config.EventType
	if eventType == "" {
		eventType = "fiso.event"
	}

	ce := map[string]interface{}{
		"specversion": "1.0",
		"id":          generateID(),
		"source":      "fiso-flow/" + p.config.FlowName,
		"type":        eventType,
		"time":        time.Now().UTC().Format(time.RFC3339),
		"data":        json.RawMessage(data),
	}

	return json.Marshal(ce)
}

func (p *Pipeline) sendToDLQ(ctx context.Context, evt source.Event, code, message string) {
	info := dlq.FailureInfo{
		OriginalTopic: evt.Topic,
		ErrorCode:     code,
		ErrorMessage:  message,
		FlowName:      p.config.FlowName,
	}
	if err := p.dlq.Send(ctx, evt.Key, evt.Value, info); err != nil {
		p.logger.Error("failed to send to DLQ",
			"flow", p.config.FlowName,
			"error", err,
		)
	}
}

func generateID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}
