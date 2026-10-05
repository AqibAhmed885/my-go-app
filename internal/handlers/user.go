package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/mail"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/AqibAhmed885/my-go-app/internal/middleware"
	"github.com/AqibAhmed885/my-go-app/internal/models"
)

type UserHandler struct {
	Users     *models.UserModel
	JWTSecret string
}

// GetUsers godoc
// @Summary      List users
// @Description  Get a paginated list of users with optional search
// @Tags         Users
// @Produce      json
// @Param        page    query     int     false  "Page number (default 1)"
// @Param        limit   query     int     false  "Page size (default 10)"
// @Param        search  query     string  false  "Search term for name or email"
// @Success      200     {object}  map[string]any
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

	users, total, err := h.Users.List(r.Context(), models.UserFilters{
		Search: search,
		Limit:  limit,
		Offset: offset,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to fetch users")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"data": users,
		"meta": map[string]any{
			"current_page":  page,
			"page_size":     limit,
			"total_records": total,
			"total_pages":   (total + limit - 1) / limit,
		},
	})
}

func (h *UserHandler) CreateUser(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	}
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

func (h *UserHandler) PutUser(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user id")
		return
	}

	var input struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	}
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

	writeJSON(w, http.StatusOK, map[string]string{"message": "deleted successfully"})
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
// @Param        payload body object true "Registration info"
// @Success      201  {object}  models.User
// @Failure      400  {object}  map[string]string
// @Failure      409  {object}  map[string]string
// @Router       /auth/register [post]
func (h *UserHandler) Register(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name     string `json:"name"`
		Email    string `json:"email"`
		Password string `json:"password"`
	}
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

	writeJSON(w, http.StatusCreated, user)
}

// Login godoc
// @Summary      Log in user
// @Description  Authenticates credentials and returns a 24-hour JWT token
// @Tags         Authentication
// @Accept       json
// @Produce      json
// @Param        credentials body object true "User login payload"
// @Success      200  {object}  map[string]any
// @Failure      401  {object}  map[string]string
// @Router       /auth/login [post]
func (h *UserHandler) Login(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
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

	writeJSON(w, http.StatusOK, map[string]any{
		"token": tokenString,
		"user":  user,
	})
}

// GetProfile godoc
// @Summary      Get current profile
// @Description  Fetches user details associated with the current JWT token
// @Tags         Users
// @Security     BearerAuth
// @Produce      json
// @Success      200  {object}  models.User
// @Failure      401  {object}  map[string]string
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

