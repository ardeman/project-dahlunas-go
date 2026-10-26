package web

import (
	"crypto/subtle"
	"encoding/json"
	"log"
	"net/http"
)

// registerTasks serves the delivery sweep for Cloud Scheduler. It is routed
// only when a task token is configured, and the token is checked first.
func registerTasks(mux *http.ServeMux, config Options) {
	if config.Shop == nil || config.TaskToken == "" {
		return
	}
	mux.HandleFunc("POST /tasks/deliveries", func(w http.ResponseWriter, r *http.Request) {
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+config.TaskToken)) != 1 {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		report, err := config.Shop.Sweep(r.Context())
		if err != nil {
			log.Printf("delivery sweep failed: %v", err)
			http.Error(w, "sweep failed", http.StatusServiceUnavailable)
			return
		}
		// Log-based alerts match this fixed text; order IDs carry no buyer data.
		for _, id := range report.Review {
			log.Printf("delivery needs review: order %s (automatic email retries stopped)", id)
		}
		if report.Pending > 0 {
			log.Printf("delivery sweep: %d orders still pending", report.Pending)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]int{"delivered": report.Delivered, "pending": report.Pending, "review": len(report.Review)})
	})
}
