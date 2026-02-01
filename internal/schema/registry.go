package schema

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// Schema represents a schema definition from the registry.
type Schema struct {
	ID      int    `json:"id"`
	Subject string `json:"subject"`
	Version int    `json:"version"`
	Schema  string `json:"schema"`
	Type    string `json:"schemaType"` // AVRO, PROTOBUF, JSON
}

// Registry retrieves schemas from a schema registry.
type Registry interface {
	// GetByID retrieves a schema by its global ID.
	GetByID(ctx context.Context, id int) (*Schema, error)
	// GetLatest retrieves the latest version of a schema for a subject.
	GetLatest(ctx context.Context, subject string) (*Schema, error)
}

// HTTPClient abstracts HTTP calls for testing.
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// ConfluentRegistry implements Registry using the Confluent Schema Registry HTTP API.
type ConfluentRegistry struct {
	baseURL string
	client  HTTPClient
	mu      sync.RWMutex
	cache   map[int]*cachedSchema
	ttl     time.Duration
	clock   func() time.Time
}

type cachedSchema struct {
	schema    *Schema
	fetchedAt time.Time
}

// RegistryOption configures the ConfluentRegistry.
type RegistryOption func(*ConfluentRegistry)

// WithCacheTTL sets the cache TTL.
func WithCacheTTL(ttl time.Duration) RegistryOption {
	return func(r *ConfluentRegistry) { r.ttl = ttl }
}

// WithHTTPClient sets the HTTP client.
func WithHTTPClient(c HTTPClient) RegistryOption {
	return func(r *ConfluentRegistry) { r.client = c }
}

// WithRegistryClock sets the clock function (for testing).
func WithRegistryClock(clock func() time.Time) RegistryOption {
	return func(r *ConfluentRegistry) { r.clock = clock }
}

// NewConfluentRegistry creates a new Confluent Schema Registry client.
func NewConfluentRegistry(baseURL string, opts ...RegistryOption) (*ConfluentRegistry, error) {
	if baseURL == "" {
		return nil, fmt.Errorf("schema registry base URL is required")
	}
	r := &ConfluentRegistry{
		baseURL: baseURL,
		client:  http.DefaultClient,
		cache:   make(map[int]*cachedSchema),
		ttl:     5 * time.Minute,
		clock:   time.Now,
	}
	for _, opt := range opts {
		opt(r)
	}
	return r, nil
}

// GetByID retrieves a schema by its global ID with caching.
func (r *ConfluentRegistry) GetByID(ctx context.Context, id int) (*Schema, error) {
	if s := r.fromCache(id); s != nil {
		return s, nil
	}

	url := fmt.Sprintf("%s/schemas/ids/%d", r.baseURL, id)
	schema, err := r.fetch(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("get schema by id %d: %w", id, err)
	}
	schema.ID = id

	r.toCache(id, schema)
	return schema, nil
}

// GetLatest retrieves the latest version of a schema for a subject.
func (r *ConfluentRegistry) GetLatest(ctx context.Context, subject string) (*Schema, error) {
	url := fmt.Sprintf("%s/subjects/%s/versions/latest", r.baseURL, subject)
	schema, err := r.fetch(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("get latest schema for %s: %w", subject, err)
	}

	r.toCache(schema.ID, schema)
	return schema, nil
}

func (r *ConfluentRegistry) fetch(ctx context.Context, url string) (*Schema, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.schemaregistry.v1+json")

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("registry returned %d: %s", resp.StatusCode, string(body))
	}

	var schema Schema
	if err := json.NewDecoder(resp.Body).Decode(&schema); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &schema, nil
}

func (r *ConfluentRegistry) fromCache(id int) *Schema {
	r.mu.RLock()
	defer r.mu.RUnlock()
	cached, ok := r.cache[id]
	if !ok {
		return nil
	}
	if r.clock().Sub(cached.fetchedAt) > r.ttl {
		return nil
	}
	return cached.schema
}

func (r *ConfluentRegistry) toCache(id int, schema *Schema) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cache[id] = &cachedSchema{
		schema:    schema,
		fetchedAt: r.clock(),
	}
}
