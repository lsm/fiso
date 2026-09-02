package proxy

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	kafka "github.com/lsm/fiso/internal/kafka"
	"github.com/lsm/fiso/internal/link"
	linkinterceptor "github.com/lsm/fiso/internal/link/interceptor"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"log/slog"
)

// buildRejectFixture compiles the shared rejection test module (ADR 0007)
// to a wasip1 .wasm and returns its path.
func buildRejectFixture(t *testing.T) string {
	t.Helper()
	outPath := filepath.Join(t.TempDir(), "reject.wasm")
	cmd := exec.Command("go", "build", "-o", outPath, ".")
	cmd.Dir = filepath.Join("..", "..", "interceptor", "wasm", "testdata", "reject")
	cmd.Env = append(os.Environ(), "GOOS=wasip1", "GOARCH=wasm")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("compile wasm module: %v\n%s", err, out)
	}
	return outPath
}

// TestProxy_OutboundInterceptorRejection_MapsStatus rounds the whole
// rejection path through a real guest: the wasm module refuses the
// unauthenticated request and Link answers with the guest-chosen status
// instead of a blanket 500; the authorized request passes to the target
// (ADR 0007).
func TestProxy_OutboundInterceptorRejection_MapsStatus(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()

	host := strings.TrimPrefix(upstream.URL, "http://")
	module := buildRejectFixture(t)

	store := link.NewTargetStore([]link.LinkTarget{
		{
			Name:     "svc",
			Protocol: "http",
			Host:     host,
			Interceptors: []link.InterceptorConfig{
				{Type: "wasm", Config: map[string]interface{}{"module": module}},
			},
		},
	})

	icRegistry := linkinterceptor.NewRegistry(nil, slog.Default())
	defer func() { _ = icRegistry.Close() }()
	if err := icRegistry.Load(context.Background(), []link.LinkTarget{
		{
			Name:     "svc",
			Protocol: "http",
			Host:     host,
			Interceptors: []link.InterceptorConfig{
				{Type: "wasm", Config: map[string]interface{}{"module": module}},
			},
		},
	}); err != nil {
		t.Fatalf("load interceptor chains: %v", err)
	}

	handler := NewHandler(Config{
		Targets:      store,
		Metrics:      link.NewMetrics(prometheus.NewRegistry()),
		Interceptors: icRegistry,
	})

	// Unauthenticated: the module rejects; the caller sees its status+reason.
	req := httptest.NewRequest("POST", "/link/svc/x", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 from the rejecting module, got %d (body %q)", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "missing credentials") {
		t.Fatalf("expected the rejection reason in the body, got %q", w.Body.String())
	}

	// Authorized: passes through to the target.
	req = httptest.NewRequest("POST", "/link/svc/x", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer token")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected the authorized request to pass, got %d (body %q)", w.Code, w.Body.String())
	}
}

// TestProxy_OutboundInterceptorRejection_BodylessRequest pins that outbound
// interceptors run for bodyless requests too: an authentication module must
// be able to refuse a GET, not only a POST with a body (ADR 0007).
func TestProxy_OutboundInterceptorRejection_BodylessRequest(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer upstream.Close()

	host := strings.TrimPrefix(upstream.URL, "http://")
	module := buildRejectFixture(t)

	targets := []link.LinkTarget{
		{
			Name:     "svc",
			Protocol: "http",
			Host:     host,
			Interceptors: []link.InterceptorConfig{
				{Type: "wasm", Config: map[string]interface{}{"module": module}},
			},
		},
	}
	store := link.NewTargetStore(targets)

	icRegistry := linkinterceptor.NewRegistry(nil, slog.Default())
	defer func() { _ = icRegistry.Close() }()
	if err := icRegistry.Load(context.Background(), targets); err != nil {
		t.Fatalf("load interceptor chains: %v", err)
	}

	handler := NewHandler(Config{
		Targets:      store,
		Metrics:      link.NewMetrics(prometheus.NewRegistry()),
		Interceptors: icRegistry,
	})

	// Bodyless GET: the module must still see (and refuse) the request.
	req := httptest.NewRequest("GET", "/link/svc/x", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected the bodyless request to be refused with 401, got %d", w.Code)
	}

	// Authorized bodyless GET passes.
	req = httptest.NewRequest("GET", "/link/svc/x", nil)
	req.Header.Set("Authorization", "Bearer token")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected the authorized bodyless request to pass, got %d", w.Code)
	}
}

