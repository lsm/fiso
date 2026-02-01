package discovery

import (
	"context"
	"testing"
)

func TestStaticResolver(t *testing.T) {
	r := &StaticResolver{}
	tests := []string{
		"api.example.com",
		"10.0.0.1:8080",
		"https://api.salesforce.com",
		"order-service.default.svc.cluster.local",
	}
	for _, host := range tests {
		got, err := r.Resolve(context.Background(), host)
		if err != nil {
			t.Errorf("Resolve(%q): unexpected error: %v", host, err)
		}
		if got != host {
			t.Errorf("Resolve(%q) = %q, want %q", host, got, host)
		}
	}
}
