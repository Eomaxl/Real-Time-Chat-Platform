package tenant

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// Middleware handles tenant context propagation
type Middleware struct {
	repo *Repository
}

// NewMiddleware creates a new tenant middleware
func NewMiddleware(repo *Repository) *Middleware {
	return &Middleware{repo: repo}
}

// InjectTenantContext middleware extracts tenant ID from JWT claims and loads tenant context
func (m *Middleware) InjectTenantContext() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Extract tenant ID from context (set by auth middleware)
		tenantID, exists := c.Get("tenant_id")
		if !exists || tenantID == "" {
			// No tenant ID in token - this might be a system request or single-tenant mode
			// Continue without tenant context
			c.Next()
			return
		}

		tenantIDStr, ok := tenantID.(string)
		if !ok {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "internal_error",
				"message": "Invalid tenant ID format",
			})
			c.Abort()
			return
		}

		// Load tenant context from database
		tenantCtx, err := m.repo.GetTenant(c.Request.Context(), tenantIDStr)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error":   "unauthorized",
				"message": "Invalid tenant",
			})
			c.Abort()
			return
		}

		// Add tenant context to request context
		ctx := WithTenantContext(c.Request.Context(), tenantCtx)
		c.Request = c.Request.WithContext(ctx)

		// Also store in gin context for easy access
		c.Set("tenant_context", tenantCtx)

		c.Next()
	}
}

// RequireTenant middleware ensures a tenant context exists
func (m *Middleware) RequireTenant() gin.HandlerFunc {
	return func(c *gin.Context) {
		_, ok := FromContext(c.Request.Context())
		if !ok {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error":   "unauthorized",
				"message": "Tenant context required",
			})
			c.Abort()
			return
		}

		c.Next()
	}
}

// RequireTier middleware ensures the tenant has a specific tier or higher
func (m *Middleware) RequireTier(minTier string) gin.HandlerFunc {
	tierLevels := map[string]int{
		"free":       1,
		"pro":        2,
		"enterprise": 3,
		"platform":   4,
	}

	return func(c *gin.Context) {
		tenantCtx, ok := FromContext(c.Request.Context())
		if !ok {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error":   "unauthorized",
				"message": "Tenant context required",
			})
			c.Abort()
			return
		}

		currentLevel, exists := tierLevels[tenantCtx.Tier]
		if !exists {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "internal_error",
				"message": "Invalid tenant tier",
			})
			c.Abort()
			return
		}

		requiredLevel, exists := tierLevels[minTier]
		if !exists {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "internal_error",
				"message": "Invalid required tier",
			})
			c.Abort()
			return
		}

		if currentLevel < requiredLevel {
			c.JSON(http.StatusForbidden, gin.H{
				"error":   "forbidden",
				"message": "Insufficient tenant tier",
			})
			c.Abort()
			return
		}

		c.Next()
	}
}

// CheckResourceLimit middleware validates resource limits before allowing operations
func (m *Middleware) CheckResourceLimit(resourceType string) gin.HandlerFunc {
	return func(c *gin.Context) {
		tenantCtx, ok := FromContext(c.Request.Context())
		if !ok {
			// No tenant context - allow operation (single-tenant mode)
			c.Next()
			return
		}

		// Store resource type for later validation
		c.Set("resource_type", resourceType)
		c.Set("tenant_limits", tenantCtx.Limits)

		c.Next()
	}
}

// GetTenantContextFromGin extracts tenant context from gin context
func GetTenantContextFromGin(c *gin.Context) (*TenantContext, bool) {
	tenantCtx, exists := c.Get("tenant_context")
	if !exists {
		return nil, false
	}

	ctx, ok := tenantCtx.(*TenantContext)
	return ctx, ok
}
