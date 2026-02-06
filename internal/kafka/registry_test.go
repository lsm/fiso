package kafka

import (
	"sort"
	"testing"
)

func TestRegistry_RegisterAndGet(t *testing.T) {
	r := NewRegistry()

	cfg := &ClusterConfig{
		Brokers: []string{"localhost:9092"},
	}

	if err := r.Register("main", cfg); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	got, ok := r.Get("main")
	if !ok {
		t.Fatal("Get() returned false for registered cluster")
	}
	if got.Name != "main" {
		t.Errorf("Get().Name = %q, want %q", got.Name, "main")
	}
	if len(got.Brokers) != 1 || got.Brokers[0] != "localhost:9092" {
		t.Errorf("Get().Brokers = %v, want [localhost:9092]", got.Brokers)
	}
}

func TestRegistry_GetNotFound(t *testing.T) {
	r := NewRegistry()

	_, ok := r.Get("nonexistent")
	if ok {
		t.Error("Get() returned true for nonexistent cluster")
	}
}

func TestRegistry_RegisterValidationError(t *testing.T) {
	r := NewRegistry()

	cfg := &ClusterConfig{} // missing brokers

	err := r.Register("bad", cfg)
	if err == nil {
		t.Fatal("Register() error = nil, want validation error")
	}
}

func TestRegistry_Has(t *testing.T) {
	r := NewRegistry()

	cfg := &ClusterConfig{
		Brokers: []string{"localhost:9092"},
	}
	_ = r.Register("main", cfg)

	if !r.Has("main") {
		t.Error("Has(main) = false, want true")
	}
	if r.Has("other") {
		t.Error("Has(other) = true, want false")
	}
}

func TestRegistry_Names(t *testing.T) {
	r := NewRegistry()

	_ = r.Register("alpha", &ClusterConfig{Brokers: []string{"a:9092"}})
	_ = r.Register("beta", &ClusterConfig{Brokers: []string{"b:9092"}})

	names := r.Names()
	sort.Strings(names)

	if len(names) != 2 {
		t.Fatalf("Names() len = %d, want 2", len(names))
	}
	if names[0] != "alpha" || names[1] != "beta" {
		t.Errorf("Names() = %v, want [alpha beta]", names)
	}
}

func TestRegistry_LoadFromMap(t *testing.T) {
	r := NewRegistry()

	clusters := map[string]ClusterConfig{
		"main": {Brokers: []string{"main:9092"}},
		"logs": {Brokers: []string{"logs:9092"}},
	}

	if err := r.LoadFromMap(clusters); err != nil {
		t.Fatalf("LoadFromMap() error = %v", err)
	}

	if !r.Has("main") {
		t.Error("main cluster not registered")
	}
	if !r.Has("logs") {
		t.Error("logs cluster not registered")
	}
}

func TestRegistry_LoadFromMapValidationError(t *testing.T) {
	r := NewRegistry()

	clusters := map[string]ClusterConfig{
		"good": {Brokers: []string{"good:9092"}},
		"bad":  {}, // missing brokers
	}

	err := r.LoadFromMap(clusters)
	if err == nil {
		t.Fatal("LoadFromMap() error = nil, want validation error")
	}
}
