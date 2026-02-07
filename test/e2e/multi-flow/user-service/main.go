package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync/atomic"
)

var (
	flowACount int64
	flowBCount int64
)

func main() {
	http.HandleFunc("/flow-a", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&flowACount, 1)
		log.Printf("Received event on flow-a (count: %d)", atomic.LoadInt64(&flowACount))
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]interface{}{
			"flow":   "a",
			"status": "ok",
		}); err != nil {
			log.Printf("Error encoding response: %v", err)
		}
	})

	http.HandleFunc("/flow-b", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&flowBCount, 1)
		log.Printf("Received event on flow-b (count: %d)", atomic.LoadInt64(&flowBCount))
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]interface{}{
			"flow":   "b",
			"status": "ok",
		}); err != nil {
			log.Printf("Error encoding response: %v", err)
		}
	})

	http.HandleFunc("/stats", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]interface{}{
			"flow_a_count": atomic.LoadInt64(&flowACount),
			"flow_b_count": atomic.LoadInt64(&flowBCount),
		}); err != nil {
			log.Printf("Error encoding response: %v", err)
		}
	})

	fmt.Println("user-service listening on :8082")
	log.Fatal(http.ListenAndServe(":8082", nil))
}
