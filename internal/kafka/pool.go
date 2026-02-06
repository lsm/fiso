package kafka

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/twmb/franz-go/pkg/kgo"
)

// producer abstracts the kafka client methods used by pooled publishers.
type producer interface {
	ProduceSync(ctx context.Context, rs ...*kgo.Record) kgo.ProduceResults
	Close()
}

// PooledPublisher wraps a Kafka client for publishing with connection pooling.
type PooledPublisher struct {
	client producer
	name   string
}

// Publish sends a message to the specified Kafka topic.
func (p *PooledPublisher) Publish(ctx context.Context, topic string, key, value []byte, headers map[string]string) error {
	record := &kgo.Record{
		Topic: topic,
		Key:   key,
		Value: value,
	}
	for k, v := range headers {
		record.Headers = append(record.Headers, kgo.RecordHeader{Key: k, Value: []byte(v)})
	}

	results := p.client.ProduceSync(ctx, record)
	if err := results.FirstErr(); err != nil {
		return fmt.Errorf("kafka publish to %s: %w", topic, err)
	}
	return nil
}

// Close is a no-op for pooled publishers; the pool manages client lifecycle.
func (p *PooledPublisher) Close() error {
	return nil
}

// PublisherPool manages shared Kafka publisher connections per cluster.
type PublisherPool struct {
	mu       sync.RWMutex
	clients  map[string]*kgo.Client
	registry *Registry
}

// NewPublisherPool creates a new publisher pool.
func NewPublisherPool(registry *Registry) *PublisherPool {
	return &PublisherPool{
		clients:  make(map[string]*kgo.Client),
		registry: registry,
	}
}

// Get returns a publisher for the named cluster, creating the client if needed.
func (p *PublisherPool) Get(clusterName string) (*PooledPublisher, error) {
	// Fast path: check if client already exists
	p.mu.RLock()
	client, exists := p.clients[clusterName]
	p.mu.RUnlock()

	if exists {
		return &PooledPublisher{client: client, name: clusterName}, nil
	}

	// Slow path: create new client
	p.mu.Lock()
	defer p.mu.Unlock()

	// Double-check after acquiring write lock
	if client, exists = p.clients[clusterName]; exists {
		return &PooledPublisher{client: client, name: clusterName}, nil
	}

	cfg, ok := p.registry.Get(clusterName)
	if !ok {
		return nil, fmt.Errorf("cluster %q not found in registry", clusterName)
	}

	opts, err := ClientOptions(cfg)
	if err != nil {
		return nil, fmt.Errorf("cluster %q options: %w", clusterName, err)
	}

	client, err = kgo.NewClient(opts...)
	if err != nil {
		return nil, fmt.Errorf("cluster %q client: %w", clusterName, err)
	}

	p.clients[clusterName] = client
	return &PooledPublisher{client: client, name: clusterName}, nil
}

// GetForConfig returns a publisher for an inline cluster config (not registered).
// This supports backwards compatibility with inline brokers.
func (p *PublisherPool) GetForConfig(cfg *ClusterConfig) (*PooledPublisher, error) {
	// Generate a key based on brokers (simple heuristic for pooling)
	key := "_inline_" + strings.Join(cfg.Brokers, ",")

	p.mu.RLock()
	client, exists := p.clients[key]
	p.mu.RUnlock()

	if exists {
		return &PooledPublisher{client: client, name: key}, nil
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if client, exists = p.clients[key]; exists {
		return &PooledPublisher{client: client, name: key}, nil
	}

	opts, err := ClientOptions(cfg)
	if err != nil {
		return nil, err
	}

	client, err = kgo.NewClient(opts...)
	if err != nil {
		return nil, err
	}

	p.clients[key] = client
	return &PooledPublisher{client: client, name: key}, nil
}

// Close closes all pooled clients.
func (p *PublisherPool) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	for name, client := range p.clients {
		client.Close()
		delete(p.clients, name)
	}
	return nil
}
