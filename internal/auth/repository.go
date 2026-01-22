package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrUserNotFound      = errors.New("user not found")
	ErrUserAlreadyExists = errors.New("user already exists")
	ErrInvalidPassword   = errors.New("invalid password")
)

// Repository handles user data operations
type Repository struct {
	db *pgxpool.Pool
}

// NewRepository creates a new user repository
func NewRepository(db *pgxpool.Pool) *Repository {
	return &Repository{db: db}
}

// CreateUser creates a new user instance
func (r *Repository) CreateUser(ctx context.Context, req *CreateUserRequest) (*User, error) {
	// Hash the password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("failed to hash password: %w", err)
	}

	// Insert user into database
	query := `
		INSERT INTO users (username, email, password_hash, created_at, updated_at)
		VALUES ($1, $2, $3, NOW(), NOW())
		RETURNING id, username, email, created_at, updated_at`

	var user User
	err = r.db.QueryRow(ctx, query, req.Username, req.Email, string(hashedPassword)).Scan(
		&user.ID, &user.Username, &user.Email, &user.CreatedAt, &user.UpdatedAt,
	)

	if err != nil {
		// Check for unique constraint violations
		if err.Error() == "UNIQUE constraint failed" ||
			err.Error() == "duplicate key value violates unique constraint" {
			return nil, ErrUserAlreadyExists
		}
		return nil, fmt.Errorf("failed to create user : %w", err)
	}

	return &user, nil
}

// GetUserByID retrieves user by their ID
func (r *Repository) GetUserByID(ctx context.Context, userID string) (*User, error) {
	query := `
		SELECT id, username , email, password_hash, created_at, updated_at 
		FROM users
		WHERE id = $1`

	var user User

	err := r.db.QueryRow(ctx, query, userID).Scan(
		&user.ID, &user.Username, &user.PasswordHash, &user.CreatedAt, &user.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("failed to get user by ID : %w", err)
	}

	return &user, nil
}

// GetUserByEmail retrieves a user by their email
func (r *Repository) GetUserByEmail(ctx context.Context, email string) (*User, error) {
	query := `
		SELECT id, username, email, password_hash, created_at, updated_at
		FROM users
		WHERE email = $1`

	var user User
	err := r.db.QueryRow(ctx, query, email).Scan(
		&user.ID, &user.Username, &user.Email, &user.PasswordHash, &user.CreatedAt, &user.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("failed to get user by email: %w", err)
	}

	return &user, nil
}

// UpdateUser updates an existing user
func (r *Repository) UpdateUser(ctx context.Context, userID string, req *UpdateUserRequest) (*User, error) {
	// Build dynamic update query
	setParts := []string{}
	args := []interface{}{}
	argIndex := 1

	if req.Username != "" {
		setParts = append(setParts, fmt.Sprintf("username = $%d", argIndex))
		args = append(args, req.Username)
		argIndex++
	}

	if req.Email != "" {
		setParts = append(setParts, fmt.Sprintf("email = $%d", argIndex))
		args = append(args, req.Email)
		argIndex++
	}

	if req.Password != "" {
		hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
		if err != nil {
			return nil, fmt.Errorf("failed to hash password: %w", err)
		}
		setParts = append(setParts, fmt.Sprintf("password_hash = $%d", argIndex))
		args = append(args, string(hashedPassword))
		argIndex++
	}

	if len(setParts) == 0 {
		// No updates requested, return current user
		return r.GetUserByID(ctx, userID)
	}

	// Add updated_at and userID
	setParts = append(setParts, fmt.Sprintf("updated_at = $%d", argIndex))
	args = append(args, time.Now())
	argIndex++
	args = append(args, userID)

	query := fmt.Sprintf(`
		UPDATE users
		SET %s
		WHERE id = $%d
		RETURNING id, username, email, created_at, updated_at`,
		fmt.Sprintf("%s", setParts[0]),
		argIndex,
	)

	// Handle multiple SET clauses
	if len(setParts) > 1 {
		setClause := ""
		for i, part := range setParts {
			if i > 0 {
				setClause += ", "
			}
			setClause += part
		}
		query = fmt.Sprintf(`
			UPDATE users
			SET %s
			WHERE id = $%d
			RETURNING id, username, email, created_at, updated_at`,
			setClause,
			argIndex,
		)
	}

	var user User
	err := r.db.QueryRow(ctx, query, args...).Scan(
		&user.ID, &user.Username, &user.Email, &user.CreatedAt, &user.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("failed to update user: %w", err)
	}

	return &user, nil
}

// DeleteUser deletes a user by their ID
func (r *Repository) DeleteUser(ctx context.Context, userID string) error {
	query := `DELETE FROM user WHERE id = $1`

	result, err := r.db.Exec(ctx, query, userID)
	if err != nil {
		return fmt.Errorf("failed to delete user : %w", err)
	}

	if result.RowsAffected() == 0 {
		return ErrUserNotFound
	}

	return nil
}

// AuthenticateUser authenticates a user with email and password
func (r *Repository) AuthenticateUser(ctx context.Context, email, password string) (*User, error) {
	user, err := r.GetUserByEmail(ctx, email)
	if err != nil {
		return nil, err
	}

	// Compare the password with hash
	err = bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password))
	if err != nil {
		return nil, ErrInvalidPassword
	}

	return user, nil
}
