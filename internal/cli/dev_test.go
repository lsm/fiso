package cli

import (
	"testing"
)

func TestContainsHealthy(t *testing.T) {
	tests := []struct {
		name   string
		input  []byte
		expect bool
	}{
		{"healthy output", []byte(`{"Health":"healthy","State":"running"}`), true},
		{"unhealthy output", []byte(`{"Health":"unhealthy","State":"running"}`), false},
		{"starting output", []byte(`{"Health":"starting","State":"running"}`), false},
		{"empty output", []byte{}, false},
		{"no health field", []byte(`{"State":"running"}`), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := containsHealthy(tt.input)
			if got != tt.expect {
				t.Errorf("containsHealthy(%s) = %v, want %v", tt.input, got, tt.expect)
			}
		})
	}
}

func TestRunDev_Help(t *testing.T) {
	if err := RunDev([]string{"-h"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
