package wasm

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

// HostHTTPConfig configures the fiso.http_call host function (ADR 0006).
type HostHTTPConfig struct {
	// LinkAddr is the Fiso-Link proxy address guest calls are routed
	// through, e.g. "http://127.0.0.1:3500".
	LinkAddr string
	// AllowedTargets is the deny-by-default allowlist; a call to any other
	// target is rejected without a network request.
	AllowedTargets []string
	// Client is the HTTP client (injected for tests; defaults to a
	// short-timeout client).
	Client *http.Client
}

// Host call result codes returned to the guest. Non-negative values are the
// number of response bytes written; negative values are errors.
const (
	HostErrInvalidRequest int32 = -1
	HostErrTargetDenied   int32 = -2
	HostErrBufferSize     int32 = -3
	HostErrUpstream       int32 = -4

// When a successful upstream response does not fit the guest's buffer, the
// host returns the negative of the required size (≤ -5 in practice) instead
// of HostErrBufferSize: the network call already happened, and a guest that
// blindly retries could repeat a non-idempotent operation. HostErrBufferSize
// is reserved for buffer-write failures where no call was repeated.
)

// maxHostResponseBody bounds a single response body; larger bodies return
// HostErrUpstream rather than being silently truncated.
const maxHostResponseBody = 64 << 20

// hostHTTPRequest is the JSON the guest passes to http_call. Bodies are
// base64 so arbitrary bytes (JSON, text, form-encoded, binary) survive the
// round-trip without re-encoding.
type hostHTTPRequest struct {
	Target  string            `json:"target"`
	Method  string            `json:"method"`
	Path    string            `json:"path"`
	Headers map[string]string `json:"headers,omitempty"`
	BodyB64 string            `json:"bodyB64,omitempty"`
}

// hostHTTPResponse is the JSON the host writes back into guest memory.
type hostHTTPResponse struct {
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers"`
	BodyB64 string            `json:"bodyB64,omitempty"`
}

// hostHTTPClient performs allowed calls through the Link proxy.
type hostHTTPClient struct {
	cfg    HostHTTPConfig
	client *http.Client
}

func newHostHTTPClient(cfg HostHTTPConfig) (*hostHTTPClient, error) {
	if cfg.LinkAddr == "" {
		return nil, fmt.Errorf("host http: linkAddr is required")
	}
	if _, err := url.Parse(cfg.LinkAddr); err != nil {
		return nil, fmt.Errorf("host http: linkAddr %q is not a valid URL: %w", cfg.LinkAddr, err)
	}
	c := cfg.Client
	if c == nil {
		c = &http.Client{Timeout: 10 * time.Second}
	}
	return &hostHTTPClient{cfg: cfg, client: c}, nil
}

// sanitizePath confines the guest to a single path under its target:
// it must be absolute, contain no ".." segments, and no encoded slashes.
func sanitizePath(path string) (string, error) {
	if path == "" {
		return "/", nil
	}
	if !strings.HasPrefix(path, "/") {
		return "", fmt.Errorf("path must be absolute")
	}
	// Check both the raw and the percent-decoded forms: Go's HTTP client
	// decodes the path before mux routing, so /api/%2e%2e/admin is a
	// traversal even though no literal ".." segment appears.
	decoded, err := url.PathUnescape(path)
	if err != nil {
		return "", fmt.Errorf("path is not validly percent-encoded")
	}
	for _, candidate := range []string{path, decoded} {
		if strings.Contains(candidate, "%2f") || strings.Contains(candidate, "%2F") || strings.Contains(candidate, "%25") {
			return "", fmt.Errorf("encoded slashes or percent signs are not allowed in path")
		}
		for _, seg := range strings.Split(candidate, "/") {
			if seg == ".." || seg == "." {
				return "", fmt.Errorf("path must not contain traversal segments")
			}
		}
	}
	return path, nil
}

// validMethodToken reports whether the method is a valid RFC 7230 token
// (no spaces or control characters).
func validMethodToken(method string) bool {
	if method == "" {
		return false
	}
	for _, r := range method {
		if r <= 0x20 || r >= 0x7f {
			return false
		}
	}
	return true
}

