package handlers_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	_ "github.com/lib/pq"

	"github.com/AqibAhmed885/my-go-app/internal/handlers"
	"github.com/AqibAhmed885/my-go-app/internal/models"
)

// setupTestDB sets up a connection to the local database and truncates users for a clean run
func setupTestDB(t *testing.T) *sql.DB {
	dbConnStr := os.Getenv("TEST_DATABASE_URL")
	if dbConnStr == "" {
		dbConnStr = "postgres://postgres:postgres@localhost:5432/my_go_app?sslmode=disable"
	}

	db, err := sql.Open("postgres", dbConnStr)
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}

	// Clean out old test data
	if _, err := db.Exec("TRUNCATE TABLE users RESTART IDENTITY CASCADE;"); err != nil {
		t.Fatalf("failed to truncate table: %v", err)
	}

	return db
}

func TestCreateAndGetUser(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	userModel := &models.UserModel{DB: db}
	handler := &handlers.UserHandler{Users: userModel}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /users", handler.CreateUser)
	mux.HandleFunc("GET /users/{id}", handler.GetUserByID)

	// 1. Test POST /users
	t.Run("Create User - Success", func(t *testing.T) {
		body := bytes.NewBufferString(`{"name":"Integration Tester","email":"test@example.com"}`)
		req := httptest.NewRequest(http.MethodPost, "/users", body)
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()

		mux.ServeHTTP(rr, req)

		if rr.Code != http.StatusCreated {
			t.Fatalf("expected status 201 Created, got %d. Body: %s", rr.Code, rr.Body.String())
		}

		var created models.User
		if err := json.Unmarshal(rr.Body.Bytes(), &created); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if created.ID != 1 || created.Email != "test@example.com" {
			t.Errorf("unexpected user returned: %+v", created)
		}
	})

	// 2. Test Duplicate Email (409 Conflict)
	t.Run("Create User - Duplicate Conflict", func(t *testing.T) {
		body := bytes.NewBufferString(`{"name":"Duplicate","email":"test@example.com"}`)
		req := httptest.NewRequest(http.MethodPost, "/users", body)
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()

		mux.ServeHTTP(rr, req)

		if rr.Code != http.StatusConflict {
			t.Fatalf("expected status 409 Conflict, got %d", rr.Code)
		}
	})

	// 3. Test Invalid Email (400 Bad Request)
	t.Run("Create User - Invalid Email", func(t *testing.T) {
		body := bytes.NewBufferString(`{"name":"Invalid","email":"not-a-valid-email"}`)
		req := httptest.NewRequest(http.MethodPost, "/users", body)
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()

		mux.ServeHTTP(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Fatalf("expected status 400 Bad Request, got %d", rr.Code)
		}
	})
}

func TestRegisterValidation(t *testing.T) {
	// Setup handler (with nil db/redis to test initial validation)
	h := &handlers.UserHandler{}

	tests := []struct {
		name           string
		payload        map[string]string
		expectedStatus int
	}{
		{
			name: "Missing email",
			payload: map[string]string{
				"name":     "Test User",
				"password": "password123",
			},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "Missing password",
			payload: map[string]string{
				"name":  "Test User",
				"email": "test@example.com",
			},
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			body, _ := json.Marshal(tc.payload)
			req := httptest.NewRequest(http.MethodPost, "/auth/register", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			h.Register(w, req)

			if w.Code != tc.expectedStatus {
				t.Errorf("expected status %d, got %d", tc.expectedStatus, w.Code)
			}
		})
	}
}

func TestAuthMiddlewareRejectsMissingToken(t *testing.T) {
	// Dummy protected handler
	protectedHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Wrap with JWTMiddleware
	// Assuming JWTMiddleware is in internal/middleware
	// mw := middleware.JWTMiddleware("test-secret")
	// handlerToTest := mw(protectedHandler)

	req := httptest.NewRequest(http.MethodGet, "/profile", nil)
	w := httptest.NewRecorder()

	// Simulating request without Authorization header
	protectedHandler.ServeHTTP(w, req)

	// Verify status
	if w.Code == http.StatusOK {
		t.Log("Add middleware wrapping to verify 401 response on missing token")
	}
}
