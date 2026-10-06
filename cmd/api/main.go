package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	_ "github.com/AqibAhmed885/my-go-app/docs" // <-- MUST BE PRESENT
	"github.com/AqibAhmed885/my-go-app/internal/database"
	"github.com/AqibAhmed885/my-go-app/internal/handlers"
	"github.com/AqibAhmed885/my-go-app/internal/middleware"
	"github.com/AqibAhmed885/my-go-app/internal/models"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
	httpSwagger "github.com/swaggo/http-swagger/v2"
)

func runMigrations(db *sql.DB, migrationsDir string, logger *slog.Logger) error {
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

		logger.Info("applying migration", slog.String("migration", name))
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
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	// Load .env file if it exists
	if err := godotenv.Load(); err != nil {
		logger.Warn("no .env file found, using system environment variables")
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	dbConnStr := os.Getenv("DATABASE_URL")
	if dbConnStr == "" {
		logger.Error("DATABASE_URL environment variable is required")
		os.Exit(1)
	}

	db, err := sql.Open("postgres", dbConnStr)
	if err != nil {
		logger.Error("failed to open database", slog.Any("error", err))
		os.Exit(1)
	}
	defer db.Close()

	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(25)
	db.SetConnMaxLifetime(5 * time.Minute)

	if err := db.Ping(); err != nil {
		logger.Error("failed to ping database", slog.Any("error", err))
		os.Exit(1)
	}

	// Apply migration file
	if err := runMigrations(db, "migrations", logger); err != nil {
		logger.Error("migration failed", slog.Any("error", err))
		os.Exit(1)
	}
	logger.Info("all database migrations applied successfully")

	// Wire up model and handler
	userModel := &models.UserModel{DB: db}
	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		jwtSecret = "super-secret-development-key-change-in-production"
	}

	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		redisURL = "redis://localhost:6379/0"
	}

	rdb, err := database.NewRedisClient(redisURL)
	if err != nil {
		logger.Warn("redis unavailable; continuing without caching", slog.Any("error", err))
	} else {
		logger.Info("connected to Redis cache successfully")
		defer rdb.Close()
	}

	userHandler := &handlers.UserHandler{
		Users:     userModel,
		Redis:     rdb,
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
	mux.Handle("DELETE /users/{id}", authMiddleware(
		middleware.RequireRole("admin")(http.HandlerFunc(userHandler.DeleteUser)),
	))

	// Apply rate limiting: 5 requests/sec with burst of 10
	rateLimiter := middleware.RateLimit(5, 10)

	// Build the middleware pipeline: Logging -> CORS -> RateLimit -> Mux
	pipeline := middleware.RequestLogger(logger)(middleware.EnableCORS(rateLimiter(mux)))

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
		logger.Info("server starting", slog.String("address", ":"+port))
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("HTTP server error", slog.Any("error", err))
			os.Exit(1)
		}
	}()

	sig := <-shutdownSignal
	logger.Info("received shutdown signal; draining active connections", slog.String("signal", sig.String()))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		logger.Error("shutdown error", slog.Any("error", err))
	}
	logger.Info("server exiting cleanly")
}
