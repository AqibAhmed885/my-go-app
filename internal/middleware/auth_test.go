package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/golang-jwt/jwt/v5"
)

func TestRequireRoleAllowsMatchingRole(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	handler := RequireRole("admin")(next)

	req := httptest.NewRequest(http.MethodDelete, "/users/1", nil)
	req = req.WithContext(context.WithValue(req.Context(), UserRoleContextKey, "admin"))
	res := httptest.NewRecorder()

	handler.ServeHTTP(res, req)

	if res.Code != http.StatusNoContent {
		t.Fatalf("expected matching role to pass, got %d", res.Code)
	}
}

func TestRequireRoleRejectsInsufficientRole(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("next handler should not be called")
	})
	handler := RequireRole("admin")(next)

	req := httptest.NewRequest(http.MethodDelete, "/users/1", nil)
	req = req.WithContext(context.WithValue(req.Context(), UserRoleContextKey, "user"))
	res := httptest.NewRecorder()

	handler.ServeHTTP(res, req)

	if res.Code != http.StatusForbidden {
		t.Fatalf("expected forbidden response, got %d", res.Code)
	}

	var body map[string]string
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["error"] != "forbidden: insufficient permissions" {
		t.Fatalf("unexpected error response: %q", body["error"])
	}
}

func TestJWTMiddlewareInjectsRoleClaim(t *testing.T) {
	const secret = "test-secret"
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id": 7,
		"role":    "admin",
	}).SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Context().Value(UserRoleContextKey); got != "admin" {
			t.Fatalf("expected admin role in context, got %v", got)
		}
		w.WriteHeader(http.StatusNoContent)
	})
	handler := JWTMiddleware(secret)(next)

	req := httptest.NewRequest(http.MethodGet, "/profile", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	res := httptest.NewRecorder()

	handler.ServeHTTP(res, req)

	if res.Code != http.StatusNoContent {
		t.Fatalf("expected valid token to pass, got %d", res.Code)
	}
}
