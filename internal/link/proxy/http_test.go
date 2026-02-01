package proxy

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/lsm/fiso/internal/link"
	"github.com/lsm/fiso/internal/link/auth"
	"github.com/lsm/fiso/internal/link/circuitbreaker"
	"github.com/lsm/fiso/internal/link/discovery"
)

func setupProxy(t *testing.T, upstream *httptest.Server, targets []link.LinkTarget, breakers map[string]*circuitbreaker.Breaker, authProvider auth.Provider) *Handler {
	t.Helper()
	store := link.NewTargetStore(targets)
	if breakers == nil {
		breakers = make(map[string]*circuitbreaker.Breaker)
	}
	if authProvider == nil {
		authProvider = &auth.NoopProvider{}
	}
	reg := prometheus.NewRegistry()
	return NewHandler(Config{
		Targets:  store,
		Breakers: breakers,
		Auth:     authProvider,
		Resolver: &discovery.StaticResolver{},
		Metrics:  link.NewMetrics(reg),
	})
}

func TestProxy_Success(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Custom", "value")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("hello"))
	}))
	defer upstream.Close()

	host := strings.TrimPrefix(upstream.URL, "http://")
	handler := setupProxy(t, upstream, []link.LinkTarget{
		{Name: "svc", Protocol: "http", Host: host},
	}, nil, nil)

	req := httptest.NewRequest("GET", "/link/svc/api/test", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if body != "hello" {
		t.Errorf("expected hello, got %s", body)
	}
	if w.Header().Get("X-Custom") != "value" {
		t.Errorf("expected X-Custom header")
	}
}

func TestProxy_TargetNotFound(t *testing.T) {
	handler := setupProxy(t, nil, nil, nil, nil)

	req := httptest.NewRequest("GET", "/link/unknown/path", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestProxy_InvalidRoute(t *testing.T) {
	handler := setupProxy(t, nil, nil, nil, nil)

	req := httptest.NewRequest("GET", "/other/path", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestProxy_CircuitBreakerOpen(t *testing.T) {
	breaker := circuitbreaker.New(circuitbreaker.Config{
		FailureThreshold: 1, SuccessThreshold: 1, ResetTimeout: 1000000000,
	})
	_ = breaker.Allow()
	breaker.RecordFailure()

	handler := setupProxy(t, nil, []link.LinkTarget{
		{Name: "svc", Protocol: "http", Host: "localhost:1234"},
	}, map[string]*circuitbreaker.Breaker{"svc": breaker}, nil)

	req := httptest.NewRequest("GET", "/link/svc/test", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", w.Code)
	}
	if w.Header().Get("Retry-After") == "" {
		t.Error("expected Retry-After header")
	}
}

func TestProxy_AuthHeaderInjection(t *testing.T) {
	var gotAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	host := strings.TrimPrefix(upstream.URL, "http://")
	provider := &mockAuthProvider{
		creds: &auth.Credentials{
			Type:    "Bearer",
			Token:   "test-token",
			Headers: map[string]string{"Authorization": "Bearer test-token"},
		},
	}

	handler := setupProxy(t, upstream, []link.LinkTarget{
		{Name: "svc", Protocol: "http", Host: host},
	}, nil, provider)

	req := httptest.NewRequest("GET", "/link/svc/test", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if gotAuth != "Bearer test-token" {
		t.Errorf("expected Bearer test-token, got %s", gotAuth)
	}
}

func TestProxy_PathAllowlisting(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	host := strings.TrimPrefix(upstream.URL, "http://")
	handler := setupProxy(t, upstream, []link.LinkTarget{
		{Name: "svc", Protocol: "http", Host: host, AllowedPaths: []string{"/api/v2/**"}},
	}, nil, nil)

	// Allowed path
	req := httptest.NewRequest("GET", "/link/svc/api/v2/users", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for allowed path, got %d", w.Code)
	}

	// Blocked path
	req = httptest.NewRequest("GET", "/link/svc/admin/secret", nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 for blocked path, got %d", w.Code)
	}
}

func TestProxy_UpstreamServerError(t *testing.T) {
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("bad gateway"))
	}))
	defer upstream.Close()

	host := strings.TrimPrefix(upstream.URL, "http://")
	handler := setupProxy(t, upstream, []link.LinkTarget{
		{Name: "svc", Protocol: "http", Host: host, Retry: link.RetryConfig{
			MaxAttempts: 2, InitialInterval: "1ms", MaxInterval: "10ms",
		}},
	}, nil, nil)

	req := httptest.NewRequest("GET", "/link/svc/test", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Should have retried
	if calls < 2 {
		t.Errorf("expected at least 2 calls (retry), got %d", calls)
	}
}

func TestProxy_UpstreamClientError_NoRetry(t *testing.T) {
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusNotFound)
	}))
	defer upstream.Close()

	host := strings.TrimPrefix(upstream.URL, "http://")
	handler := setupProxy(t, upstream, []link.LinkTarget{
		{Name: "svc", Protocol: "http", Host: host, Retry: link.RetryConfig{
			MaxAttempts: 3, InitialInterval: "1ms", MaxInterval: "10ms",
		}},
	}, nil, nil)

	req := httptest.NewRequest("GET", "/link/svc/test", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Should NOT retry on 4xx
	if calls != 1 {
		t.Errorf("expected 1 call (no retry on 4xx), got %d", calls)
	}
}

