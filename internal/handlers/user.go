package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/redis/go-redis/v9"

	"github.com/AqibAhmed885/my-go-app/internal/middleware"
	"github.com/AqibAhmed885/my-go-app/internal/models"
)

type UserHandler struct {
	Users     *models.UserModel
	Redis     *redis.Client // Redis client instance
	JWTSecret string
}

// RegisterRequest is the payload accepted when creating an account.
type RegisterRequest struct {
	Name     string `json:"name" example:"Aqib Ahmed"`
	Email    string `json:"email" example:"aqib@example.com"`
	Password string `json:"password" example:"secret123"`
}

// LoginRequest is the payload accepted when authenticating a user.
type LoginRequest struct {
	Email    string `json:"email" example:"aqib@example.com"`
	Password string `json:"password" example:"secret123"`
}

// AuthResponse contains a JWT and the authenticated user's public profile.
type AuthResponse struct {
	Token string      `json:"token" example:"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."`
	User  models.User `json:"user"`
}

// UserListResponse is the paginated response returned by GET /users.
type UserListResponse struct {
	Data []models.User  `json:"data"`
	Meta map[string]any `json:"meta"`
}

// MessageResponse is returned for successful operations with a message only.
type MessageResponse struct {
	Message string `json:"message" example:"deleted successfully"`
}

// ErrorResponse is returned when an API request fails.
type ErrorResponse struct {
	Error string `json:"error" example:"invalid request body"`
}

func (h *UserHandler) invalidateUserCache(r *http.Request) {
	if h.Redis == nil {
		return
	}

	// Find and remove all cached user list keys
	iter := h.Redis.Scan(r.Context(), 0, "users:*", 0).Iterator()
	for iter.Next(r.Context()) {
		_ = h.Redis.Del(r.Context(), iter.Val()).Err()
	}
}

