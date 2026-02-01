package auth

import "context"

// Credentials holds authentication credentials to inject into requests.
type Credentials struct {
	Type    string            // "Bearer", "APIKey", "Basic"
	Token   string            // token value
	Headers map[string]string // additional headers to inject
}

// Provider retrieves credentials for a given target.
type Provider interface {
	GetCredentials(ctx context.Context, targetName string) (*Credentials, error)
}

// NoopProvider always returns nil credentials (no auth).
type NoopProvider struct{}

func (n *NoopProvider) GetCredentials(_ context.Context, _ string) (*Credentials, error) {
	return nil, nil
}
