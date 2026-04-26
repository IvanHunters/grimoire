package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"go.mongodb.org/mongo-driver/mongo"
)

// HealthResponse represents the health check response
type HealthResponse struct {
	Status   string `json:"status"`
	MongoDB  string `json:"mongodb"`
	Time     string `json:"time"`
}

// Health handles health check requests
func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	response := HealthResponse{
		Status: "ok",
		Time:   time.Now().Format(time.RFC3339),
	}

	// Check MongoDB connection
	if err := h.db.Client().Ping(ctx, nil); err != nil {
		response.MongoDB = "error: " + err.Error()
		response.Status = "degraded"
	} else {
		response.MongoDB = "connected"
	}

	w.Header().Set("Content-Type", "application/json")
	if response.Status != "ok" {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	json.NewEncoder(w).Encode(response)
}
