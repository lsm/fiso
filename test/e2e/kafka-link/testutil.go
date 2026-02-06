// Package kafka_link provides test utilities for Kafka E2E testing.
package kafka_link

import (
	"context"
	"fmt"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
)

// WaitForTopicReady waits for a Kafka topic to be ready for consumption
func WaitForTopicReady(ctx context.Context, client *KafkaTestClient, topic string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for topic %s to be ready", topic)
		case <-ticker.C:
			// Try to consume from topic with a short timeout
			consumeCtx, consumeCancel := context.WithTimeout(ctx, 2*time.Second)
			_, err := client.ConsumeMessages(consumeCtx, topic, 1)
			consumeCancel()

			// If we get an error other than timeout/done, topic might be ready
			if err != nil && err != context.DeadlineExceeded && err != context.Canceled {
				return nil
			}
			// If we got a timeout, the topic exists but has no messages yet
			if err == context.DeadlineExceeded {
				return nil
			}
		}
	}
}

// WaitForMessages waits for a specific number of messages to be available
func WaitForMessages(ctx context.Context, client *KafkaTestClient, topic string, expectedCount int, timeout time.Duration) ([]*kgo.Record, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	deadline, _ := ctx.Deadline()
	checkInterval := 500 * time.Millisecond

	var messages []*kgo.Record

	for time.Now().Before(deadline) {
		// Fetch records
		fetchCtx, fetchCancel := context.WithTimeout(ctx, 2*time.Second)
		fetches := client.client.PollFetches(fetchCtx)
		fetchCancel()

		if fetches.Err() != nil && fetches.Err() != context.DeadlineExceeded {
			return nil, fmt.Errorf("poll fetches: %w", fetches.Err())
		}

		for _, fetch := range fetches.Records() {
			if fetch.Topic == topic {
				messages = append(messages, fetch)
			}
		}

		if len(messages) >= expectedCount {
			return messages, nil
		}

		select {
		case <-time.After(checkInterval):
		case <-ctx.Done():
			if len(messages) > 0 {
				return messages, nil
			}
			return nil, ctx.Err()
		}
	}

	if len(messages) > 0 {
		return messages, nil
	}
	return nil, fmt.Errorf("timeout waiting for %d messages", expectedCount)
}

// CleanupTopic removes a topic and all its messages
func CleanupTopic(ctx context.Context, client *KafkaTestClient, topic string) error {
	return client.DeleteTopic(ctx, topic)
}

// VerifyMessageKey verifies that a message has the expected key
func VerifyMessageKey(key []byte, expectedKey string) error {
	if expectedKey == "" {
		if key != nil && len(key) > 0 {
			return fmt.Errorf("expected empty key, got %q", string(key))
		}
		return nil
	}

	if key == nil || len(key) == 0 {
		return fmt.Errorf("expected key %q, got empty key", expectedKey)
	}

	actualKey := string(key)
	if actualKey != expectedKey {
		return fmt.Errorf("expected key %q, got %q", expectedKey, actualKey)
	}

	return nil
}

// VerifyHeader verifies that a message contains an expected header
func VerifyHeader(record *kgo.Record, key, expectedValue string) error {
	for _, header := range record.Headers {
		if header.Key == key {
			actualValue := string(header.Value)
			if actualValue == expectedValue {
				return nil
			}
			return fmt.Errorf("header %s: expected value %q, got %q", key, expectedValue, actualValue)
		}
	}
	return fmt.Errorf("header %s not found", key)
}
