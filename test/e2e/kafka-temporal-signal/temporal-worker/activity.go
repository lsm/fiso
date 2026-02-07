package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
)

// Activities holds shared dependencies for Temporal activities.
type Activities struct {
	LinkAddr string
}

// ProcessSignalData receives the CloudEvent directly as a structured map,
// extracts the data payload, and calls an external service via fiso-link.
// This demonstrates: Kafka → fiso-flow → Temporal signal → activity → fiso-link → external-api.
// The event is now a map[string]interface{} instead of []byte, making it compatible
// with Java/Kotlin SDK workflows that deserialize JSON to typed objects.
func (a *Activities) ProcessSignalData(_ context.Context, event map[string]interface{}) (string, error) {
	log.Printf("SIGNAL_RECEIVED event: %v", event)

	// Extract the data field directly (no need to unmarshal)
	dataRaw, err := json.Marshal(event["data"])
	if err != nil {
		return "", fmt.Errorf("marshal data: %w", err)
	}

	// Extract order_id for logging
	var data map[string]interface{}
	if err := json.Unmarshal(dataRaw, &data); err != nil {
		return "", fmt.Errorf("unmarshal data: %w", err)
	}
	orderID, _ := data["order_id"].(string)

	log.Printf("processing signal for order_id=%s via fiso-link", orderID)

	// Call external service via fiso-link
	linkURL := a.LinkAddr + "/link/echo"
	resp, err := http.Post(linkURL, "application/json", strings.NewReader(string(dataRaw)))
	if err != nil {
		return "", fmt.Errorf("fiso-link call: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	log.Printf("fiso-link response: status=%d body=%s", resp.StatusCode, body)

	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("fiso-link returned %d: %s", resp.StatusCode, body)
	}

	result := fmt.Sprintf("order_id=%s external_response=%s", orderID, body)
	log.Printf("SIGNAL_PROCESSED %s", result)
	return result, nil
}
