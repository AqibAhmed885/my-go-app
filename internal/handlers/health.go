package handlers

import (
	"context"
	"database/sql"
	"net/http"
	"time"
)

type HealthHandler struct {
	DB *sql.DB
}

// Liveness check: confirms the HTTP process is running
func (h *HealthHandler) Liveness(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status": "UP",
	})
}

// Readiness check: verifies external dependencies (PostgreSQL) are reachable
func (h *HealthHandler) Readiness(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	if err := h.DB.PingContext(ctx); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"status":   "DOWN",
			"database": "unreachable",
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"status":   "UP",
		"database": "connected",
	})
}