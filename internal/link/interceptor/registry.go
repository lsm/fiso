// Package interceptor provides interceptor chain management for Fiso-Link.
// It manages WASM-based interceptors that can process requests in outbound
// (before upstream) and inbound (after response) phases.
package interceptor

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/lsm/fiso/internal/interceptor"
	wasminterceptor "github.com/lsm/fiso/internal/interceptor/wasm"
	"github.com/lsm/fiso/internal/link"
)

// Phase indicates when an interceptor should run.
type Phase string

const (
	// PhaseOutbound runs the interceptor before sending to upstream/target.
	PhaseOutbound Phase = "outbound"
	// PhaseInbound runs the interceptor after receiving response from upstream.
	PhaseInbound Phase = "inbound"
)

// TargetChains holds the interceptor chains for a single target.
type TargetChains struct {
	Outbound *interceptor.Chain
	Inbound  *interceptor.Chain
}

// MetricsRecorder records interceptor metrics.
type MetricsRecorder interface {
	RecordInterceptorInvocation(target, module, phase string, success bool, durationSeconds float64)
}

// Registry manages interceptor chains per target.
type Registry struct {
	mu       sync.RWMutex
	chains   map[string]*TargetChains // target name -> chains
	metrics  MetricsRecorder
	logger   *slog.Logger
	runtimes []interceptor.Interceptor // All runtimes for cleanup
}

// NewRegistry creates a new interceptor registry.
func NewRegistry(metrics MetricsRecorder, logger *slog.Logger) *Registry {
	if logger == nil {
		logger = slog.Default()
	}
	return &Registry{
		chains:  make(map[string]*TargetChains),
		metrics: metrics,
		logger:  logger,
	}
}

// Load builds interceptor chains for all targets from configuration.
func (r *Registry) Load(ctx context.Context, targets []link.LinkTarget) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Close existing runtimes
	for _, rt := range r.runtimes {
		if err := rt.Close(); err != nil {
			r.logger.Warn("failed to close interceptor runtime", "error", err)
		}
	}
	r.runtimes = nil

	// Clear existing chains
	r.chains = make(map[string]*TargetChains)

	for _, target := range targets {
		if len(target.Interceptors) == 0 {
			continue
		}

		chains, runtimes, err := r.buildChainsForTarget(ctx, target)
		if err != nil {
			return fmt.Errorf("target %q: %w", target.Name, err)
		}

		r.chains[target.Name] = chains
		r.runtimes = append(r.runtimes, runtimes...)

		r.logger.Info("loaded interceptor chains for target",
			"target", target.Name,
			"outbound_count", chains.Outbound.Len(),
			"inbound_count", chains.Inbound.Len(),
		)
	}

	return nil
}

// buildChainsForTarget creates interceptor chains for a single target.
func (r *Registry) buildChainsForTarget(ctx context.Context, target link.LinkTarget) (*TargetChains, []interceptor.Interceptor, error) {
	var outboundInterceptors []interceptor.Interceptor
	var inboundInterceptors []interceptor.Interceptor
	var runtimes []interceptor.Interceptor

	for i, ic := range target.Interceptors {
		icInterceptor, err := r.createInterceptor(ctx, ic, i)
		if err != nil {
			// Close any runtimes we've created so far
			for _, rt := range runtimes {
				_ = rt.Close()
			}
			return nil, nil, err
		}
		runtimes = append(runtimes, icInterceptor)

		// Determine phase
		phase := PhaseOutbound
		if phaseVal, ok := ic.Config["phase"]; ok {
			if phaseStr, ok := phaseVal.(string); ok {
				phase = Phase(phaseStr)
			}
		}

		switch phase {
		case PhaseOutbound:
			outboundInterceptors = append(outboundInterceptors, icInterceptor)
		case PhaseInbound:
			inboundInterceptors = append(inboundInterceptors, icInterceptor)
		default:
			// Default to outbound
			outboundInterceptors = append(outboundInterceptors, icInterceptor)
		}
	}

	return &TargetChains{
		Outbound: interceptor.NewChain(outboundInterceptors...),
		Inbound:  interceptor.NewChain(inboundInterceptors...),
	}, runtimes, nil
}

