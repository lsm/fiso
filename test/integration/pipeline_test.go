//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/lsm/fiso/internal/dlq"
	"github.com/lsm/fiso/internal/pipeline"
	httpsink "github.com/lsm/fiso/internal/sink/http"
	"github.com/lsm/fiso/internal/source/kafka"
	celxform "github.com/lsm/fiso/internal/transform/cel"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
)

func brokers() []string {
	b := os.Getenv("KAFKA_BROKERS")
	if b == "" {
		b = "localhost:9092"
	}
	return strings.Split(b, ",")
}

func TestPipeline_EndToEnd(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	topic := fmt.Sprintf("integration-test-%d", time.Now().UnixNano())
	dlqTopic := fmt.Sprintf("fiso-dlq-%s", topic)

	// Create topic via admin client
	adminClient, err := kgo.NewClient(kgo.SeedBrokers(brokers()...))
	if err != nil {
		t.Fatalf("admin client: %v", err)
	}
	defer adminClient.Close()

	admin := kadm.NewClient(adminClient)
	_, err = admin.CreateTopics(ctx, 1, 1, nil, topic)
	if err != nil {
		t.Fatalf("create topic: %v", err)
	}
	defer func() {
		_, _ = admin.DeleteTopics(ctx, topic, dlqTopic)
	}()

	// Start HTTP sink (echo server)
	received := make(chan []byte, 10)
	sinkServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		received <- body
		w.WriteHeader(http.StatusOK)
	}))
	defer sinkServer.Close()

	// Build pipeline: Kafka source → CEL transform → HTTP sink
	logger := slog.Default()

	src, err := kafka.NewSource(kafka.Config{
		Brokers:       brokers(),
		Topic:         topic,
		ConsumerGroup: fmt.Sprintf("test-cg-%d", time.Now().UnixNano()),
		StartOffset:   "earliest",
	}, logger)
	if err != nil {
		t.Fatalf("kafka source: %v", err)
	}

	transformer, err := celxform.NewTransformer(`{"transformed": true, "original": data}`)
	if err != nil {
		t.Fatalf("cel transformer: %v", err)
	}

	sk, err := httpsink.NewSink(httpsink.Config{
		URL:    sinkServer.URL,
		Method: "POST",
	})
	if err != nil {
		t.Fatalf("http sink: %v", err)
	}

	pub, err := kafka.NewPublisher(brokers())
	if err != nil {
		t.Fatalf("dlq publisher: %v", err)
	}
	dlqHandler := dlq.NewHandler(pub)

	p := pipeline.New(pipeline.Config{
		FlowName:  "integration-test",
		EventType: "test.event",
	}, src, transformer, sk, dlqHandler)

	// Start pipeline in background
	pipelineCtx, pipelineCancel := context.WithCancel(ctx)
	pipelineDone := make(chan error, 1)
	go func() {
		pipelineDone <- p.Run(pipelineCtx)
	}()

	// Give pipeline time to start consuming
	time.Sleep(2 * time.Second)

	// Produce a test message
	producer, err := kgo.NewClient(kgo.SeedBrokers(brokers()...))
	if err != nil {
		t.Fatalf("producer client: %v", err)
	}
	defer producer.Close()

	record := &kgo.Record{
		Topic: topic,
		Key:   []byte("test-key"),
		Value: []byte(`{"event_id": "abc-123", "action": "created"}`),
	}
	results := producer.ProduceSync(ctx, record)
	if err := results.FirstErr(); err != nil {
		t.Fatalf("produce: %v", err)
	}

	// Wait for the event to arrive at the sink
	select {
	case body := <-received:
		var ce map[string]interface{}
		if err := json.Unmarshal(body, &ce); err != nil {
			t.Fatalf("unmarshal CloudEvent: %v", err)
		}

		// Verify CloudEvent structure
		if ce["specversion"] != "1.0" {
			t.Errorf("expected specversion 1.0, got %v", ce["specversion"])
		}
		if ce["type"] != "test.event" {
			t.Errorf("expected type test.event, got %v", ce["type"])
		}
		if ce["source"] != "fiso-flow/integration-test" {
			t.Errorf("expected source fiso-flow/integration-test, got %v", ce["source"])
		}
		if _, ok := ce["data"]; !ok {
			t.Error("expected data field in CloudEvent")
		}

	case <-time.After(15 * time.Second):
		t.Fatal("timed out waiting for event at sink")
	}

	// Test graceful shutdown
	pipelineCancel()

	select {
	case err := <-pipelineDone:
		if err != nil && err != context.Canceled {
			t.Errorf("unexpected pipeline error: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for pipeline shutdown")
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := p.Shutdown(shutdownCtx); err != nil {
		t.Errorf("pipeline shutdown error: %v", err)
	}
}