// call validates the request against the allowlist and performs it.
func (h *hostHTTPClient) call(ctx context.Context, req hostHTTPRequest) (hostHTTPResponse, error) {
	if req.Target == "" {
		return hostHTTPResponse{}, fmt.Errorf("invalid request: target is required")
	}
	if !slices.Contains(h.cfg.AllowedTargets, req.Target) {
		// Denied before any network activity (deny-by-default, ADR 0006).
		return hostHTTPResponse{}, fmt.Errorf("target %q is not in the interceptor's httpTargets allowlist", req.Target)
	}
	path, err := sanitizePath(req.Path)
	if err != nil {
		return hostHTTPResponse{}, fmt.Errorf("invalid request: %w", err)
	}

	method := strings.ToUpper(req.Method)
	if method == "" {
		method = http.MethodPost
	}
	if !validMethodToken(method) {
		return hostHTTPResponse{}, fmt.Errorf("invalid request: method %q is not a valid HTTP token", req.Method)
	}
	url := fmt.Sprintf("%s/link/%s%s", strings.TrimSuffix(h.cfg.LinkAddr, "/"), req.Target, path)

	var body io.Reader
	if req.BodyB64 != "" {
		raw, err := base64.StdEncoding.DecodeString(req.BodyB64)
		if err != nil {
			return hostHTTPResponse{}, fmt.Errorf("invalid request: bodyB64 is not valid base64: %w", err)
		}
		body = bytes.NewReader(raw)
	}
	httpReq, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return hostHTTPResponse{}, err
	}
	for k, v := range req.Headers {
		httpReq.Header.Set(k, v)
	}

	resp, err := h.client.Do(httpReq)
	if err != nil {
		return hostHTTPResponse{}, fmt.Errorf("link call: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxHostResponseBody+1))
	if err != nil {
		return hostHTTPResponse{}, err
	}
	if len(respBody) > maxHostResponseBody {
		return hostHTTPResponse{}, fmt.Errorf("upstream response exceeds %d bytes", maxHostResponseBody)
	}
	headers := make(map[string]string, len(resp.Header))
	for k, v := range resp.Header {
		if len(v) > 0 {
			headers[k] = v[0]
		}
	}
	return hostHTTPResponse{
		Status:  resp.StatusCode,
		Headers: headers,
		BodyB64: base64.StdEncoding.EncodeToString(respBody),
	}, nil
}

// hostHTTPExport builds the http_call host function into the given module
// builder. The guest owns all memory: it passes a request slice and a
// response buffer; the host writes only into the buffer it was given.
func hostHTTPExport(b wazero.HostModuleBuilder, client *hostHTTPClient) {
	b.NewFunctionBuilder().
		WithFunc(func(ctx context.Context, mod api.Module, reqPtr, reqLen, respPtr, respCap uint32) int32 {
			mem := mod.Memory()
			if mem == nil {
				return HostErrUpstream
			}
			reqBytes, ok := mem.Read(reqPtr, reqLen)
			if !ok {
				return HostErrInvalidRequest
			}
			var req hostHTTPRequest
			if err := json.Unmarshal(reqBytes, &req); err != nil {
				return HostErrInvalidRequest
			}
			resp, err := client.call(ctx, req)
			if err != nil {
				if strings.Contains(err.Error(), "allowlist") {
					return HostErrTargetDenied
				}
				if strings.Contains(err.Error(), "invalid request") {
					return HostErrInvalidRequest
				}
				return HostErrUpstream
			}
			respBytes, err := json.Marshal(resp)
			if err != nil {
				return HostErrUpstream
			}
			if uint32(len(respBytes)) > respCap {
				// The upstream call already happened: returning a bare
				// buffer-error code would invite a retry that repeats a
				// possibly non-idempotent operation. Embed the needed size
				// so the guest can retry with a larger buffer only when it
				// knows the operation is idempotent.
				return -int32(len(respBytes))
			}
			if !mem.Write(respPtr, respBytes) {
				return HostErrBufferSize
			}
			return int32(len(respBytes))
		}).
		Export("http_call")
}
