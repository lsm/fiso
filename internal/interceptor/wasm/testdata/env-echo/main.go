package main

import (
	"encoding/json"
	"os"
	"strings"
)

// Test module for the env-delivery contract: it prints the environment it
// was instantiated with as a JSON object, so host-side tests can assert
// that configured interceptor env reaches the guest.
func main() {
	env := make(map[string]string)
	for _, kv := range os.Environ() {
		if key, value, found := strings.Cut(kv, "="); found {
			env[key] = value
		}
	}
	_ = json.NewEncoder(os.Stdout).Encode(env)
}
