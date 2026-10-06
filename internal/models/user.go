package models

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/lib/pq"
)

var (
	ErrNotFound    = errors.New("record not found")
	ErrDuplicate   = errors.New("duplicate record")
	ErrInvalidAuth = errors.New("invalid credentials")
)

type User struct {
	ID           int       `json:"id" example:"1" gorm:"primaryKey;autoIncrement"`
	Name         string    `json:"name" example:"Aqib Ahmed" gorm:"type:text;not null"`
	Email        string    `json:"email" example:"aqib@example.com" gorm:"type:text;not null;uniqueIndex"`
	PasswordHash string    `json:"-" example:"$2a$10$..." gorm:"column:password_hash;type:text;not null;default:''"`
	Role         string    `json:"role" example:"user" gorm:"type:text;not null;default:user"`
	CreatedAt    time.Time `json:"created_at" example:"2025-01-01T00:00:00Z" gorm:"type:timestamptz;not null;default:CURRENT_TIMESTAMP"`
	UpdatedAt    time.Time `json:"updated_at" example:"2025-01-01T00:00:00Z" gorm:"type:timestamptz;not null;default:CURRENT_TIMESTAMP"`
}

type UserModel struct {
	DB *sql.DB
}

// UserFilters holds pagination and search parameters
type UserFilters struct {
	Search string
	Limit  int
	Offset int
}

func (m *UserModel) GetAll(ctx context.Context) ([]User, error) {
	rows, err := m.DB.QueryContext(ctx, "SELECT id, name, email, role, created_at, updated_at FROM users ORDER BY id ASC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	users := make([]User, 0)
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Name, &u.Email, &u.Role, &u.CreatedAt, &u.UpdatedAt); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, nil
}

func (m *UserModel) GetByID(ctx context.Context, id int) (*User, error) {
	var u User
	query := "SELECT id, name, email, role, created_at, updated_at FROM users WHERE id = $1"
	err := m.DB.QueryRowContext(ctx, query, id).Scan(&u.ID, &u.Name, &u.Email, &u.Role, &u.CreatedAt, &u.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	return &u, nil
}

func (m *UserModel) Insert(ctx context.Context, name, email string) (*User, error) {
	var u User
	query := `INSERT INTO users (name, email)
	          VALUES ($1, $2) 
	          RETURNING id, name, email, role, created_at, updated_at`
	err := m.DB.QueryRowContext(ctx, query, name, email).Scan(&u.ID, &u.Name, &u.Email, &u.Role, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == "23505" {
			return nil, ErrDuplicate
		}
		return nil, err
	}
	return &u, nil
}

func (m *UserModel) Update(ctx context.Context, id int, name, email string) (*User, error) {
	var u User
	query := `UPDATE users
	          SET name = $1, email = $2, updated_at = CURRENT_TIMESTAMP 
	          WHERE id = $3 
	          RETURNING id, name, email, role, created_at, updated_at`
	err := m.DB.QueryRowContext(ctx, query, name, email, id).Scan(&u.ID, &u.Name, &u.Email, &u.Role, &u.CreatedAt, &u.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	} else if err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == "23505" {
			return nil, ErrDuplicate
		}
		return nil, err
	}
	return &u, nil
}

