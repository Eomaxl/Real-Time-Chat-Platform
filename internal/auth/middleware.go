package auth

import (
	"context"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// ContextKey represents keys used in request context
type ContextKey string

const (
	UserIDKey   ContextKey = "user_id"
	UserKey     ContextKey = "user_key"
	ClaimsKey   ContextKey = "claims"
	TenantIDKey ContextKey = "tentant_id"
)

// Middleware handles JWT authentication and authorization
type Middleware struct {
	jwtService *JWTService
	authRepo   *Repository
}

// NewMiddleware creates a new authentication middleware
func NewMiddleware(jwtService *JWTService, authRepo *Repository) *Middleware {
	return &Middleware{
		jwtService: jwtService,
		authRepo:   authRepo,
	}
}

// RequireAuth middleware validates JWT tokens for protected endpoints
func (m *Middleware) RequireAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Extract token from Authorization header
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error":   "unauthorized",
				"message": "Authorization header is required",
			})
			c.Abort()
			return
		}

		// Check bearer token format
		tokenParts := strings.Split(authHeader, " ")
		if len(tokenParts) != 2 || tokenParts[0] != "Bearer" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error":   "unauthorized",
				"message": "Invalid authorization header format",
			})
			c.Abort()
			return
		}

		tokenString := tokenParts[1]

		// Validate token
		claims, err := m.jwtService.ValidateToken(tokenString)
		if err != nil {
			status := http.StatusUnauthorized
			message := "Invalid token"

			if err == ErrExpiredToken {
				message = "Token has expired"
			}

			c.JSON(status, gin.H{
				"error":   "unauthorized",
				"message": message,
			})
			c.Abort()
			return
		}

		// Only allow access tokens for API requests
		if claims.Type != "access" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error":   "unauthorized",
				"message": "Invalid token type",
			})
			c.Abort()
			return
		}

		// Store user information in context
		c.Set(string(UserIDKey), claims.UserID)
		c.Set(string(ClaimsKey), claims)
		if claims.TenantID != "" {
			c.Set(string(TenantIDKey), claims.TenantID)
		}
		c.Next()
	}
}

// RequireChannelMembership middleware validates that the user is a member of the specified channel
func (m *Middleware) RequireChannelMembership() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Get user ID from context (set by RequireAuth middleware)
		userID, exists := c.Get(string(UserIDKey))
		if !exists {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error":   "unauthorized",
				"message": "User not authenticated",
			})
			c.Abort()
			return
		}

		// Get channel ID from URL parameter
		channelID := c.Param("channelId")
		if channelID == "" {
			channelID = c.Param("id")
		}

		if channelID == "" {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":   "bad_request",
				"message": "Channel ID is required",
			})
			c.Abort()
			return
		}

		// Check channel membership
		isMember, err := m.isChannelMember(c.Request.Context(), userID.(string), channelID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "internal_error",
				"message": "Failed to verify channel membership",
			})
			c.Abort()
			return
		}
		c.Next()

		if !isMember {
			c.JSON(http.StatusForbidden, gin.H{
				"error":   "forbidden",
				"message": "User is not a member of this channel",
			})
			c.Abort()
			return
		}
		c.Next()
	}
}

// RequireRole middleware validates that the user has the specified role
func (m *Middleware) RequireRole(requiredRole string) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Get user ID from context
		userID, exists := c.Get(string(UserIDKey))
		if !exists {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error":   "unauthorized",
				"message": "User not authorized",
			})
			c.Abort()
			return
		}

		// Get channelID if present
		channelID := c.Param("channelID")
		if channelID == "" {
			channelID = c.Param("id")
		}

		// Check user role
		userRole, err := m.getUserRole(c.Request.Context(), userID.(string), channelID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "internal_error",
				"message": "Failed to verify user role",
			})
			c.Abort()
			return
		}

		// Check if the user has required role
		if !m.hasRequiredRole(userRole, requiredRole) {
			c.JSON(http.StatusForbidden, gin.H{
				"error":   "forbidden",
				"message": "Insufficient permissions",
			})
			c.Abort()
			return
		}

		c.Next()
	}
}

// WebsocketAuth validates JWT tokens for websocket connections
func (m *Middleware) WebsocketAuth(tokenString string) (*Claims, error) {
	if tokenString == "" {
		return nil, ErrInvalidToken
	}

	// Validate token
	claims, err := m.jwtService.ValidateToken(tokenString)
	if err != nil {
		return nil, err
	}

	// Only allow access tokens for Websocket connections
	if claims.Type != "access" {
		return nil, ErrInvalidToken
	}

	return claims, err
}

// isChannelMember checks if a user is a member of the specified channel
func (m *Middleware) isChannelMember(ctx context.Context, userID, channelID string) (bool, error) {
	// We are implementing a basic check using auth repository's database connection
	db := m.authRepo.db

	query := `SELECT EXISTS( SELECT 1 FROM channel_members WHERE user_id = $1 AND channel_id = $2)`

	var exists bool
	err := db.QueryRow(ctx, query, userID, channelID).Scan(&exists)
	if err != nil {
		return false, err
	}
	return exists, nil
}

// getUserRole get the user's role in the channel (or global role if no channel specified)
func (m *Middleware) getUserRole(ctx context.Context, userID, channelID string) (string, error) {
	db := m.authRepo.db

	if channelID == "" {
		query := `SELECT role FROM channel_members WHERE user_id = $1 AND channel_id = $2`

		var role string
		err := db.QueryRow(ctx, query, userID, channelID).Scan(&role)
		if err != nil {
			return "", err
		}
		return role, nil
	}
	// Typically we would have a separate user_roles table. For now returning "member" as default
	return "member", nil
}

// hasRequredRole checks if the user's role meets the requirement
func (m *Middleware) hasRequiredRole(userRole, requiredRole string) bool {
	// Define role heirarchy
	roleHierarchy := map[string]int{
		"member":    1,
		"moderator": 2,
		"admin":     3,
		"owner":     4,
	}

	userLevel, userExists := roleHierarchy[userRole]
	requiredLevel, requiredExists := roleHierarchy[requiredRole]

	if !userExists || !requiredExists {
		return false
	}

	return userLevel >= requiredLevel
}

// GetUserIDFromContext extracts user ID from gin context
func GetUserIDFromContext(c *gin.Context) (string, bool) {
	userID, exists := c.Get(string(UserIDKey))
	if !exists {
		return "", false
	}

	userIDStr, ok := userID.(string)
	return userIDStr, ok
}

// GetClaimsFromContext extracts JWT claims from gin context
func GetClaimsFromContext(c *gin.Context) (*Claims, bool) {
	claims, exists := c.Get(string(ClaimsKey))
	if !exists {
		return nil, false
	}

	claimsObj, ok := claims.(*Claims)
	return claimsObj, ok
}

// GetTenantIDFromContext extracts tenant ID from gin context
func GetTenantIDFromContext(c *gin.Context) (string, bool) {
	tenantID, exists := c.Get(string(TenantIDKey))
	if !exists {
		return "", false
	}

	tenantIDStr, ok := tenantID.(string)
	return tenantIDStr, ok
}
