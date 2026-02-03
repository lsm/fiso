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

// ProcessSignalData parses the CloudEvent received via signal, extracts
// the data payload, and calls an external service via fiso-link.
// This demonstrates: Kafka → fiso-flow → Temporal signal → activity → fiso-link → external-api.
func (a *Activities) ProcessSignalData(_ context.Context, event []byte) (string, error) {
	log.Printf("SIGNAL_RECEIVED event: %s", event)

	// Parse the CloudEvent envelope
	var ce map[string]interface{}
	if err := json.Unmarshal(event, &ce); err != nil {
		return "", fmt.Errorf("unmarshal cloudevent: %w", err)
	}

	// Extract the data field
	dataRaw, err := json.Marshal(ce["data"])
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