// TestProxy_KafkaTarget_Rejection_MapsStatus pins the rejection mapping for
// kafka-protocol Link targets: the publish proxy answers a refusal with the
// module-chosen status instead of a blanket 500 (ADR 0007).
func TestProxy_KafkaTarget_Rejection_MapsStatus(t *testing.T) {
	module := buildRejectFixture(t)

	targets := []link.LinkTarget{
		{
			Name:     "events",
			Protocol: "kafka",
			Kafka:    &link.KafkaConfig{Cluster: "local", Topic: "events"},
			Interceptors: []link.InterceptorConfig{
				{Type: "wasm", Config: map[string]interface{}{"module": module}},
			},
		},
	}
	store := link.NewTargetStore(targets)

	icRegistry := linkinterceptor.NewRegistry(nil, slog.Default())
	defer func() { _ = icRegistry.Close() }()
	if err := icRegistry.Load(context.Background(), targets); err != nil {
		t.Fatalf("load interceptor chains: %v", err)
	}

	handler := NewKafkaHandlerWithInterceptors(
		kafka.NewPublisherPool(kafka.NewRegistry()),
		store,
		nil,
		nil,
		link.NewMetrics(prometheus.NewRegistry()),
		slog.Default(),
		icRegistry,
	)

	req := httptest.NewRequest("POST", "/link/events", strings.NewReader(`{"msg":1}`))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 from the rejecting module, got %d (body %q)", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "missing credentials") {
		t.Fatalf("expected the rejection reason in the body, got %q", w.Body.String())
	}
}

