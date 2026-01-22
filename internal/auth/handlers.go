package auth

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// Handler handles HTTP requests for authentication
type Handler struct {
	service *Service
}

// NewHandler creates a new authentication handler
func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

// Register handles user registration
func (h *Handler) Register(c *gin.Context) {
	var req CreateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "bad_request",
			"message": "Invalid request body",
			"details": err.Error(),
		})
		return
	}

	user, tokens, err := h.service.Register(c.Request.Context(), &req)
	if err != nil {
		status := http.StatusInternalServerError
		message := "Registration failed"

		if err == ErrUserAlreadyExists {
			status = http.StatusConflict
			message = "User already exists"
		}

		c.JSON(status, gin.H{
			"error":   "registration_failed",
			"message": message,
		})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"user":   user,
		"tokens": tokens,
	})
}

// Login handles user login
func (h *Handler) Login(c *gin.HandlerFunc) {
	var req LoginRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "bad_request",
			"message": "Invalid request body",
			"details": err.Error(),
		})
		return
	}

	user, tokens, err := h.service.Login(c.Request.Context(), &req)
	if err != nil {
		status := http.StatusUnauthorized
		message := "Authentication failed"

		if err == ErrUserNotFound || err == ErrInvalidPassword {
			message = "Invalid email or password"
		}

		c.JSON(status, gin.H{
			"error":   "authentication_failed",
			"message": message,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"user":   user,
		"tokens": tokens,
	})
}

// RefreshToken handles the token refresh
func (h *Handler) RefreshToken(c *gin.Context) {
	var req RefreshTokenRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "bad_request",
			"message": "Invalid request body",
			"details": err.Error(),
		})
		return
	}

	tokens, err := h.service.RefreshToken(c.Request.Context(), &req)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error":   "token_refresh_failed",
			"message": "Invalid refresh token",
		})
		return
	}

	c.JSON(http.StatusOK, gin.h{
		"tokens": tokens,
	})
}

// GetProfile handles getting user profile
func (h *Handler) GetProfile(c *gin.Context) {
	userID, exists := GetUserIDFromContext(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error":   "unauthorized",
			"message": "User not authenticated",
		})
		return
	}

	user, err := h.service.GetUser(c.Request.Context(), userID)
	if err != nil {
		status := http.StatusInternalServerError
		message := "Failed to get profile"

		if err == ErrUserNotFound {
			status = http.StatusNotFound
			message = "User not found"
		}

		c.JSON(status, gin.H{
			"error":   "profile_fetch_failed",
			"message": message,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"user": user,
	})
}

// UpdateProfile handles updating user profile
func (h *Handler) UpdateProfile(c *gin.Context) {
	userID, exists := GetUserIDFromContext(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error":   "unauthorized",
			"message": "User not authenticated",
		})
		return
	}

	var req UpdateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "bad_request",
			"message": "Invalid request body",
			"details": err.Error(),
		})
		return
	}

	user, err := h.service.UpdateUser(c.Request.Context(), userID, &req)
	if err != nil {
		status := http.StatusInternalServerError
		message := "Failed to update user profile"

		if err == ErrUserNotFound {
			status = http.StatusNotFound
			message = "User not found"
		} else if err == ErrUserAlreadyExists {
			status = http.StatusConflict
			message = "Username or email already exists"
		}

		c.JSON(status, gin.H{
			"error":   "profile_update_failed",
			"message": message,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"user": user,
	})
}

// DeleteAccount handles account deletion
func (h *Handler) DeleteAccount(c *gin.Context) {
	userID, exists := GetUserIDFromContext(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error":   "unauthorized",
			"message": "User not authenticated",
		})
		return
	}

	err := h.service.DeleteUser(c.Request.Context(), userID)
	if err != nil {
		status := http.StatusInternalServerError
		message := "Failed to delete account"

		if err == ErrUserNotFound {
			status = http.StatusNotFound
			message = "User not found"
		}

		c.JSON(status, gin.H{
			"error":   "account_deletion_failed",
			"message": message,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Account deleted successfully",
	})
}

// RegisterRoutes registers authentication routes
func (h *Handler) RegisterRoutes(router *gin.Engine, middleware *Middleware) {
	auth := router.Group("/v1/auth")
	{
		auth.POST("/register", h.Register)
		auth.POST("/login", h.Login)
		auth.POST("/refresh", h.RefreshToken)
	}

	// Protected routes
	profile := router.Group("/v1/profile")
	profile.Use(middleware.RequireAuth())
	{
		profile.GET("", h.GetProfile)
		profile.PUT("", h.UpdateProfile)
		profile.DELETE("", h.DeleteAccount)
	}
}
