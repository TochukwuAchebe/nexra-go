// Receives Nexra events on :8080/nexra/events and verifies each signature.
//
//	NEXRA_WEBHOOK_SECRET=whsec_... go run ./examples/webhook-receiver
package main

import (
	"io"
	"log"
	"net/http"
	"os"

	nexra "github.com/TochukwuAchebe/nexra-go"
)

func main() {
	secret := os.Getenv("NEXRA_WEBHOOK_SECRET")
	http.HandleFunc("/nexra/events", func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			http.Error(w, "unreadable body", http.StatusBadRequest)
			return
		}
		event, err := nexra.ConstructEvent(raw, r.Header.Get(nexra.SignatureHeader), secret, nexra.VerifyOptions{})
		if err != nil {
			http.Error(w, "bad signature", http.StatusBadRequest)
			return
		}
		// De-duplicate on event.ID, then hand the work to a queue and answer quickly.
		log.Printf("event %s %s", event.ID, event.Type)
		w.WriteHeader(http.StatusNoContent)
	})
	log.Fatal(http.ListenAndServe(":8080", nil))
}
