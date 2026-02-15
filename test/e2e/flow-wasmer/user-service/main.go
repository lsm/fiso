package main

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
)

func main() {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read body: "+err.Error(), http.StatusBadRequest)
			return
		}

		// Log the received payload for test verification
		log.Printf("received event: %s", body)

		// Log headers for test verification
		for key, values := range r.Header {
			for _, value := range values {
				log.Printf("header: %s=%s", key, value)
			}
		}

		// Parse and validate the payload
		var data map[string]interface{}
		if err := json.Unmarshal(body, &data); err != nil {
			http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
			return
		}

		// Echo the enriched payload back
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":      "received",
			"order_id":    data["order_id"],
			"enriched":    data["wasmer_enriched"],
			"processedAt": data["processed_at"],
			"category":    data["value_category"],
		})
	})

	addr := ":8082"
	log.Printf("user-service listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, nil))
}
