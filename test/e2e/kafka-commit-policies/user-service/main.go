package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
)

func main() {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read body: "+err.Error(), http.StatusBadRequest)
			return
		}
		log.Printf("received event: %s", body)

		// Inject a delivery failure for messages containing "fail-me".
		// Used by the sink_or_dlq test to trigger DLQ routing.
		if strings.Contains(string(body), "fail-me") {
			log.Printf("injecting failure for: %s", body)
			http.Error(w, "injected failure", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"status":"ok"}`)
	})

	log.Printf("user-service listening on :8082")
	log.Fatal(http.ListenAndServe(":8082", nil))
}
