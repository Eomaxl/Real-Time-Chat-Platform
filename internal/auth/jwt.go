package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var (
	ErrInvalidToken = errors.New("invalid token")
	ErrExpiredToken = errors.New("token has expired")
	ErrInvalidClaim = errors.New("invalid token claims")
)

// JWTService handles JWT token operations
type JWTService struct {
	secretKey       []byte
	accessTokenTTL  time.Duration
	refreshTokenTTL time.Duration
}

// NewJWTService creates a new JWT Service
func NewJWTService(secretKey string, accessTokenTTL, refreshTokenTTL time.Duration) *JWTService {
	return &JWTService{
		secretKey:       []byte(secretKey),
		accessTokenTTL:  accessTokenTTL,
		refreshTokenTTL: refreshTokenTTL,
	}
}

// GenerateTokenPair generates both access and refresh tokens for a user
func (j *JWTService) GenerateTokenPair(user *User) (*TokenPair, error) {
	// Generate access token
	accessToken, err := j.generateToken(user, "access", j.accessTokenTTL)
	if err != nil {
		return nil, fmt.Errorf("failed to generate access token : %w", err)
	}

	// Generate refresh token
	refreshToken, err := j.generateToken(user, "refresh", j.refreshTokenTTL)
	if err != nil {
		return nil, fmt.Errorf("failed to generate refresh token: %w", err)
	}

	return &TokenPair{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresIn:    int64(j.accessTokenTTL.Seconds()),
	}, nil
}

// generateToken generates a JWT token with the specifies type and TTL
func (j *JWTService) generateToken(user *User, tokenType string, ttl time.Duration) (string, error) {
	now := time.Now()
	claims := &Claims{
		UserID:   user.ID,
		Username: user.Username,
		Email:    user.Email,
		TenantID: user.TenantID,
		Type:     tokenType,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
			NotBefore: jwt.NewNumericDate(now),
			Issuer:    "chat-platform",
			Subject:   user.ID,
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
	return token.SignedString(j.secretKey)
}

// generateToken generates a JWT token with the specified type and TTL
func (j *JWTService) ValidateToken(tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		// Verify signing method
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return j.secretKey, nil
	})

	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, ErrExpiredToken
		}
		return nil, ErrInvalidToken
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, ErrInvalidToken
	}

	return claims, nil
}

// RefreshToken generates a new access token using a valid refresh token
func (j *JWTService) RefreshToken(refreshTokenString string) (*TokenPair, error) {
	claims, err := j.ValidateToken(refreshTokenString)
	if err != nil {
		return nil, fmt.Errorf("invalid refresh token: %w", err)
	}

	if claims.Type != "refresh" {
		return nil, ErrInvalidToken
	}

	// Create a user object from claims to generate new tokens
	user := &User{
		ID:       claims.UserID,
		Username: claims.Username,
		Email:    claims.Email,
		TenantID: claims.TenantID,
	}

	return j.GenerateTokenPair(user)

}

// ExtractUserID extracts the user ID from a valid token
func (j *JWTService) ExtractUserID(tokenString string) (string, error) {
	claims, err := j.ValidateToken(tokenString)
	if err != nil {
		return "", err
	}

	return claims.UserID, nil
}
