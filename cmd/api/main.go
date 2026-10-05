package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	_ "github.com/AqibAhmed885/my-go-app/docs" // <-- MUST BE PRESENT
	"github.com/AqibAhmed885/my-go-app/internal/handlers"
	"github.com/AqibAhmed885/my-go-app/internal/middleware"
	"github.com/AqibAhmed885/my-go-app/internal/models"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
	httpSwagger "github.com/swaggo/http-swagger/v2"
)

type responseRecorder struct {
	http.ResponseWriter
	statusCode int
}

func (rec *responseRecorder) WriteHeader(code int) {
	rec.statusCode = code
	rec.ResponseWriter.WriteHeader(code)
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &responseRecorder{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(rec, r)
		log.Printf("[%s] %s %d - %v", r.Method, r.URL.Path, rec.statusCode, time.Since(start))
	})
}

func runMigrations(db *sql.DB, migrationsDir string) error {
	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		return fmt.Errorf("reading migrations dir: %w", err)
	}

	var sqlFiles []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || filepath.Ext(name) != ".sql" || strings.HasSuffix(name, ".down.sql") {
			continue
		}
		sqlFiles = append(sqlFiles, name)
	}

	// Sort explicitly to guarantee 000001 runs before 000002
	sort.Strings(sqlFiles)

	for _, name := range sqlFiles {
		filePath := filepath.Join(migrationsDir, name)
		query, err := os.ReadFile(filePath)
		if err != nil {
			return fmt.Errorf("reading file %s: %w", name, err)
		}

		log.Printf("Applying migration: %s", name)
		if _, err := db.Exec(string(query)); err != nil {
			return fmt.Errorf("executing migration %s: %w", name, err)
		}
	}
	return nil
}

// @title           My Go App API
// @version         1.0
// @description     A production-ready REST API in Go with PostgreSQL, JWT Auth, and Rate Limiting.
// @host            localhost:8080
// @BasePath        /

// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
// @description Type "Bearer" followed by a space and your JWT token.
func main() {
	// Load .env file if it exists
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, using system environment variables")
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	dbConnStr := os.Getenv("DATABASE_URL")
	if dbConnStr == "" {
		log.Fatal("DATABASE_URL environment variable is required")
	}

	db, err := sql.Open("postgres", dbConnStr)
	if err != nil {
		log.Fatalf("Failed to open DB: %v", err)
	}
	defer db.Close()

	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(25)
	db.SetConnMaxLifetime(5 * time.Minute)

	if err := db.Ping(); err != nil {
		log.Fatalf("Failed to ping DB: %v", err)
	}

	// Apply migration file
	if err := runMigrations(db, "migrations"); err != nil {
		log.Fatalf("Migration failed: %v", err)
	}
	log.Println("All database migrations applied successfully")

	// Wire up model and handler
	userModel := &models.UserModel{DB: db}
	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		jwtSecret = "super-secret-development-key-change-in-production"
	}

	userHandler := &handlers.UserHandler{
		Users:     userModel,
		JWTSecret: jwtSecret,
	}

	authMiddleware := middleware.JWTMiddleware(jwtSecret)

	mux := http.NewServeMux()
	mux.HandleFunc("/", httpSwagger.WrapHandler) // Public Routes
	mux.HandleFunc("POST /auth/register", userHandler.Register)
	mux.HandleFunc("POST /auth/login", userHandler.Login)

	// Protected Routes
	// Protected Routes
	mux.Handle("GET /profile", authMiddleware(http.HandlerFunc(userHandler.GetProfile)))
	mux.Handle("DELETE /users/{id}", authMiddleware(http.HandlerFunc(userHandler.DeleteUser)))

	// Apply rate limiting: 5 requests/sec with burst of 10
	rateLimiter := middleware.RateLimit(5, 10)

	// Build the middleware pipeline: Logging -> CORS -> RateLimit -> Mux
	pipeline := loggingMiddleware(middleware.EnableCORS(rateLimiter(mux)))

	mux.HandleFunc("GET /users", userHandler.GetUsers)
	mux.HandleFunc("POST /users", userHandler.CreateUser)
	mux.HandleFunc("GET /users/{id}", userHandler.GetUserByID)
	mux.HandleFunc("PUT /users/{id}", userHandler.PutUser)
	// mux.HandleFunc("DELETE /users/{id}", userHandler.DeleteUser)

	// Health Checks (Bypasses authentication)
	healthHandler := &handlers.HealthHandler{DB: db}
	mux.HandleFunc("GET /healthz", healthHandler.Liveness)
	mux.HandleFunc("GET /readyz", healthHandler.Readiness)

	server := &http.Server{
		Addr:         ":" + port,
		Handler:      pipeline,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	shutdownSignal := make(chan os.Signal, 1)
	signal.Notify(shutdownSignal, os.Interrupt, syscall.SIGTERM)

	go func() {
		log.Printf("Server running on http://localhost:%s\n", port)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("HTTP server error: %v", err)
		}
	}()

	sig := <-shutdownSignal
	log.Printf("Received signal: %v. Draining active connections...", sig)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Printf("Shutdown error: %v", err)
	}
	log.Println("Server exiting cleanly")
}
