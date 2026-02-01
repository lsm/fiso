package kafka

import (
	"testing"
)

func TestNewSource_MissingBrokers(t *testing.T) {
	_, err := NewSource(Config{
		Topic:         "test",
		ConsumerGroup: "test-group",
	}, nil)
	if err == nil {
		t.Fatal("expected error for missing brokers")
	}
}

func TestNewSource_MissingTopic(t *testing.T) {
	_, err := NewSource(Config{
		Brokers:       []string{"localhost:9092"},
		ConsumerGroup: "test-group",
	}, nil)
	if err == nil {
		t.Fatal("expected error for missing topic")
	}
}

func TestNewSource_MissingConsumerGroup(t *testing.T) {
	_, err := NewSource(Config{
		Brokers: []string{"localhost:9092"},
		Topic:   "test",
	}, nil)
	if err == nil {
		t.Fatal("expected error for missing consumer group")
	}
}

func TestNewSource_ValidConfig(t *testing.T) {
	s, err := NewSource(Config{
		Brokers:       []string{"localhost:9092"},
		Topic:         "test-topic",
		ConsumerGroup: "test-group",
		StartOffset:   "earliest",
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = s.Close() }()

	if s.topic != "test-topic" {
		t.Errorf("expected topic test-topic, got %s", s.topic)
	}
}

func TestNewSource_DefaultOffset(t *testing.T) {
	s, err := NewSource(Config{
		Brokers:       []string{"localhost:9092"},
		Topic:         "test-topic",
		ConsumerGroup: "test-group",
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = s.Close() }()
}