func TestProxy_QueryStringForwarding(t *testing.T) {
	var gotQuery string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	host := strings.TrimPrefix(upstream.URL, "http://")
	handler := setupProxy(t, upstream, []link.LinkTarget{
		{Name: "svc", Protocol: "http", Host: host},
	}, nil, nil)

	req := httptest.NewRequest("GET", "/link/svc/search?q=hello&page=1", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if gotQuery != "q=hello&page=1" {
		t.Errorf("expected query string forwarded, got %s", gotQuery)
	}
}

func TestProxy_PostBody(t *testing.T) {
	var gotBody string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		w.WriteHeader(http.StatusCreated)
	}))
	defer upstream.Close()

	host := strings.TrimPrefix(upstream.URL, "http://")
	handler := setupProxy(t, upstream, []link.LinkTarget{
		{Name: "svc", Protocol: "http", Host: host},
	}, nil, nil)

	req := httptest.NewRequest("POST", "/link/svc/data", strings.NewReader(`{"key":"value"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("expected 201, got %d", w.Code)
	}
	if gotBody != `{"key":"value"}` {
		t.Errorf("expected body forwarded, got %s", gotBody)
	}
}

// mockAuthProvider implements auth.Provider for testing.
type mockAuthProvider struct {
	creds *auth.Credentials
	err   error
}

func (m *mockAuthProvider) GetCredentials(_ context.Context, _ string) (*auth.Credentials, error) {
	return m.creds, m.err
}

func TestProxy_AuthError(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	host := strings.TrimPrefix(upstream.URL, "http://")
	provider := &mockAuthProvider{err: fmt.Errorf("auth failed")}
	handler := setupProxy(t, upstream, []link.LinkTarget{
		{Name: "svc", Protocol: "http", Host: host},
	}, nil, provider)

	req := httptest.NewRequest("GET", "/link/svc/test", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

func TestProxy_DefaultProtocol(t *testing.T) {
	handler := setupProxy(t, nil, []link.LinkTarget{
		{Name: "svc", Host: "127.0.0.1:1"},
	}, nil, nil)

	req := httptest.NewRequest("GET", "/link/svc/test", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	// Will fail connecting but exercises the URL construction path with default protocol
	if w.Code == http.StatusOK {
		t.Error("expected non-200 for unreachable host")
	}
}

func TestProxy_CircuitBreakerRecordsSuccess(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	host := strings.TrimPrefix(upstream.URL, "http://")
	breaker := circuitbreaker.New(circuitbreaker.Config{
		FailureThreshold: 5, SuccessThreshold: 1, ResetTimeout: 1e9,
	})
	handler := setupProxy(t, upstream, []link.LinkTarget{
		{Name: "svc", Protocol: "http", Host: host},
	}, map[string]*circuitbreaker.Breaker{"svc": breaker}, nil)

	req := httptest.NewRequest("GET", "/link/svc/test", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestProxy_RetryConfigCustom(t *testing.T) {
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	host := strings.TrimPrefix(upstream.URL, "http://")
	handler := setupProxy(t, upstream, []link.LinkTarget{
		{Name: "svc", Protocol: "http", Host: host, Retry: link.RetryConfig{
			MaxAttempts: 5, InitialInterval: "1ms", MaxInterval: "5ms", Jitter: 0.1,
		}},
	}, nil, nil)

	req := httptest.NewRequest("GET", "/link/svc/test", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestProxy_NoMetrics(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	host := strings.TrimPrefix(upstream.URL, "http://")
	store := link.NewTargetStore([]link.LinkTarget{
		{Name: "svc", Protocol: "http", Host: host},
	})
	handler := NewHandler(Config{Targets: store})

	req := httptest.NewRequest("GET", "/link/svc/test", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestProxy_TargetOnlyName(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			t.Errorf("expected path '/', got %q", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	host := strings.TrimPrefix(upstream.URL, "http://")
	handler := setupProxy(t, upstream, []link.LinkTarget{
		{Name: "svc", Protocol: "http", Host: host},
	}, nil, nil)

	req := httptest.NewRequest("GET", "/link/svc", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestProxy_PathAllowExactMatch(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	host := strings.TrimPrefix(upstream.URL, "http://")
	handler := setupProxy(t, upstream, []link.LinkTarget{
		{Name: "svc", Protocol: "http", Host: host, AllowedPaths: []string{"/api/health"}},
	}, nil, nil)

	req := httptest.NewRequest("GET", "/link/svc/api/health", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestProxy_429RetriesOnTooManyRequests(t *testing.T) {
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls < 2 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	host := strings.TrimPrefix(upstream.URL, "http://")
	handler := setupProxy(t, upstream, []link.LinkTarget{
		{Name: "svc", Protocol: "http", Host: host, Retry: link.RetryConfig{
			MaxAttempts: 3, InitialInterval: "1ms", MaxInterval: "5ms",
		}},
	}, nil, nil)

	req := httptest.NewRequest("GET", "/link/svc/test", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if calls < 2 {
		t.Errorf("expected retry on 429, got %d calls", calls)
	}
}
