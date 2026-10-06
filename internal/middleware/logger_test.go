package middleware

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequestLoggerRecordsRequestAndPreservesRequestID(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	handler := RequestLogger(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := RequestIDFromContext(r.Context()); got != "request-123" {
			t.Fatalf("expected request ID in context, got %q", got)
		}
		w.WriteHeader(http.StatusCreated)
	}))

	req := httptest.NewRequest(http.MethodPost, "/users", nil)
	req.Header.Set("X-Request-ID", "request-123")
	res := httptest.NewRecorder()

	handler.ServeHTTP(res, req)

	if res.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d", res.Code)
	}
	if got := res.Header().Get("X-Request-ID"); got != "request-123" {
		t.Fatalf("expected response request ID, got %q", got)
	}

	var entry map[string]any
	if err := json.Unmarshal(logs.Bytes(), &entry); err != nil {
		t.Fatalf("decode structured log: %v", err)
	}
	for key, want := range map[string]string{
		"msg":         "http request",
		"method":      http.MethodPost,
		"path":        "/users",
		"request_id":  "request-123",
		"remote_addr": req.RemoteAddr,
	} {
		if got := entry[key]; got != want {
			t.Errorf("log field %s = %v, want %q", key, got, want)
		}
	}
	if got := entry["status"]; got != float64(http.StatusCreated) {
		t.Errorf("log status = %v, want %d", got, http.StatusCreated)
	}
}
