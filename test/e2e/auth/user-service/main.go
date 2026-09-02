package main

import (
	"io"
	"log"
	"net/http"
	"sort"
	"strings"
)

// user-service receives the post-authentication request and logs every
// header, so the E2E can assert the credential was stripped and the
// verdict headers were added before the sink saw the event.
func main() {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read body: "+err.Error(), http.StatusBadRequest)
			return
		}
		names := make([]string, 0, len(r.Header))
		for name := range r.Header {
			names = append(names, name)
		}
		sort.Strings(names)
		values := make([]string, 0, len(names))
		for _, name := range names {
			values = append(values, name+"="+r.Header.Get(name))
		}
		log.Printf("received event: %s; headers: %s", body, strings.Join(values, " "))

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	})

	addr := ":8082"
	log.Printf("user-service listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, nil))
}
