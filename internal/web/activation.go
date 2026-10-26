package web

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/ardeman/project-dahlunas-go/internal/shop"
)

// registerActivations serves apps that bind a code to one device
// (docs/activation.md). Answers are JSON with a fixed error word the app
// translates; nothing about the device or the code is logged.
func registerActivations(mux *http.ServeMux, config Options) {
	if config.Shop == nil || config.Shop.Activations == nil {
		return
	}
	limiter := &checkoutLimiter{}
	mux.HandleFunc("POST /activations", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/json")
		answer := func(status int, body map[string]any) {
			w.WriteHeader(status)
			json.NewEncoder(w).Encode(body)
		}
		if !limiter.allow(time.Now()) {
			w.Header().Set("Retry-After", "2")
			answer(http.StatusTooManyRequests, map[string]any{"error": "busy"})
			return
		}
		var request struct {
			Code      string `json:"code"`
			InstallID string `json:"install_id"`
			Move      bool   `json:"move"`
		}
		d := json.NewDecoder(io.LimitReader(r.Body, 4096))
		d.DisallowUnknownFields()
		if d.Decode(&request) != nil {
			answer(http.StatusBadRequest, map[string]any{"error": "invalid"})
			return
		}
		activation, left, err := config.Shop.Activate(r.Context(), request.Code, request.InstallID, request.Move)
		switch {
		case err == nil:
			answer(http.StatusOK, map[string]any{"activation": activation, "moves_left": left})
		case errors.Is(err, shop.ErrInput):
			answer(http.StatusBadRequest, map[string]any{"error": "invalid"})
		case errors.Is(err, shop.ErrActivationOff):
			answer(http.StatusNotFound, map[string]any{"error": "not_supported"})
		case errors.Is(err, shop.ErrMoved):
			answer(http.StatusConflict, map[string]any{"error": "moved", "moves_left": left})
		case errors.Is(err, shop.ErrMoveLimit):
			answer(http.StatusConflict, map[string]any{"error": "move_limit", "moves_left": 0})
		case errors.Is(err, shop.ErrConflict):
			answer(http.StatusServiceUnavailable, map[string]any{"error": "busy"})
		default:
			log.Printf("activation failed: %v", err)
			answer(http.StatusServiceUnavailable, map[string]any{"error": "busy"})
		}
	})
}
