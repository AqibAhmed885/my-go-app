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

// Liveness godoc
// @Summary      Liveness check
// @Description  Confirms that the HTTP process is running
// @Tags         System
// @Produce      json
// @Success      200 {object} HealthResponse
// @Router       /healthz [get]
// Liveness check: confirms the HTTP process is running
func (h *HealthHandler) Liveness(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, HealthResponse{Status: "UP"})
}

// Readiness godoc
// @Summary      Readiness check
// @Description  Verifies that PostgreSQL is reachable
// @Tags         System
// @Produce      json
// @Success      200 {object} HealthResponse
// @Failure      503 {object} HealthResponse
// @Router       /readyz [get]
// Readiness check: verifies external dependencies (PostgreSQL) are reachable
func (h *HealthHandler) Readiness(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	if err := h.DB.PingContext(ctx); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, HealthResponse{Status: "DOWN", Database: "unreachable"})
		return
	}

	writeJSON(w, http.StatusOK, HealthResponse{Status: "UP", Database: "connected"})
}
