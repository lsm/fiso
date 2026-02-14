package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
)

// CDCEvent represents a Debezium CDC event structure
type CDCEvent struct {
	Payload struct {
		Before map[string]interface{} `json:"before"`
		After  map[string]interface{} `json:"after"`
		Source struct {
			Version   string `json:"version"`
			Connector string `json:"connector"`
			Name      string `json:"name"`
			TsMs      int64  `json:"ts_ms"`
			Database  string `json:"db"`
			Schema    string `json:"schema"`
			Table     string `json:"table"`
		} `json:"source"`
		Op   string `json:"op"`
		TsMs int64  `json:"ts_ms"`
	} `json:"payload"`
}

// Order represents an order from the CDC event
type Order struct {
	ID         int     `json:"id"`
	OrderID    string  `json:"order_id"`
	CustomerID string  `json:"customer_id"`
	Amount     float64 `json:"amount"`
	Status     string  `json:"status"`
	CreatedAt  string  `json:"created_at"`
	UpdatedAt  string  `json:"updated_at"`
}

var (
	receivedEvents = make(map[string]bool)
	eventsMutex    sync.Mutex
	kafkaClient    *kgo.Client
)

func main() {
	brokers := os.Getenv("KAFKA_BROKERS")
	if brokers == "" {
		brokers = "kafka:9092"
	}

	log.Printf("Starting user-service with Kafka brokers: %s", brokers)

	// Create Kafka consumer
	var err error
	kafkaClient, err = kgo.NewClient(
		kgo.SeedBrokers(brokers),
		kgo.ConsumeTopics("orders-cdc"),
		kgo.ConsumerGroup("debezium-cdc-consumer"),
		kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()),
	)
	if err != nil {
		log.Fatalf("Failed to create Kafka client: %v", err)
	}
	defer kafkaClient.Close()

	// Start consuming in background
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go consumeCDCEvents(ctx)

	// HTTP endpoints
	http.HandleFunc("/health", healthHandler)
	http.HandleFunc("/events", eventsHandler)
	http.HandleFunc("/", rootHandler)

	// Start HTTP server
	go func() {
		log.Printf("user-service listening on :8082")
		if err := http.ListenAndServe(":8082", nil); err != nil {
			log.Fatalf("HTTP server failed: %v", err)
		}
	}()

	// Wait for shutdown signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan
	log.Println("Shutting down user-service...")
}

func consumeCDCEvents(ctx context.Context) {
	log.Println("Starting to consume CDC events from orders-cdc topic...")

	for {
		select {
		case <-ctx.Done():
			return
		default:
			fetches := kafkaClient.PollFetches(ctx)
			if fetches.Err() != nil {
				if fetches.Err() == context.Canceled {
					return
				}
				log.Printf("Error polling: %v", fetches.Err())
				time.Sleep(1 * time.Second)
				continue
			}

			fetches.EachRecord(func(record *kgo.Record) {
				processCDCRecord(record.Value, string(record.Key))
			})
		}
	}
}

func processCDCRecord(value []byte, key string) {
	log.Printf("Received CDC event - Key: %s", key)
	log.Printf("Raw event: %s", string(value))

	// Try to parse as Debezium CDC event
	var cdcEvent CDCEvent
	if err := json.Unmarshal(value, &cdcEvent); err != nil {
		log.Printf("Failed to parse CDC event: %v", err)
		// Try parsing as raw JSON
		var rawData map[string]interface{}
		if err := json.Unmarshal(value, &rawData); err != nil {
			log.Printf("Failed to parse as raw JSON: %v", err)
			return
		}
		log.Printf("Raw event data: %+v", rawData)
		return
	}

	// Extract operation type
	op := cdcEvent.Payload.Op
	var orderData map[string]interface{}
	var eventType string

	switch op {
	case "c":
		eventType = "INSERT"
		orderData = cdcEvent.Payload.After
	case "u":
		eventType = "UPDATE"
		orderData = cdcEvent.Payload.After
		log.Printf("  Before: %+v", cdcEvent.Payload.Before)
	case "d":
		eventType = "DELETE"
		orderData = cdcEvent.Payload.Before
	case "r":
		eventType = "SNAPSHOT"
		orderData = cdcEvent.Payload.After
	default:
		eventType = "UNKNOWN"
		orderData = cdcEvent.Payload.After
	}

	// Extract order_id from the event
	var orderID string
	if orderData != nil {
		if id, ok := orderData["order_id"].(string); ok {
			orderID = id
		}
	}

	log.Printf("CDC Event Type: %s, Table: %s, OrderID: %s",
		eventType, cdcEvent.Payload.Source.Table, orderID)
	log.Printf("  Data: %+v", orderData)

	// Track received events
	if orderID != "" {
		eventsMutex.Lock()
		receivedEvents[orderID] = true
		eventsMutex.Unlock()
		log.Printf("Tracked order: %s (total tracked: %d)", orderID, len(receivedEvents))
	}
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

func eventsHandler(w http.ResponseWriter, r *http.Request) {
	eventsMutex.Lock()
	defer eventsMutex.Unlock()

	events := make([]string, 0, len(receivedEvents))
	for k := range receivedEvents {
		events = append(events, k)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"count":  len(events),
		"events": events,
	})
}

func rootHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"service": "debezium-cdc-verification",
		"status":  "running",
	})
}
