package webhook

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func makeReview(annotations map[string]string, containers []Container) AdmissionReview {
	pod := PodPartial{}
	pod.Metadata.Annotations = annotations
	pod.Spec.Containers = containers
	podBytes, _ := json.Marshal(pod)

	return AdmissionReview{
		APIVersion: "admission.k8s.io/v1",
		Kind:       "AdmissionReview",
		Request: &AdmissionRequest{
			UID:    "test-uid",
			Object: podBytes,
		},
	}
}

func TestWebhook_InjectsWhenAnnotated(t *testing.T) {
	h := NewWebhookHandler(DefaultSidecarConfig())
	review := makeReview(
		map[string]string{AnnotationInject: "true"},
		[]Container{{Name: "app", Image: "myapp:latest"}},
	)
	body, _ := json.Marshal(review)

	req := httptest.NewRequest(http.MethodPost, "/mutate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp AdmissionReview
	json.NewDecoder(w.Body).Decode(&resp)

	if resp.Response == nil {
		t.Fatal("expected response")
	}
	if !resp.Response.Allowed {
		t.Error("expected allowed=true")
	}
	if resp.Response.PatchType != "JSONPatch" {
		t.Errorf("expected PatchType 'JSONPatch', got %q", resp.Response.PatchType)
	}
	if resp.Response.Patch == "" {
		t.Fatal("expected patch to be non-empty")
	}

	// Verify patch contains sidecar
	if !strings.Contains(resp.Response.Patch, "fiso-link") {
		t.Error("expected patch to contain fiso-link sidecar")
	}
	if !strings.Contains(resp.Response.Patch, "injected") {
		t.Error("expected patch to contain injection status annotation")
	}
}

func TestWebhook_SkipsWithoutAnnotation(t *testing.T) {
	h := NewWebhookHandler(DefaultSidecarConfig())
	review := makeReview(
		map[string]string{},
		[]Container{{Name: "app", Image: "myapp:latest"}},
	)
	body, _ := json.Marshal(review)

	req := httptest.NewRequest(http.MethodPost, "/mutate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	var resp AdmissionReview
	json.NewDecoder(w.Body).Decode(&resp)

	if resp.Response.PatchType != "" {
		t.Error("expected no patch when annotation not present")
	}
}

func TestWebhook_SkipsAlreadyInjected(t *testing.T) {
	h := NewWebhookHandler(DefaultSidecarConfig())
	review := makeReview(
		map[string]string{AnnotationInject: "true", AnnotationStatus: "injected"},
		[]Container{{Name: "app", Image: "myapp:latest"}},
	)
	body, _ := json.Marshal(review)

	req := httptest.NewRequest(http.MethodPost, "/mutate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	var resp AdmissionReview
	json.NewDecoder(w.Body).Decode(&resp)

	if resp.Response.PatchType != "" {
		t.Error("expected no patch when already injected")
	}
}

func TestWebhook_MethodNotAllowed(t *testing.T) {
	h := NewWebhookHandler(DefaultSidecarConfig())
	req := httptest.NewRequest(http.MethodGet, "/mutate", nil)
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

func TestWebhook_InvalidContentType(t *testing.T) {
	h := NewWebhookHandler(DefaultSidecarConfig())
	req := httptest.NewRequest(http.MethodPost, "/mutate", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "text/plain")
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestWebhook_InvalidJSON(t *testing.T) {
	h := NewWebhookHandler(DefaultSidecarConfig())
	req := httptest.NewRequest(http.MethodPost, "/mutate", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestWebhook_MissingRequest(t *testing.T) {
	h := NewWebhookHandler(DefaultSidecarConfig())
	review := AdmissionReview{APIVersion: "v1", Kind: "AdmissionReview"}
	body, _ := json.Marshal(review)
	req := httptest.NewRequest(http.MethodPost, "/mutate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestWebhook_PatchContainsSidecarConfig(t *testing.T) {
	cfg := SidecarConfig{
		Image:         "custom/fiso-link:v2",
		CPURequest:    "100m",
		MemoryRequest: "128Mi",
		CPULimit:      "500m",
		MemoryLimit:   "512Mi",
		ProxyPort:     3501,
		MetricsPort:   9091,
	}
	h := NewWebhookHandler(cfg)
	review := makeReview(
		map[string]string{AnnotationInject: "true"},
		[]Container{{Name: "app", Image: "myapp:latest"}},
	)
	body, _ := json.Marshal(review)

	req := httptest.NewRequest(http.MethodPost, "/mutate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	var resp AdmissionReview
	json.NewDecoder(w.Body).Decode(&resp)

	if !strings.Contains(resp.Response.Patch, "custom/fiso-link:v2") {
		t.Error("expected custom image in patch")
	}
	if !strings.Contains(resp.Response.Patch, "3501") {
		t.Error("expected custom proxy port in patch")
	}
}

func TestWebhook_ResponseUID(t *testing.T) {
	h := NewWebhookHandler(DefaultSidecarConfig())
	review := makeReview(
		map[string]string{},
		[]Container{{Name: "app", Image: "myapp:latest"}},
	)
	body, _ := json.Marshal(review)

	req := httptest.NewRequest(http.MethodPost, "/mutate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	var resp AdmissionReview
	json.NewDecoder(w.Body).Decode(&resp)

	if resp.Response.UID != "test-uid" {
		t.Errorf("expected UID 'test-uid', got %q", resp.Response.UID)
	}
}

func TestWebhook_EmptyContainersList(t *testing.T) {
	h := NewWebhookHandler(DefaultSidecarConfig())
	review := makeReview(
		map[string]string{AnnotationInject: "true"},
		nil, // no containers
	)
	body, _ := json.Marshal(review)

	req := httptest.NewRequest(http.MethodPost, "/mutate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	var resp AdmissionReview
	json.NewDecoder(w.Body).Decode(&resp)

	if resp.Response.Patch == "" {
		t.Fatal("expected patch even with empty containers")
	}
	// Should use "add" with array value, not "add" with /-
	if !strings.Contains(resp.Response.Patch, "/spec/containers") {
		t.Error("expected containers path in patch")
	}
}

func TestWebhook_UnparseableObject(t *testing.T) {
	h := NewWebhookHandler(DefaultSidecarConfig())
	// Use a valid JSON value that cannot be decoded as PodPartial
	// (a JSON string instead of an object)
	review := AdmissionReview{
		APIVersion: "admission.k8s.io/v1",
		Kind:       "AdmissionReview",
		Request: &AdmissionRequest{
			UID:    "uid-1",
			Object: json.RawMessage(`"just a string"`),
		},
	}
	body, _ := json.Marshal(review)

	req := httptest.NewRequest(http.MethodPost, "/mutate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	var resp AdmissionReview
	json.NewDecoder(w.Body).Decode(&resp)

	// Should allow but not patch
	if !resp.Response.Allowed {
		t.Error("expected allowed=true on parse failure")
	}
	if resp.Response.PatchType != "" {
		t.Error("expected no patch on parse failure")
	}
}

func TestWebhook_NilAnnotations(t *testing.T) {
	h := NewWebhookHandler(DefaultSidecarConfig())
	// Build a pod with nil annotations
	pod := PodPartial{}
	pod.Spec.Containers = []Container{{Name: "app", Image: "myapp:latest"}}
	podBytes, _ := json.Marshal(pod)

	review := AdmissionReview{
		APIVersion: "admission.k8s.io/v1",
		Kind:       "AdmissionReview",
		Request: &AdmissionRequest{
			UID:    "uid-nil-ann",
			Object: podBytes,
		},
	}
	body, _ := json.Marshal(review)

	req := httptest.NewRequest(http.MethodPost, "/mutate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	var resp AdmissionReview
	json.NewDecoder(w.Body).Decode(&resp)

	// No injection annotation means no patch
	if resp.Response.PatchType != "" {
		t.Error("expected no patch with nil annotations")
	}
}

func TestDefaultSidecarConfig(t *testing.T) {
	cfg := DefaultSidecarConfig()
	if cfg.Image == "" {
		t.Error("expected default image")
	}
	if cfg.ProxyPort != 3500 {
		t.Errorf("expected proxy port 3500, got %d", cfg.ProxyPort)
	}
	if cfg.MetricsPort != 9090 {
		t.Errorf("expected metrics port 9090, got %d", cfg.MetricsPort)
	}
}
