package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

type contextKey string

const (
	UserIDContextKey   = contextKey("user_id")
	UserRoleContextKey = contextKey("user_role")
)

// JWTMiddleware validates a bearer token and adds its user ID and role to the request context.
func JWTMiddleware(jwtSecret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				writeAuthError(w, http.StatusUnauthorized, "missing authorization header")
				return
			}

			parts := strings.Split(authHeader, " ")
			if len(parts) != 2 || parts[0] != "Bearer" || parts[1] == "" {
				writeAuthError(w, http.StatusUnauthorized, "invalid authorization header format")
				return
			}

			claims := jwt.MapClaims{}
			token, err := jwt.ParseWithClaims(parts[1], claims, func(t *jwt.Token) (any, error) {
				if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
					return nil, jwt.ErrSignatureInvalid
				}
				return []byte(jwtSecret), nil
			})
			if err != nil || token == nil || !token.Valid {
				writeAuthError(w, http.StatusUnauthorized, "invalid or expired token")
				return
			}

			userID, ok := claims["user_id"].(float64)
			if !ok {
				writeAuthError(w, http.StatusUnauthorized, "invalid token claims")
				return
			}

			role, _ := claims["role"].(string)
			ctx := context.WithValue(r.Context(), UserIDContextKey, int(userID))
			ctx = context.WithValue(ctx, UserRoleContextKey, role)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireRole allows a request through only when its authenticated role matches one of the allowed roles.
func RequireRole(allowedRoles ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			role, _ := r.Context().Value(UserRoleContextKey).(string)
			for _, allowedRole := range allowedRoles {
				if role == allowedRole {
					next.ServeHTTP(w, r)
					return
				}
			}

			writeAuthError(w, http.StatusForbidden, "forbidden: insufficient permissions")
		})
	}
}

func writeAuthError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}
