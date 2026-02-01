package webhook

import (
	"encoding/json"
	"fmt"
	"net/http"
)

const (
	// AnnotationInject is the annotation key that triggers sidecar injection.
	AnnotationInject = "fiso.io/inject"
	// AnnotationStatus records whether injection has occurred.
	AnnotationStatus = "fiso.io/status"
)

// SidecarConfig defines the sidecar container configuration.
type SidecarConfig struct {
	Image         string `json:"image"`
	CPURequest    string `json:"cpuRequest"`
	MemoryRequest string `json:"memoryRequest"`
	CPULimit      string `json:"cpuLimit"`
	MemoryLimit   string `json:"memoryLimit"`
	ProxyPort     int    `json:"proxyPort"`
	MetricsPort   int    `json:"metricsPort"`
}

// DefaultSidecarConfig returns the default sidecar injection configuration.
func DefaultSidecarConfig() SidecarConfig {
	return SidecarConfig{
		Image:         "ghcr.io/lsm/fiso-link:latest",
		CPURequest:    "50m",
		MemoryRequest: "64Mi",
		CPULimit:      "200m",
		MemoryLimit:   "256Mi",
		ProxyPort:     3500,
		MetricsPort:   9090,
	}
}

// AdmissionReview is a simplified admission review request/response.
type AdmissionReview struct {
	APIVersion string            `json:"apiVersion"`
	Kind       string            `json:"kind"`
	Request    *AdmissionRequest `json:"request,omitempty"`
	Response   *AdmissionResponse `json:"response,omitempty"`
}

// AdmissionRequest represents the incoming admission request.
type AdmissionRequest struct {
	UID    string          `json:"uid"`
	Object json.RawMessage `json:"object"`
}

// AdmissionResponse represents the admission response.
type AdmissionResponse struct {
	UID       string `json:"uid"`
	Allowed   bool   `json:"allowed"`
	PatchType string `json:"patchType,omitempty"`
	Patch     string `json:"patch,omitempty"`
}

// PodPartial is the minimal pod structure needed for injection decision.
type PodPartial struct {
	Metadata struct {
		Annotations map[string]string `json:"annotations"`
	} `json:"metadata"`
	Spec struct {
		Containers []Container `json:"containers"`
	} `json:"spec"`
}

// Container is a simplified container spec.
type Container struct {
	Name  string `json:"name"`
	Image string `json:"image"`
}

// JSONPatchOp represents a single JSON Patch operation.
type JSONPatchOp struct {
	Op    string      `json:"op"`
	Path  string      `json:"path"`
	Value interface{} `json:"value,omitempty"`
}

// WebhookHandler handles mutating admission webhook requests for sidecar injection.
type WebhookHandler struct {
	config SidecarConfig
}

// NewWebhookHandler creates a new webhook handler.
func NewWebhookHandler(config SidecarConfig) *WebhookHandler {
	return &WebhookHandler{config: config}
}

// ServeHTTP handles the admission webhook request.
func (h *WebhookHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if r.Header.Get("Content-Type") != "application/json" {
		http.Error(w, "invalid content type", http.StatusBadRequest)
		return
	}

	var review AdmissionReview
	if err := json.NewDecoder(r.Body).Decode(&review); err != nil {
		http.Error(w, fmt.Sprintf("decode error: %v", err), http.StatusBadRequest)
		return
	}

	if review.Request == nil {
		http.Error(w, "missing request in admission review", http.StatusBadRequest)
		return
	}

	response := h.handleAdmission(review.Request)

	review.Response = response
	review.Request = nil

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(review); err != nil {
		http.Error(w, fmt.Sprintf("encode error: %v", err), http.StatusInternalServerError)
		return
	}
}

func (h *WebhookHandler) handleAdmission(req *AdmissionRequest) *AdmissionResponse {
	resp := &AdmissionResponse{
		UID:     req.UID,
		Allowed: true,
	}

	var pod PodPartial
	if err := json.Unmarshal(req.Object, &pod); err != nil {
		return resp // Allow but don't patch on parse failure
	}

	// Check if injection is requested
	if pod.Metadata.Annotations[AnnotationInject] != "true" {
		return resp
	}

	// Check if already injected
	if pod.Metadata.Annotations[AnnotationStatus] == "injected" {
		return resp
	}

	// Build JSON Patch
	patch := h.buildPatch(pod)
	if len(patch) == 0 {
		return resp
	}

	patchBytes, err := json.Marshal(patch)
	if err != nil {
		return resp
	}

	resp.PatchType = "JSONPatch"
	resp.Patch = string(patchBytes)
	return resp
}

func (h *WebhookHandler) buildPatch(pod PodPartial) []JSONPatchOp {
	sidecar := map[string]interface{}{
		"name":  "fiso-link",
		"image": h.config.Image,
		"ports": []map[string]interface{}{
			{"containerPort": h.config.ProxyPort, "name": "proxy", "protocol": "TCP"},
			{"containerPort": h.config.MetricsPort, "name": "metrics", "protocol": "TCP"},
		},
		"resources": map[string]interface{}{
			"requests": map[string]string{
				"cpu":    h.config.CPURequest,
				"memory": h.config.MemoryRequest,
			},
			"limits": map[string]string{
				"cpu":    h.config.CPULimit,
				"memory": h.config.MemoryLimit,
			},
		},
		"env": []map[string]string{
			{"name": "FISO_LINK_CONFIG", "value": "/etc/fiso/link/config.yaml"},
		},
	}

	var patch []JSONPatchOp

	// Add sidecar container
	if len(pod.Spec.Containers) == 0 {
		patch = append(patch, JSONPatchOp{
			Op:    "add",
			Path:  "/spec/containers",
			Value: []interface{}{sidecar},
		})
	} else {
		patch = append(patch, JSONPatchOp{
			Op:    "add",
			Path:  "/spec/containers/-",
			Value: sidecar,
		})
	}

	// Add injection status annotation
	if pod.Metadata.Annotations == nil {
		patch = append(patch, JSONPatchOp{
			Op:    "add",
			Path:  "/metadata/annotations",
			Value: map[string]string{AnnotationStatus: "injected"},
		})
	} else {
		patch = append(patch, JSONPatchOp{
			Op:    "add",
			Path:  "/metadata/annotations/fiso.io~1status",
			Value: "injected",
		})
	}

	return patch
}
