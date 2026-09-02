package main

import (
	"encoding/json"
	"os"
	"time"
)

// Test module for the guest-clock contract: it prints the guest's wall
// clock as JSON so host-side tests can assert that time-dependent guests
// (e.g. JWT exp/nbf verification) see the real host time instead of a
// frozen sandbox default.
type report struct {
	Now int64 `json:"now"`
}

func main() {
	_ = json.NewEncoder(os.Stdout).Encode(report{Now: time.Now().Unix()})
}
