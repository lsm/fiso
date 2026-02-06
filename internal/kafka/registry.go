package kafka

import (
	"fmt"
	"sync"
)

// Registry manages named Kafka cluster configurations.
type Registry struct {
	mu       sync.RWMutex
	clusters map[string]*ClusterConfig
}

// NewRegistry creates a new cluster registry.
func NewRegistry() *Registry {
	return &Registry{
		clusters: make(map[string]*ClusterConfig),
	}
}

// Register adds a cluster configuration to the registry.
func (r *Registry) Register(name string, cfg *ClusterConfig) error {
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("cluster %q: %w", name, err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	cfg.Name = name
	r.clusters[name] = cfg
	return nil
}

// Get retrieves a cluster configuration by name.
func (r *Registry) Get(name string) (*ClusterConfig, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	cfg, ok := r.clusters[name]
	return cfg, ok
}

// Has checks if a cluster exists in the registry.
func (r *Registry) Has(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	_, ok := r.clusters[name]
	return ok
}

// Names returns all registered cluster names.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.clusters))
	for name := range r.clusters {
		names = append(names, name)
	}
	return names
}

// LoadFromMap populates the registry from a configuration map.
func (r *Registry) LoadFromMap(clusters map[string]ClusterConfig) error {
	for name, cfg := range clusters {
		cfgCopy := cfg // avoid loop variable capture
		if err := r.Register(name, &cfgCopy); err != nil {
			return err
		}
	}
	return nil
}