// createInterceptor creates an interceptor from configuration.
func (r *Registry) createInterceptor(ctx context.Context, ic link.InterceptorConfig, index int) (interceptor.Interceptor, error) {
	switch ic.Type {
	case "wasm":
		return r.createWASMInterceptor(ctx, ic.Config, index)
	default:
		return nil, fmt.Errorf("interceptor[%d]: unknown type %q", index, ic.Type)
	}
}

// createWASMInterceptor creates a WASM-based interceptor.
func (r *Registry) createWASMInterceptor(ctx context.Context, config map[string]interface{}, index int) (*InterceptorWrapper, error) {
	// Extract module path
	modulePath, ok := config["module"].(string)
	if !ok {
		return nil, fmt.Errorf("interceptor[%d]: wasm config missing 'module' field", index)
	}

	// Read WASM module from file
	wasmBytes, err := os.ReadFile(modulePath)
	if err != nil {
		return nil, fmt.Errorf("interceptor[%d]: read wasm module %q: %w", index, modulePath, err)
	}

	// Create wazero runtime
	runtime, err := wasminterceptor.NewWazeroRuntime(ctx, wasmBytes)
	if err != nil {
		return nil, fmt.Errorf("interceptor[%d]: create wasm runtime for %q: %w", index, modulePath, err)
	}

	// Extract failOpen (default false)
	failOpen := false
	if failOpenVal, ok := config["failOpen"].(bool); ok {
		failOpen = failOpenVal
	}

	// Create WASM interceptor
	wasmIc := wasminterceptor.New(runtime, modulePath)

	return &InterceptorWrapper{
		Interceptor: wasmIc,
		module:      modulePath,
		failOpen:    failOpen,
		metrics:     r.metrics,
	}, nil
}

// GetChains returns the interceptor chains for a target.
// Returns nil if no interceptors are configured for the target.
func (r *Registry) GetChains(targetName string) *TargetChains {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.chains[targetName]
}

// ProcessOutbound runs the outbound interceptor chain for a target.
// Returns the modified request, or an error. If no chain is configured,
// returns the original request unchanged.
func (r *Registry) ProcessOutbound(ctx context.Context, targetName string, req *interceptor.Request) (*interceptor.Request, error) {
	chains := r.GetChains(targetName)
	if chains == nil || chains.Outbound == nil || chains.Outbound.Len() == 0 {
		return req, nil
	}
	return chains.Outbound.Process(ctx, req)
}

// ProcessInbound runs the inbound interceptor chain for a target.
// Returns the modified request, or an error. If no chain is configured,
// returns the original request unchanged.
func (r *Registry) ProcessInbound(ctx context.Context, targetName string, req *interceptor.Request) (*interceptor.Request, error) {
	chains := r.GetChains(targetName)
	if chains == nil || chains.Inbound == nil || chains.Inbound.Len() == 0 {
		return req, nil
	}
	return chains.Inbound.Process(ctx, req)
}

// Close closes all interceptor runtimes.
func (r *Registry) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	var firstErr error
	for _, rt := range r.runtimes {
		if err := rt.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	r.runtimes = nil
	r.chains = make(map[string]*TargetChains)
	return firstErr
}

// InterceptorWrapper wraps an interceptor with metrics and failOpen behavior.
type InterceptorWrapper struct {
	interceptor.Interceptor
	module   string
	failOpen bool
	metrics  MetricsRecorder
}

// Process invokes the wrapped interceptor with metrics recording.
func (w *InterceptorWrapper) Process(ctx context.Context, req *interceptor.Request) (*interceptor.Request, error) {
	start := time.Now()

	result, err := w.Interceptor.Process(ctx, req)

	duration := time.Since(start).Seconds()
	success := err == nil

	if w.metrics != nil {
		w.metrics.RecordInterceptorInvocation(
			"", // Target not available here
			w.module,
			string(req.Direction),
			success,
			duration,
		)
	}

	if err != nil && w.failOpen {
		// Log error but continue with original request
		slog.Warn("interceptor failed but failOpen is true, continuing",
			"module", w.module,
			"error", err,
		)
		return req, nil
	}

	return result, err
}