func (m *UserModel) Delete(ctx context.Context, id int) error {
	res, err := m.DB.ExecContext(ctx, "DELETE FROM users WHERE id = $1", id)
	if err != nil {
		return err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func (m *UserModel) List(ctx context.Context, f UserFilters) ([]User, int, error) {
	if f.Limit <= 0 || f.Limit > 100 {
		f.Limit = 10 // Default page size
	}
	if f.Offset < 0 {
		f.Offset = 0
	}

	// Dynamic WHERE clause for search
	whereClause := ""
	args := []any{}
	argID := 1

	if f.Search != "" {
		whereClause = fmt.Sprintf("WHERE name ILIKE $%d OR email ILIKE $%d", argID, argID)
		args = append(args, "%"+f.Search+"%")
		argID++
	}

	// 1. Get total matching count
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM users %s", whereClause)
	var totalCount int
	if err := m.DB.QueryRowContext(ctx, countQuery, args...).Scan(&totalCount); err != nil {
		return nil, 0, err
	}

	// 2. Fetch paginated records
	dataQuery := fmt.Sprintf(`
		SELECT id, name, email, role, created_at, updated_at
		FROM users 
		%s 
		ORDER BY id ASC 
		LIMIT $%d OFFSET $%d`, whereClause, argID, argID+1)

	args = append(args, f.Limit, f.Offset)

	rows, err := m.DB.QueryContext(ctx, dataQuery, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	users := make([]User, 0)
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Name, &u.Email, &u.Role, &u.CreatedAt, &u.UpdatedAt); err != nil {
			return nil, 0, err
		}
		users = append(users, u)
	}

	return users, totalCount, nil
}

// Register hashes password and saves new user
func (m *UserModel) Register(ctx context.Context, name, email, password string) (*User, error) {
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("failed to hash password: %w", err)
	}

	var u User
	query := `INSERT INTO users (name, email, password_hash)
	          VALUES ($1, $2, $3) 
	          RETURNING id, name, email, role, created_at, updated_at`

	err = m.DB.QueryRowContext(ctx, query, name, email, string(hashedPassword)).
		Scan(&u.ID, &u.Name, &u.Email, &u.Role, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == "23505" {
			return nil, ErrDuplicate
		}
		return nil, err
	}

	return &u, nil
}

// Authenticate checks user credentials
func (m *UserModel) Authenticate(ctx context.Context, email, password string) (*User, error) {
	var u User
	query := `SELECT id, name, email, password_hash, role, created_at, updated_at FROM users WHERE email = $1`
	err := m.DB.QueryRowContext(ctx, query, email).
		Scan(&u.ID, &u.Name, &u.Email, &u.PasswordHash, &u.Role, &u.CreatedAt, &u.UpdatedAt)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrInvalidAuth
	} else if err != nil {
		return nil, err
	}

	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)); err != nil {
		return nil, ErrInvalidAuth
	}

	return &u, nil
}

// func (m *UserModel) List(ctx context.Context, f UserFilters) ([]User, int, error) {
// 	if f.Limit <= 0 || f.Limit > 100 {
// 		f.Limit = 10
// 	}
// 	if f.Offset < 0 {
// 		f.Offset = 0
// 	}

// 	whereClause := ""
// 	args := []any{}
// 	argID := 1

// 	if f.Search != "" {
// 		whereClause = fmt.Sprintf("WHERE name ILIKE $%d OR email ILIKE $%d", argID, argID)
// 		args = append(args, "%"+f.Search+"%")
// 		argID++
// 	}

// 	var totalCount int
// 	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM users %s", whereClause)
// 	if err := m.DB.QueryRowContext(ctx, countQuery, args...).Scan(&totalCount); err != nil {
// 		return nil, 0, err
// 	}

// 	dataQuery := fmt.Sprintf(`
// 		SELECT id, name, email, created_at, updated_at
// 		FROM users
// 		%s
// 		ORDER BY id ASC
// 		LIMIT $%d OFFSET $%d`, whereClause, argID, argID+1)

// 	args = append(args, f.Limit, f.Offset)
// 	rows, err := m.DB.QueryContext(ctx, dataQuery, args...)
// 	if err != nil {
// 		return nil, 0, err
// 	}
// 	defer rows.Close()

// 	users := make([]User, 0)
// 	for rows.Next() {
// 		var u User
// 		if err := rows.Scan(&u.ID, &u.Name, &u.Email, &u.CreatedAt, &u.UpdatedAt); err != nil {
// 			return nil, 0, err
// 		}
// 		users = append(users, u)
// 	}

// 	return users, totalCount, nil
// }