// TestProxy_InboundInterceptorRejection_MapsStatus pins the response-side
// mapping: an inbound-phase module can refuse an upstream response and the
// caller sees the guest-chosen status (ADR 0007).
func TestProxy_InboundInterceptorRejection_MapsStatus(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"response":true}`))
	}))
	defer upstream.Close()

	host := strings.TrimPrefix(upstream.URL, "http://")
	module := buildRejectFixture(t)

	targets := []link.LinkTarget{
		{
			Name:     "svc",
			Protocol: "http",
			Host:     host,
			Interceptors: []link.InterceptorConfig{
				{Type: "wasm", Config: map[string]interface{}{"module": module, "phase": "inbound"}},
			},
		},
	}
	store := link.NewTargetStore(targets)

	icRegistry := linkinterceptor.NewRegistry(nil, slog.Default())
	defer func() { _ = icRegistry.Close() }()
	if err := icRegistry.Load(context.Background(), targets); err != nil {
		t.Fatalf("load interceptor chains: %v", err)
	}

	handler := NewHandler(Config{
		Targets:      store,
		Metrics:      link.NewMetrics(prometheus.NewRegistry()),
		Interceptors: icRegistry,
	})

	// The upstream response carries no Authorization header, so the
	// inbound module refuses it.
	req := httptest.NewRequest("POST", "/link/svc/x", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer token")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected the refused response to surface as 401, got %d (body %q)", w.Code, w.Body.String())
	}
}

// countingRegistry wraps a prometheus registry the tests can read counters
// from for rejection-metric assertions.
func rejectionCount(t *testing.T, metrics *link.Metrics, target, method, status string) float64 {
	t.Helper()
	return testutil.ToFloat64(metrics.RequestsTotal.WithLabelValues(target, method, status, "sync"))
}

// TestProxy_OutboundRejection_RecordedInMetrics pins that an outbound
// rejection is counted with the guest-chosen status — authentication
// verdicts must appear on the request-rate and status dashboards, matching
// the kafka path (ADR 0007).
func TestProxy_OutboundRejection_RecordedInMetrics(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	host := strings.TrimPrefix(upstream.URL, "http://")
	module := buildRejectFixture(t)

	targets := []link.LinkTarget{
		{
			Name:     "svc",
			Protocol: "http",
			Host:     host,
			Interceptors: []link.InterceptorConfig{
				{Type: "wasm", Config: map[string]interface{}{"module": module}},
			},
		},
	}
	store := link.NewTargetStore(targets)
	icRegistry := linkinterceptor.NewRegistry(nil, slog.Default())
	defer func() { _ = icRegistry.Close() }()
	if err := icRegistry.Load(context.Background(), targets); err != nil {
		t.Fatalf("load interceptor chains: %v", err)
	}

	metrics := link.NewMetrics(prometheus.NewRegistry())
	handler := NewHandler(Config{Targets: store, Metrics: metrics, Interceptors: icRegistry})

	req := httptest.NewRequest("GET", "/link/svc/x", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
	if got := rejectionCount(t, metrics, "svc", "GET", "401"); got != 1 {
		t.Fatalf("expected the 401 rejection to be recorded once, got %v", got)
	}
}

// TestProxy_InboundRejection_ReportsFinalStatus pins that a guest turning an
// upstream 200 into a caller-visible 401 is reported as 401 in request
// metrics — not as a successful 200 (ADR 0007).
func TestProxy_InboundRejection_ReportsFinalStatus(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"response":true}`))
	}))
	defer upstream.Close()

	host := strings.TrimPrefix(upstream.URL, "http://")
	module := buildRejectFixture(t)

	targets := []link.LinkTarget{
		{
			Name:     "svc",
			Protocol: "http",
			Host:     host,
			Interceptors: []link.InterceptorConfig{
				{Type: "wasm", Config: map[string]interface{}{"module": module, "phase": "inbound"}},
			},
		},
	}
	store := link.NewTargetStore(targets)
	icRegistry := linkinterceptor.NewRegistry(nil, slog.Default())
	defer func() { _ = icRegistry.Close() }()
	if err := icRegistry.Load(context.Background(), targets); err != nil {
		t.Fatalf("load interceptor chains: %v", err)
	}

	metrics := link.NewMetrics(prometheus.NewRegistry())
	handler := NewHandler(Config{Targets: store, Metrics: metrics, Interceptors: icRegistry})

	req := httptest.NewRequest("POST", "/link/svc/x", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer token")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
	if got := rejectionCount(t, metrics, "svc", "POST", "200"); got != 0 {
		t.Fatalf("the rewritten response must not be counted as 200, got %v", got)
	}
	if got := rejectionCount(t, metrics, "svc", "POST", "401"); got != 1 {
		t.Fatalf("expected the final 401 to be recorded once, got %v", got)
	}
}

// TestProxy_InboundRejection_OnUpstreamErrorResponse pins that inbound
// interceptors also see upstream error responses: a policy module can
// refuse to forward a 5xx body instead of the retry path bypassing it
// (ADR 0007).
func TestProxy_InboundRejection_OnUpstreamErrorResponse(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer upstream.Close()

	host := strings.TrimPrefix(upstream.URL, "http://")
	module := buildRejectFixture(t)

	targets := []link.LinkTarget{
		{
			Name:     "svc",
			Protocol: "http",
			Host:     host,
			Retry:    link.RetryConfig{MaxAttempts: 1},
			Interceptors: []link.InterceptorConfig{
				{Type: "wasm", Config: map[string]interface{}{"module": module, "phase": "inbound"}},
			},
		},
	}
	store := link.NewTargetStore(targets)
	icRegistry := linkinterceptor.NewRegistry(nil, slog.Default())
	defer func() { _ = icRegistry.Close() }()
	if err := icRegistry.Load(context.Background(), targets); err != nil {
		t.Fatalf("load interceptor chains: %v", err)
	}

	metrics := link.NewMetrics(prometheus.NewRegistry())
	handler := NewHandler(Config{Targets: store, Metrics: metrics, Interceptors: icRegistry})

	req := httptest.NewRequest("POST", "/link/svc/x", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer token")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected the inbound module's 401 to win over the upstream 500, got %d", w.Code)
	}
	if got := rejectionCount(t, metrics, "svc", "POST", "401"); got != 1 {
		t.Fatalf("expected the final 401 to be recorded once, got %v", got)
	}
}

