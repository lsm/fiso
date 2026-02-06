package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/lsm/fiso/internal/source/kafka"
)

// publisher is an interface that allows mocking the Kafka publisher for testing.
type publisher interface {
	Publish(ctx context.Context, topic string, key, value []byte, headers map[string]string) error
	Close() error
}

// newPublisherFunc is the function used to create a Kafka publisher.
// Tests can replace this to stub out the actual publisher.
var newPublisherFunc func(brokers []string) (publisher, error) = func(brokers []string) (publisher, error) {
	return kafka.NewPublisher(brokers)
}

// RunProduce produces test events to Kafka.
func RunProduce(args []string) error {
	if len(args) > 0 && (args[0] == "-h" || args[0] == "--help") {
		fmt.Println(`Usage: fiso produce --topic <name> [--file <path>] [--json <data>] [--count <n>] [--rate <duration>] [--brokers <addrs>]

Produces test events to a Kafka topic without needing docker commands.

Flags:
  --topic     Topic name (required)
  --file      Path to JSON file containing event data (one JSON object per line for multiple events)
  --json      Inline JSON data for a single event
  --count     Number of events to produce (default: 1)
  --rate      Rate limit duration between produces (e.g., 100ms, 1s). Default: no limiting
  --brokers   Kafka broker addresses (default: localhost:9092)

Examples:
  # Produce single event from file
  fiso produce --topic orders --file sample-order.json

  # Produce inline JSON
  fiso produce --topic orders --json '{"order_id":"TEST-001"}'

  # Produce multiple events from JSONL file
  fiso produce --topic orders --count 10 --file orders.jsonl

  # With rate limiting (100ms between events)
  fiso produce --topic orders --rate 100ms --file orders.jsonl`)
		return nil
	}

	topic, err := parseStringFlag(args, "--topic")
	if err != nil {
		return err
	}
	if topic == "" {
		return fmt.Errorf("--topic flag is required")
	}

	filePath, _ := parseStringFlag(args, "--file")
	inlineJSON, _ := parseStringFlag(args, "--json")
	count, _ := parseIntFlag(args, "--count", 1)
	rateStr, _ := parseStringFlag(args, "--rate")
	brokersStr, _ := parseStringFlag(args, "--brokers")

	brokers := []string{"localhost:9092"}
	if brokersStr != "" {
		brokers = strings.Split(brokersStr, ",")
		for i, b := range brokers {
			brokers[i] = strings.TrimSpace(b)
		}
	}

	if filePath == "" && inlineJSON == "" {
		return fmt.Errorf("either --file or --json must be specified")
	}
	if filePath != "" && inlineJSON != "" {
		return fmt.Errorf("cannot specify both --file and --json")
	}

	var rate time.Duration
	if rateStr != "" {
		rate, err = time.ParseDuration(rateStr)
		if err != nil {
			return fmt.Errorf("invalid rate duration: %w", err)
		}
	}

	publisher, err := newPublisherFunc(brokers)
	if err != nil {
		return fmt.Errorf("create kafka publisher: %w", err)
	}
	defer func() { _ = publisher.Close() }()

	ctx := context.Background()

	if inlineJSON != "" {
		return produceInlineJSON(ctx, publisher, topic, inlineJSON, count, rate)
	}

	return produceFromFile(ctx, publisher, topic, filePath, count, rate)
}

func produceInlineJSON(ctx context.Context, pub publisher, topic, jsonStr string, count int, rate time.Duration) error {
	var data json.RawMessage
	if err := json.Unmarshal([]byte(jsonStr), &data); err != nil {
		return fmt.Errorf("invalid json: %w", err)
	}

	for i := 0; i < count; i++ {
		if err := pub.Publish(ctx, topic, nil, data, nil); err != nil {
			return fmt.Errorf("publish event %d: %w", i+1, err)
		}
		fmt.Printf("Produced event %d to topic %s\n", i+1, topic)

		if rate > 0 && i < count-1 {
			time.Sleep(rate)
		}
	}

	fmt.Printf("Successfully produced %d event(s) to %s\n", count, topic)
	return nil
}

func produceFromFile(ctx context.Context, pub publisher, topic, filePath string, count int, rate time.Duration) error {
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("open file: %w", err)
	}
	defer func() { _ = file.Close() }()

	produced := 0
	scanner := bufio.NewScanner(file)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var data json.RawMessage
		if err := json.Unmarshal([]byte(line), &data); err != nil {
			return fmt.Errorf("invalid json on line %d: %w", lineNum, err)
		}

		if err := pub.Publish(ctx, topic, nil, data, nil); err != nil {
			return fmt.Errorf("publish event from line %d: %w", lineNum, err)
		}

		produced++
		fmt.Printf("Produced event %d (line %d) to topic %s\n", produced, lineNum, topic)

		if count > 0 && produced >= count {
			break
		}

		if rate > 0 {
			time.Sleep(rate)
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read file: %w", err)
	}

	if produced == 0 {
		return fmt.Errorf("no valid json events found in file")
	}

	fmt.Printf("Successfully produced %d event(s) to %s\n", produced, topic)
	return nil
}

func parseStringFlag(args []string, flag string) (string, error) {
	for i, arg := range args {
		if arg == flag {
			if i+1 < len(args) {
				return args[i+1], nil
			}
			return "", fmt.Errorf("flag %s requires a value", flag)
		}
	}
	return "", nil
}

func parseIntFlag(args []string, flag string, defaultVal int) (int, error) {
	str, err := parseStringFlag(args, flag)
	if err != nil {
		return 0, err
	}
	if str == "" {
		return defaultVal, nil
	}
	var val int
	if _, err := fmt.Sscanf(str, "%d", &val); err != nil {
		return 0, fmt.Errorf("invalid value for %s: must be an integer", flag)
	}
	if val < 1 {
		return 0, fmt.Errorf("invalid value for %s: must be >= 1", flag)
	}
	return val, nil
}
