package kafka

import (
	"strings"
	"testing"
)

func TestClusterConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     ClusterConfig
		wantErr string
	}{
		{
			name: "valid minimal config",
			cfg: ClusterConfig{
				Brokers: []string{"localhost:9092"},
			},
			wantErr: "",
		},
		{
			name: "valid with PLAIN auth",
			cfg: ClusterConfig{
				Brokers: []string{"localhost:9092"},
				Auth: AuthConfig{
					Mechanism: "PLAIN",
					Username:  "user",
					Password:  "pass",
				},
			},
			wantErr: "",
		},
		{
			name: "valid with SCRAM-SHA-256",
			cfg: ClusterConfig{
				Brokers: []string{"localhost:9092"},
				Auth: AuthConfig{
					Mechanism: "SCRAM-SHA-256",
					Username:  "user",
					Password:  "pass",
				},
			},
			wantErr: "",
		},
		{
			name: "valid with SCRAM-SHA-512",
			cfg: ClusterConfig{
				Brokers: []string{"localhost:9092"},
				Auth: AuthConfig{
					Mechanism: "SCRAM-SHA-512",
					Username:  "user",
					Password:  "pass",
				},
			},
			wantErr: "",
		},
		{
			name: "valid with TLS",
			cfg: ClusterConfig{
				Brokers: []string{"localhost:9092"},
				TLS: TLSConfig{
					Enabled: true,
				},
			},
			wantErr: "",
		},
		{
			name: "valid with mTLS",
			cfg: ClusterConfig{
				Brokers: []string{"localhost:9092"},
				TLS: TLSConfig{
					Enabled:  true,
					CertFile: "/path/to/cert.pem",
					KeyFile:  "/path/to/key.pem",
				},
			},
			wantErr: "",
		},
		{
			name:    "missing brokers",
			cfg:     ClusterConfig{},
			wantErr: "at least one broker is required",
		},
		{
			name: "invalid auth mechanism",
			cfg: ClusterConfig{
				Brokers: []string{"localhost:9092"},
				Auth: AuthConfig{
					Mechanism: "INVALID",
					Username:  "user",
					Password:  "pass",
				},
			},
			wantErr: "unsupported SASL mechanism",
		},
		{
			name: "auth mechanism without username",
			cfg: ClusterConfig{
				Brokers: []string{"localhost:9092"},
				Auth: AuthConfig{
					Mechanism: "PLAIN",
					Password:  "pass",
				},
			},
			wantErr: "username is required",
		},
		{
			name: "auth mechanism without password",
			cfg: ClusterConfig{
				Brokers: []string{"localhost:9092"},
				Auth: AuthConfig{
					Mechanism: "PLAIN",
					Username:  "user",
				},
			},
			wantErr: "password is required",
		},
		{
			name: "TLS certFile without keyFile",
			cfg: ClusterConfig{
				Brokers: []string{"localhost:9092"},
				TLS: TLSConfig{
					Enabled:  true,
					CertFile: "/path/to/cert.pem",
				},
			},
			wantErr: "both certFile and keyFile must be specified together",
		},
		{
			name: "TLS keyFile without certFile",
			cfg: ClusterConfig{
				Brokers: []string{"localhost:9092"},
				TLS: TLSConfig{
					Enabled: true,
					KeyFile: "/path/to/key.pem",
				},
			},
			wantErr: "both certFile and keyFile must be specified together",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("Validate() error = %v, want nil", err)
				}
			} else {
				if err == nil {
					t.Errorf("Validate() error = nil, want error containing %q", tt.wantErr)
				} else if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("Validate() error = %v, want error containing %q", err, tt.wantErr)
				}
			}
		})
	}
}

func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}

func TestClusterConfig_Validate_OAUTHBEARER(t *testing.T) {
	tests := []struct {
		name    string
		cfg     ClusterConfig
		wantErr string
	}{
		{
			name: "valid azure oauth",
			cfg: ClusterConfig{
				Brokers: []string{"localhost:9092"},
				Auth: AuthConfig{
					Mechanism: "OAUTHBEARER",
					OAuth: &OAuthConfig{
						Provider:        "azure",
						TenantID:        "tenant-123",
						ClientID:        "client-123",
						ClientSecretEnv: "SECRET_ENV",
						Scope:           "api://kafka/.default",
					},
				},
			},
			wantErr: "",
		},
		{
			name: "missing oauth config",
			cfg: ClusterConfig{
				Brokers: []string{"localhost:9092"},
				Auth: AuthConfig{
					Mechanism: "OAUTHBEARER",
				},
			},
			wantErr: "oauth config is required",
		},
		{
			name: "missing provider",
			cfg: ClusterConfig{
				Brokers: []string{"localhost:9092"},
				Auth: AuthConfig{
					Mechanism: "OAUTHBEARER",
					OAuth:     &OAuthConfig{},
				},
			},
			wantErr: "oauth.provider is required",
		},
		{
			name: "missing clientId",
			cfg: ClusterConfig{
				Brokers: []string{"localhost:9092"},
				Auth: AuthConfig{
					Mechanism: "OAUTHBEARER",
					OAuth: &OAuthConfig{
						Provider: "azure",
						TenantID: "tenant-123",
					},
				},
			},
			wantErr: "oauth.clientId is required",
		},
		{
			name: "missing scope",
			cfg: ClusterConfig{
				Brokers: []string{"localhost:9092"},
				Auth: AuthConfig{
					Mechanism: "OAUTHBEARER",
					OAuth: &OAuthConfig{
						Provider: "azure",
						TenantID: "tenant-123",
						ClientID: "client-123",
					},
				},
			},
			wantErr: "oauth.scope is required",
		},
		{
			name: "missing tenantId for azure",
			cfg: ClusterConfig{
				Brokers: []string{"localhost:9092"},
				Auth: AuthConfig{
					Mechanism: "OAUTHBEARER",
					OAuth: &OAuthConfig{
						Provider: "azure",
						ClientID: "client-123",
						Scope:    "api://kafka/.default",
					},
				},
			},
			wantErr: "oauth.tenantId is required for Azure",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("expected no error, got: %v", err)
				}
			} else {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.wantErr)
				} else if !contains(err.Error(), tt.wantErr) {
					t.Errorf("expected error containing %q, got: %v", tt.wantErr, err)
				}
			}
		})
	}
}

func TestKafkaGlobalConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     KafkaGlobalConfig
		wantErr string
	}{
		{
			name: "valid clusters",
			cfg: KafkaGlobalConfig{
				Clusters: map[string]ClusterConfig{
					"main": {Brokers: []string{"localhost:9092"}},
					"logs": {Brokers: []string{"logs:9092"}},
				},
			},
			wantErr: "",
		},
		{
			name: "empty clusters is valid",
			cfg: KafkaGlobalConfig{
				Clusters: map[string]ClusterConfig{},
			},
			wantErr: "",
		},
		{
			name: "invalid cluster in map",
			cfg: KafkaGlobalConfig{
				Clusters: map[string]ClusterConfig{
					"main": {Brokers: []string{"localhost:9092"}},
					"bad":  {}, // missing brokers
				},
			},
			wantErr: "cluster \"bad\"",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("Validate() error = %v, want nil", err)
				}
			} else {
				if err == nil {
					t.Errorf("Validate() error = nil, want error containing %q", tt.wantErr)
				} else if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("Validate() error = %v, want error containing %q", err, tt.wantErr)
				}
			}
		})
	}
}