// TestProxy_InboundRejection_SpanMarkedError pins the tracing half of the
// final-status contract: a guest turning an upstream 200 into a 401 must
// leave the proxy span marked as an error, not Ok (ADR 0007).
func TestProxy_InboundRejection_SpanMarkedError(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"response":true}`))
	}))
	defer upstream.Close()

	host := strings.TrimPrefix(upstream.URL, "http://")
	module := buildRejectFixture(t)

	targets := []link.LinkTarget{
		{
			Name:     "svc",
			Protocol: "http",
			Host:     host,
			Interceptors: []link.InterceptorConfig{
				{Type: "wasm", Config: map[string]interface{}{"module": module, "phase": "inbound"}},
			},
		},
	}
	store := link.NewTargetStore(targets)
	icRegistry := linkinterceptor.NewRegistry(nil, slog.Default())
	defer func() { _ = icRegistry.Close() }()
	if err := icRegistry.Load(context.Background(), targets); err != nil {
		t.Fatalf("load interceptor chains: %v", err)
	}

	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	handler := NewHandler(Config{Targets: store, Metrics: link.NewMetrics(prometheus.NewRegistry()), Interceptors: icRegistry})
	handler.SetTracer(tp.Tracer("test"))
	defer func() { _ = tp.Shutdown(context.Background()) }()

	req := httptest.NewRequest("POST", "/link/svc/x", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer token")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}

	for _, span := range sr.Ended() {
		if span.Name() == "fiso.proxy.request" {
			if span.Status().Code != codes.Error {
				t.Fatalf("the rewritten-to-401 response must mark the proxy span as an error, got %v", span.Status().Code)
			}
			return
		}
	}
	t.Fatal("no proxy span recorded")
}

// trackedBody records whether it was drained to EOF before Close — closing
// an undrained HTTP/1.x body forfeits the keep-alive connection.
type trackedBody struct {
	reader  *bytes.Reader
	eofRead bool
	closed  bool
}

func (b *trackedBody) Read(p []byte) (int, error) {
	n, err := b.reader.Read(p)
	if err == io.EOF {
		b.eofRead = true
	}
	return n, err
}

func (b *trackedBody) Close() error { b.closed = true; return nil }

// TestProxy_RetryDrainsIntermediateResponses pins that retried attempts
// drain the previous response body to EOF before closing it, keeping the
// transport's keep-alive connections reusable (the final attempt's body
// stays forwardable).
func TestProxy_RetryDrainsIntermediateResponses(t *testing.T) {
	var attempts int32
	var first *trackedBody
	rt := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		n := atomic.AddInt32(&attempts, 1)
		body := &trackedBody{reader: bytes.NewReader([]byte(`{"err":true}`))}
		if n == 1 {
			first = body
		}
		status := http.StatusInternalServerError
		if n > 1 {
			status = http.StatusOK
			body = &trackedBody{reader: bytes.NewReader([]byte(`{"ok":true}`))}
		}
		return &http.Response{
			StatusCode:    status,
			Header:        http.Header{},
			Body:          body,
			ContentLength: int64(body.reader.Len()),
		}, nil
	})

	store := link.NewTargetStore([]link.LinkTarget{
		{Name: "svc", Protocol: "http", Host: "upstream.test", Retry: link.RetryConfig{MaxAttempts: 2}},
	})
	handler := NewHandler(Config{Targets: store, Metrics: link.NewMetrics(prometheus.NewRegistry())})
	handler.client = &http.Client{Transport: rt}

	req := httptest.NewRequest("POST", "/link/svc/x", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected the retried request to succeed, got %d", w.Code)
	}
	if first == nil || !first.closed {
		t.Fatal("the intermediate response must be closed")
	}
	if !first.eofRead {
		t.Fatal("the intermediate response must be drained to EOF before close (keep-alive reuse)")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// TestProxy_OutboundRejection_SpanMarkedError pins the tracing for outbound
// rejections: the span carries the guest status attribute and is marked as
// an error, mirroring the inbound path (ADR 0007).
func TestProxy_OutboundRejection_SpanMarkedError(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	host := strings.TrimPrefix(upstream.URL, "http://")
	module := buildRejectFixture(t)

	targets := []link.LinkTarget{
		{
			Name:     "svc",
			Protocol: "http",
			Host:     host,
			Interceptors: []link.InterceptorConfig{
				{Type: "wasm", Config: map[string]interface{}{"module": module}},
			},
		},
	}
	store := link.NewTargetStore(targets)
	icRegistry := linkinterceptor.NewRegistry(nil, slog.Default())
	defer func() { _ = icRegistry.Close() }()
	if err := icRegistry.Load(context.Background(), targets); err != nil {
		t.Fatalf("load interceptor chains: %v", err)
	}

	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	handler := NewHandler(Config{Targets: store, Metrics: link.NewMetrics(prometheus.NewRegistry()), Interceptors: icRegistry})
	handler.SetTracer(tp.Tracer("test"))
	defer func() { _ = tp.Shutdown(context.Background()) }()

	req := httptest.NewRequest("GET", "/link/svc/x", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}

	for _, span := range sr.Ended() {
		if span.Name() == "fiso.proxy.request" {
			if span.Status().Code != codes.Error {
				t.Fatalf("an outbound rejection must mark the proxy span as an error, got %v", span.Status().Code)
			}
			return
		}
	}
	t.Fatal("no proxy span recorded")
}

// TestProxy_ErrorResponse_StreamsWithoutInboundChain pins the unbuffered
// error-forwarding path: a target with no inbound interceptors forwards the
// upstream error response verbatim without buffering it.
func TestProxy_ErrorResponse_StreamsWithoutInboundChain(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "upstream exploded", http.StatusBadGateway)
	}))
	defer upstream.Close()

	host := strings.TrimPrefix(upstream.URL, "http://")
	store := link.NewTargetStore([]link.LinkTarget{
		{Name: "svc", Protocol: "http", Host: host, Retry: link.RetryConfig{MaxAttempts: 1}},
	})
	metrics := link.NewMetrics(prometheus.NewRegistry())
	handler := NewHandler(Config{Targets: store, Metrics: metrics})

	req := httptest.NewRequest("POST", "/link/svc/x", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("expected the upstream 502 forwarded, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "upstream exploded") {
		t.Fatalf("expected the error body forwarded verbatim, got %q", w.Body.String())
	}
	if got := rejectionCount(t, metrics, "svc", "POST", "502"); got != 1 {
		t.Fatalf("expected the 502 recorded once, got %v", got)
	}
}

// TestProxy_OutboundRejection_EmitsCompletionLog pins that an outbound
// rejection emits the same correlation-aware "proxy request completed"
// record as the success path, so verdicts join request logs (ADR 0007).
func TestProxy_OutboundRejection_EmitsCompletionLog(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	host := strings.TrimPrefix(upstream.URL, "http://")
	module := buildRejectFixture(t)

	targets := []link.LinkTarget{
		{
			Name:     "svc",
			Protocol: "http",
			Host:     host,
			Interceptors: []link.InterceptorConfig{
				{Type: "wasm", Config: map[string]interface{}{"module": module}},
			},
		},
	}
	store := link.NewTargetStore(targets)
	icRegistry := linkinterceptor.NewRegistry(nil, slog.Default())
	defer func() { _ = icRegistry.Close() }()
	if err := icRegistry.Load(context.Background(), targets); err != nil {
		t.Fatalf("load interceptor chains: %v", err)
	}

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	handler := NewHandler(Config{
		Targets:      store,
		Metrics:      link.NewMetrics(prometheus.NewRegistry()),
		Interceptors: icRegistry,
		Logger:       logger,
	})

	req := httptest.NewRequest("GET", "/link/svc/x", nil)
	req.Header.Set("x-correlation-id", "corr-outbound-9")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}

	logs := buf.String()
	if !strings.Contains(logs, "proxy request completed") {
		t.Fatalf("the rejection must emit the completion record, got:\n%s", logs)
	}
	if !strings.Contains(logs, "corr-outbound-9") {
		t.Fatalf("the completion record must carry the correlation ID, got:\n%s", logs)
	}
}

// TestProxy_KafkaRejection_LogsCorrelationID pins that the kafka publish
// proxy's rejection verdict carries the resolved correlation ID (ADR 0007).
func TestProxy_KafkaRejection_LogsCorrelationID(t *testing.T) {
	module := buildRejectFixture(t)

	targets := []link.LinkTarget{
		{
			Name:     "events",
			Protocol: "kafka",
			Kafka:    &link.KafkaConfig{Cluster: "local", Topic: "events"},
			Interceptors: []link.InterceptorConfig{
				{Type: "wasm", Config: map[string]interface{}{"module": module}},
			},
		},
	}
	store := link.NewTargetStore(targets)
	icRegistry := linkinterceptor.NewRegistry(nil, slog.Default())
	defer func() { _ = icRegistry.Close() }()
	if err := icRegistry.Load(context.Background(), targets); err != nil {
		t.Fatalf("load interceptor chains: %v", err)
	}

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	handler := NewKafkaHandlerWithInterceptors(
		kafka.NewPublisherPool(kafka.NewRegistry()),
		store,
		nil,
		nil,
		link.NewMetrics(prometheus.NewRegistry()),
		logger,
		icRegistry,
	)

	req := httptest.NewRequest("POST", "/link/events", strings.NewReader(`{"msg":1}`))
	req.Header.Set("x-correlation-id", "corr-kafka-7")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
	if !strings.Contains(buf.String(), "corr-kafka-7") {
		t.Fatalf("the kafka rejection verdict must carry the correlation ID, got:\n%s", buf.String())
	}
}

// TestProxy_TransportFailure_LabeledWithReturnedStatus pins that a total
// transport failure (connection refused) is counted under the 502 the caller
// receives, not a generic "error" label (ADR 0007 observability).
func TestProxy_TransportFailure_LabeledWithReturnedStatus(t *testing.T) {
	store := link.NewTargetStore([]link.LinkTarget{
		{Name: "svc", Protocol: "http", Host: "127.0.0.1:1", Retry: link.RetryConfig{MaxAttempts: 1}},
	})
	metrics := link.NewMetrics(prometheus.NewRegistry())
	handler := NewHandler(Config{Targets: store, Metrics: metrics})

	req := httptest.NewRequest("POST", "/link/svc/x", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", w.Code)
	}
	if got := rejectionCount(t, metrics, "svc", "POST", "502"); got != 1 {
		t.Fatalf("expected the 502 recorded once, got %v", got)
	}
	if got := rejectionCount(t, metrics, "svc", "POST", "error"); got != 0 {
		t.Fatalf("the generic error label must not be used, got %v", got)
	}
}
