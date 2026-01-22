package auth

import (
	"context"
	"fmt"
	"time"
)

// Service handles authentication business logic
type Service struct {
	repo       *Repository
	jwtService *JWTService
}

// NewService creates a new authentication service
func NewService(repo *Repository, jwtService *JWTService) *Service {
	return &Service{
		repo:       repo,
		jwtService: jwtService,
	}
}

// Register creates a new user account
func (s *Service) Register(ctx context.Context, req *CreateUserRequest) (*User, *TokenPair, error) {
	// Create User
	user, err := s.repo.CreateUser(ctx, req)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create user: %w", err)
	}

	// Generate tokens
	tokens, err := s.jwtService.GenerateTokenPair(user)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create user: %w", err)
	}
	return user, tokens, nil
}

// Login authenticates a user and returns tokens
func (s *Service) Login(ctx context.Context, req *LoginRequest) (*User, *TokenPair, error) {
	// Authentication user
	user, err := s.repo.AuthenticateUser(ctx, req.Email, req.Password)
	if err != nil {
		return nil, nil, fmt.Errorf("authentication failed : %w", err)
	}

	// Generate tokens
	tokens, err := s.jwtService.GenerateTokenPair(user)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate tokens : %w", err)
	}

	return user, tokens, nil
}

// RefreshToken generates  new tokens using refresh token
func (s *Service) RefreshToken(ctx context.Context, req *RefreshTokenRequest) (*TokenPair, error) {
	tokens, err := s.jwtService.RefreshToken(req.RefreshToken)
	if err != nil {
		return nil, fmt.Errorf("failed to refresh token : %w", err)
	}

	return tokens, nil
}

// GetUser retrieves a user by ID
func (s *Service) GetUser(ctx context.Context, userID string) (*User, error) {
	user, err := s.repo.GetUserByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get user : %w", err)
	}

	return user, nil
}

// UpdateUser updates user information
func (s *Service) UpdateUser(ctx context.Context, userID string, req *UpdateUserRequest) (*User, error) {
	user, err := s.repo.UpdateUser(ctx, userID, req)
	if err != nil {
		return nil, fmt.Errorf("failed to update user : %w", err)
	}

	return user, nil
}

// DeleteUser deletes a user account
func (s *Service) DeleteUser(ctx context.Context, userID string) error {
	err := s.repo.DeleteUser(ctx, userID)
	if err != nil {
		return fmt.Errorf("failed to delete user: %w", err)
	}
	return nil
}

// NewJWTServiceFromConfig creates a JWT Service from configuration
func NewJWTServiceFromConfig(jwtSecret string) *JWTService {
	return NewJWTService(
		jwtSecret,
		15*time.Minute,
		7*24*time.Hour,
	)
}
