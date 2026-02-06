package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/lsm/fiso/internal/source/kafka"
	"github.com/twmb/franz-go/pkg/kgo"
	"gopkg.in/yaml.v3"
)

// kafkaConsumer is an interface that abstracts the Kafka client for testing.
type kafkaConsumer interface {
	PollFetches(ctx context.Context) kgo.Fetches
	MarkCommitRecords(rs ...*kgo.Record)
	CommitMarkedOffsets(ctx context.Context) error
	Close()
}

// createKafkaClientFunc is the function used to create a Kafka client.
// Tests can replace this to stub out the actual client.
var createKafkaClientFunc = func(cfg kafka.Config) (kafkaConsumer, error) {
	return createKafkaClient(cfg)
}

// RunConsume consumes and displays events from Kafka for debugging.
func RunConsume(args []string) error {
	if len(args) > 0 && (args[0] == "-h" || args[0] == "--help") {
		fmt.Println(`Usage: fiso consume --topic <name> [options]

Consumes and displays events from a Kafka topic for debugging.
Uses the Kafka configuration from fiso/flows/*.yaml files.

Options:
  --topic <name>         Kafka topic to consume (required)
  --from-beginning       Start from the earliest offset instead of latest
  --max-messages <n>     Maximum number of messages to consume (default: 10)
  --follow               Continuously consume new messages (like tail -f)

Examples:
  fiso consume --topic orders --max-messages 10
  fiso consume --topic orders --from-beginning --max-messages 100
  fiso consume --topic orders --follow`)
		return nil
	}

	// Parse flags
	var topic string
	fromBeginning := false
	maxMessages := 10
	follow := false

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--topic":
			if i+1 >= len(args) {
				return fmt.Errorf("--topic requires a value")
			}
			topic = args[i+1]
			i++
		case "--from-beginning":
			fromBeginning = true
		case "--max-messages":
			if i+1 >= len(args) {
				return fmt.Errorf("--max-messages requires a value")
			}
			var err error
			maxMessages, err = parseMessageCount(args[i+1])
			if err != nil {
				return fmt.Errorf("invalid --max-messages value: %w", err)
			}
			i++
		case "--follow":
			follow = true
		default:
			if strings.HasPrefix(args[i], "--") {
				return fmt.Errorf("unknown flag: %s", args[i])
			}
		}
	}

	if topic == "" {
		return fmt.Errorf("--topic is required")
	}

	// Load Kafka configuration from flow files
	kafkaCfg, err := loadKafkaConfig(topic)
	if err != nil {
		return fmt.Errorf("load kafka config: %w", err)
	}

	// Create Kafka consumer
	cfg := kafka.Config{
		Brokers:       kafkaCfg.Brokers,
		Topic:         topic,
		ConsumerGroup: fmt.Sprintf("fiso-consume-%d", time.Now().Unix()),
		StartOffset:   "latest",
	}
	if fromBeginning {
		cfg.StartOffset = "earliest"
	}

	client, err := createKafkaClientFunc(cfg)
	if err != nil {
		return fmt.Errorf("create kafka client: %w", err)
	}
	defer client.Close()

	fmt.Printf("Consuming from topic: %s\n", topic)
	fmt.Printf("Brokers: %s\n", strings.Join(cfg.Brokers, ", "))
	fmt.Printf("Offset: %s\n", cfg.StartOffset)
	if follow {
		fmt.Println("Mode: follow (continuous)")
	} else {
		fmt.Printf("Max messages: %d\n", maxMessages)
	}
	fmt.Println()

	// Set up context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, shutdownSignals...)
	go func() {
		<-sigChan
		fmt.Println("\nShutting down...")
		cancel()
	}()

	// Consume messages
	messageCount := 0
	if follow {
		// Follow mode: consume continuously
		err = consumeFollow(ctx, client, topic)
	} else {
		// Batch mode: consume up to maxMessages
		err = consumeBatch(ctx, client, topic, maxMessages, &messageCount)
	}

	if err != nil && ctx.Err() == nil {
		return fmt.Errorf("consume error: %w", err)
	}

	if !follow {
		fmt.Printf("\nConsumed %d message(s)\n", messageCount)
	}

	return nil
}

// consumeBatch consumes a fixed number of messages and exits.
func consumeBatch(ctx context.Context, client kafkaConsumer, topic string, maxMessages int, messageCount *int) error {
	for *messageCount < maxMessages {
		fetches := client.PollFetches(ctx)
		if errs := fetches.Errors(); len(errs) > 0 {
			for _, err := range errs {
				fmt.Fprintf(os.Stderr, "Fetch error: %v\n", err.Err)
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			continue
		}

		recordCount := 0
		fetches.EachRecord(func(record *kgo.Record) {
			recordCount++
		})

		if recordCount == 0 {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			// No messages yet, wait a bit
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(100 * time.Millisecond):
				continue
			}
		}

		fetches.EachRecord(func(record *kgo.Record) {
			printMessage(record)
			*messageCount++
			client.MarkCommitRecords(record)
		})

		// Commit offsets
		if err := client.CommitMarkedOffsets(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "Commit error: %v\n", err)
		}
	}

	return nil
}