// GetUsers godoc
// @Summary      List users
// @Description  Get a paginated list of users with optional search
// @Tags         Users
// @Produce      json
// @Param        page    query     int     false  "Page number (default 1)"
// @Param        limit   query     int     false  "Page size (default 10)"
// @Param        search  query     string  false  "Search term for name or email"
// @Success      200     {object}  UserListResponse
// @Failure      500     {object}  ErrorResponse
// @Router       /users [get]
func (h *UserHandler) GetUsers(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	page, _ := strconv.Atoi(query.Get("page"))
	if page < 1 {
		page = 1
	}

	limit, _ := strconv.Atoi(query.Get("limit"))
	if limit < 1 || limit > 100 {
		limit = 10
	}

	search := query.Get("search")
	offset := (page - 1) * limit

	// Construct unique cache key per page, limit, and search term
	cacheKey := fmt.Sprintf("users:page:%d:limit:%d:search:%s", page, limit, search)

	// 1. Try reading from Redis cache
	if h.Redis != nil {
		cachedData, err := h.Redis.Get(r.Context(), cacheKey).Bytes()
		if err == nil {
			// Cache Hit: serve directly from Redis
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-Cache", "HIT")
			w.WriteHeader(http.StatusOK)
			w.Write(cachedData)
			return
		}
	}

	// 2. Cache Miss: fetch from Postgres
	users, total, err := h.Users.List(r.Context(), models.UserFilters{
		Search: search,
		Limit:  limit,
		Offset: offset,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to fetch users")
		return
	}

	resp := UserListResponse{
		Data: users,
		Meta: map[string]any{
			"current_page":  page,
			"page_size":     limit,
			"total_records": total,
			"total_pages":   (total + limit - 1) / limit,
		},
	}

	// 3. Store result in Redis with a 5-minute TTL
	if h.Redis != nil {
		if payload, err := json.Marshal(resp); err == nil {
			_ = h.Redis.Set(r.Context(), cacheKey, payload, 5*time.Minute).Err()
		}
	}

	w.Header().Set("X-Cache", "MISS")
	writeJSON(w, http.StatusOK, resp)
}

// CreateUser godoc
// @Summary      Create a user
// @Description  Creates a user profile without a password or authentication token
// @Tags         Users
// @Accept       json
// @Produce      json
// @Param        payload body UserRequest true "User details"
// @Success      201 {object} models.User
// @Failure      400 {object} ErrorResponse
// @Failure      409 {object} ErrorResponse
// @Failure      500 {object} ErrorResponse
// @Router       /users [post]
func (h *UserHandler) CreateUser(w http.ResponseWriter, r *http.Request) {
	var input UserRequest
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if input.Name == "" {
		writeError(w, http.StatusBadRequest, "'name' is required")
		return
	}
	if _, err := mail.ParseAddress(input.Email); err != nil {
		writeError(w, http.StatusBadRequest, "invalid email address format")
		return
	}

	user, err := h.Users.Insert(r.Context(), input.Name, input.Email)
	if errors.Is(err, models.ErrDuplicate) {
		writeError(w, http.StatusConflict, "user with this email already exists")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}

	writeJSON(w, http.StatusCreated, user)
}

// GetUserByID godoc
// @Summary      Get a user
// @Description  Returns a user by numeric ID
// @Tags         Users
// @Produce      json
// @Param        id path int true "User ID" example(1)
// @Success      200 {object} models.User
// @Failure      400 {object} ErrorResponse
// @Failure      404 {object} ErrorResponse
// @Failure      500 {object} ErrorResponse
// @Router       /users/{id} [get]
func (h *UserHandler) GetUserByID(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user id")
		return
	}

	user, err := h.Users.GetByID(r.Context(), id)
	if errors.Is(err, models.ErrNotFound) {
		writeError(w, http.StatusNotFound, "user not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}

	writeJSON(w, http.StatusOK, user)
}

// PutUser godoc
// @Summary      Update a user
// @Description  Updates a user's name and email address
// @Tags         Users
// @Accept       json
// @Produce      json
// @Param        id path int true "User ID" example(1)
// @Param        payload body UserRequest true "Updated user details"
// @Success      200 {object} models.User
// @Failure      400 {object} ErrorResponse
// @Failure      404 {object} ErrorResponse
// @Failure      409 {object} ErrorResponse
// @Failure      500 {object} ErrorResponse
// @Router       /users/{id} [put]
func (h *UserHandler) PutUser(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user id")
		return
	}

	var input UserRequest
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if input.Name == "" {
		writeError(w, http.StatusBadRequest, "'name' is required")
		return
	}
	if _, err := mail.ParseAddress(input.Email); err != nil {
		writeError(w, http.StatusBadRequest, "invalid email address format")
		return
	}

	user, err := h.Users.Update(r.Context(), id, input.Name, input.Email)
	if errors.Is(err, models.ErrNotFound) {
		writeError(w, http.StatusNotFound, "user not found")
		return
	} else if errors.Is(err, models.ErrDuplicate) {
		writeError(w, http.StatusConflict, "user with this email already exists")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}

	writeJSON(w, http.StatusOK, user)
}

// DeleteUser godoc
// @Summary      Delete a user
// @Description  Deletes a user by ID. Requires a valid bearer token and administrator privileges.
// @Tags         Users
// @Produce      json
// @Param        id path int true "User ID" example(1)
// @Security     BearerAuth
// @Success      200 {object} MessageResponse
// @Failure      400 {object} ErrorResponse
// @Failure      401 {object} ErrorResponse
// @Failure      403 {object} ErrorResponse
// @Failure      404 {object} ErrorResponse
// @Failure      500 {object} ErrorResponse
// @Router       /users/{id} [delete]
func (h *UserHandler) DeleteUser(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user id")
		return
	}

	if err := h.Users.Delete(r.Context(), id); errors.Is(err, models.ErrNotFound) {
		writeError(w, http.StatusNotFound, "user not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}

	h.invalidateUserCache(r)
	writeJSON(w, http.StatusOK, MessageResponse{Message: "deleted successfully"})
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

// Register godoc
// @Summary      Register a new user
// @Description  Creates a new user profile with a hashed password
// @Tags         Authentication
// @Accept       json
// @Produce      json
// @Param        payload body RegisterRequest true "Registration info"
// @Success      201  {object}  models.User
// @Failure      400  {object}  ErrorResponse
// @Failure      409  {object}  ErrorResponse
// @Failure      500  {object}  ErrorResponse
// @Router       /auth/register [post]
func (h *UserHandler) Register(w http.ResponseWriter, r *http.Request) {
	var input RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if input.Name == "" || len(input.Password) < 6 {
		writeError(w, http.StatusBadRequest, "name and password (min 6 characters) are required")
		return
	}
	if _, err := mail.ParseAddress(input.Email); err != nil {
		writeError(w, http.StatusBadRequest, "invalid email address format")
		return
	}

	user, err := h.Users.Register(r.Context(), input.Name, input.Email, input.Password)
	if errors.Is(err, models.ErrDuplicate) {
		writeError(w, http.StatusConflict, "user with this email already exists")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}

	h.invalidateUserCache(r)
	writeJSON(w, http.StatusCreated, user)
}

// Login godoc
// @Summary      Log in user
// @Description  Authenticates credentials and returns a 24-hour JWT token
// @Tags         Authentication
// @Accept       json
// @Produce      json
// @Param        credentials body LoginRequest true "User login payload"
// @Success      200  {object}  AuthResponse
// @Failure      400  {object}  ErrorResponse
// @Failure      401  {object}  ErrorResponse
// @Failure      500  {object}  ErrorResponse
// @Router       /auth/login [post]
func (h *UserHandler) Login(w http.ResponseWriter, r *http.Request) {
	var input LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	user, err := h.Users.Authenticate(r.Context(), input.Email, input.Password)
	if errors.Is(err, models.ErrInvalidAuth) {
		writeError(w, http.StatusUnauthorized, "invalid email or password")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "server error")
		return
	}

	// Generate JWT (valid for 24 hours)
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id": user.ID,
		"exp":     time.Now().Add(24 * time.Hour).Unix(),
	})

	tokenString, err := token.SignedString([]byte(h.JWTSecret))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not generate token")
		return
	}

	writeJSON(w, http.StatusOK, AuthResponse{Token: tokenString, User: *user})
}

// GetProfile godoc
// @Summary      Get current profile
// @Description  Fetches user details associated with the current JWT token
// @Tags         Users
// @Security     BearerAuth
// @Produce      json
// @Success      200  {object}  models.User
// @Failure      401  {object}  ErrorResponse
// @Failure      404  {object}  ErrorResponse
// @Router       /profile [get]
// GetProfile is protected by JWTMiddleware
func (h *UserHandler) GetProfile(w http.ResponseWriter, r *http.Request) {
	userID, ok := r.Context().Value(middleware.UserIDContextKey).(int)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	user, err := h.Users.GetByID(r.Context(), userID)
	if err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}

	writeJSON(w, http.StatusOK, user)
}
