package main

import (
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
		log.Printf("received event: %s", body)

		// Echo the body back so the test can inspect it
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	})

	addr := ":8082"
	log.Printf("user-service listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, nil))
}