// consumeFollow continuously consumes messages until interrupted.
func consumeFollow(ctx context.Context, client kafkaConsumer, topic string) error {
	for {
		fetches := client.PollFetches(ctx)
		if errs := fetches.Errors(); len(errs) > 0 {
			for _, err := range errs {
				fmt.Fprintf(os.Stderr, "Fetch error: %v\n", err.Err)
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			continue
		}

		fetches.EachRecord(func(record *kgo.Record) {
			printMessage(record)
			client.MarkCommitRecords(record)
		})

		// Commit offsets
		if err := client.CommitMarkedOffsets(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "Commit error: %v\n", err)
		}

		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
}

// printMessage pretty-prints a Kafka message to stdout.
func printMessage(record *kgo.Record) {
	fmt.Printf("---\n")
	fmt.Printf("Partition: %d\n", record.Partition)
	fmt.Printf("Offset:    %d\n", record.Offset)
	fmt.Printf("Timestamp: %s\n", record.Timestamp.Format(time.RFC3339))
	fmt.Printf("Key:       %s\n", string(record.Key))

	if len(record.Headers) > 0 {
		fmt.Printf("Headers:\n")
		for _, h := range record.Headers {
			fmt.Printf("  %s: %s\n", h.Key, string(h.Value))
		}
	}

	// Try to pretty-print JSON value
	var value interface{}
	if err := json.Unmarshal(record.Value, &value); err == nil {
		pretty, _ := json.MarshalIndent(value, "  ", "  ")
		fmt.Printf("Value:\n  %s\n", string(pretty))
	} else {
		// Not JSON, print as string
		fmt.Printf("Value:     %s\n", string(record.Value))
	}
	fmt.Println()
}

// kafkaConfig holds Kafka connection configuration.
type kafkaConfig struct {
	Brokers []string
}

// loadKafkaConfig loads Kafka configuration from flow definition files.
func loadKafkaConfig(topic string) (*kafkaConfig, error) {
	// Search for Kafka source configuration in flow files
	flowDir := "./fiso/flows"
	entries, err := os.ReadDir(flowDir)
	if err != nil {
		// If fiso/flows doesn't exist, use default localhost config
		return &kafkaConfig{Brokers: []string{"localhost:9092"}}, nil
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := entry.Name()
		if !strings.HasSuffix(ext, ".yaml") && !strings.HasSuffix(ext, ".yml") {
			continue
		}

		path := flowDir + "/" + entry.Name()
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		// Parse YAML to extract Kafka brokers
		cfg, err := parseKafkaConfigFromYAML(data)
		if err != nil {
			continue
		}
		if cfg != nil {
			return cfg, nil
		}
	}

	// Default to localhost
	return &kafkaConfig{Brokers: []string{"localhost:9092"}}, nil
}

// parseKafkaConfigFromYAML extracts Kafka configuration from a flow definition YAML.
func parseKafkaConfigFromYAML(data []byte) (*kafkaConfig, error) {
	type flowConfig struct {
		Source struct {
			Type   string `yaml:"type"`
			Config struct {
				Brokers       []string `yaml:"brokers"`
				Topic         string   `yaml:"topic"`
				ConsumerGroup string   `yaml:"consumerGroup"`
			} `yaml:"config"`
		} `yaml:"source"`
	}

	var fc flowConfig
	if err := yaml.Unmarshal(data, &fc); err != nil {
		return nil, err
	}

	if fc.Source.Type == "kafka" && len(fc.Source.Config.Brokers) > 0 {
		return &kafkaConfig{Brokers: fc.Source.Config.Brokers}, nil
	}

	return nil, nil
}

// createKafkaClient creates a Kafka client for consuming.
func createKafkaClient(cfg kafka.Config) (*kgo.Client, error) {
	offset := kgo.NewOffset().AtEnd()
	if cfg.StartOffset == "earliest" {
		offset = kgo.NewOffset().AtStart()
	}

	opts := []kgo.Opt{
		kgo.SeedBrokers(cfg.Brokers...),
		kgo.ConsumerGroup(cfg.ConsumerGroup),
		kgo.ConsumeTopics(cfg.Topic),
		kgo.ConsumeResetOffset(offset),
		kgo.DisableAutoCommit(),
		// Add short timeout for initial connection attempts
		// This allows tests to fail fast when Kafka is unavailable
		kgo.DialTimeout(3 * time.Second),
	}

	return kgo.NewClient(opts...)
}

// parseMessageCount parses the max-messages flag value.
func parseMessageCount(s string) (int, error) {
	s = strings.TrimSpace(s)
	var n int
	numParsed, err := fmt.Sscanf(s, "%d", &n)
	if err != nil || numParsed != 1 {
		return 0, fmt.Errorf("invalid number: %s", s)
	}
	// Check if the entire string was just the number (no extra chars)
	formatted := fmt.Sprintf("%d", n)
	if s != formatted {
		return 0, fmt.Errorf("invalid number: %s", s)
	}
	if n <= 0 {
		return 0, fmt.Errorf("must be positive: %s", s)
	}
	return n, nil
}
