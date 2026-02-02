package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
)

func main() {
	linkAddr := os.Getenv("FISO_LINK_ADDR")
	if linkAddr == "" {
		linkAddr = "http://fiso-link:8080"
	}

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read body: "+err.Error(), http.StatusBadRequest)
			return
		}
		log.Printf("received event: %s", body)

		// Business logic: call external service via fiso-link
		linkURL := linkAddr + "/link/echo"
		log.Printf("calling fiso-link: %s", linkURL)

		resp, err := http.Post(linkURL, "application/json", strings.NewReader(`{"lookup":"enrichment-data"}`))
		if err != nil {
			http.Error(w, "fiso-link call failed: "+err.Error(), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()

		linkBody, _ := io.ReadAll(resp.Body)
		log.Printf("fiso-link response: status=%d body=%s", resp.StatusCode, linkBody)

		if resp.StatusCode >= 400 {
			http.Error(w, fmt.Sprintf("fiso-link returned %d: %s", resp.StatusCode, linkBody), http.StatusBadGateway)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"status":"processed","external_data":%q}`, string(linkBody))
	})

	addr := ":8082"
	log.Printf("user-service listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, nil))
}
