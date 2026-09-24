// Package middleware provides HTTP middleware: request IDs, structured logging,
// recovery, CORS, and JWT authentication.
package middleware

import (
	"context"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/visionforge/visionforge/apps/api/internal/auth"
	"github.com/visionforge/visionforge/apps/api/internal/observability"
	vtypes "github.com/visionforge/visionforge/packages/types"
)

// ctxKey is private to avoid collisions.
type ctxKey string

const userCtxKey ctxKey = "user"

// RequestID injects a request ID (from header or generated).
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		rid := c.GetHeader("X-Request-ID")
		if rid == "" {
			rid = auth.GenerateRequestID()
		}
		c.Writer.Header().Set("X-Request-ID", rid)
		ctx := observability.WithRequestID(c.Request.Context(), rid)
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}

// StructuredLogger logs every request with timing and status.
func StructuredLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.FullPath()
		if path == "" {
			path = c.Request.URL.Path
		}
		c.Next()
		latency := time.Since(start)
		status := c.Writer.Status()
		log := observability.Logger(c.Request.Context()).With(
			"method", c.Request.Method,
			"route", path,
			"status", status,
			"latency_ms", latency.Milliseconds(),
			"remote", c.ClientIP(),
		)
		if len(c.Errors) > 0 {
			log.Error("request failed", "errors", c.Errors.String())
		} else if status >= 500 {
			log.Error("server error")
		} else if status >= 400 {
			log.Warn("client error")
		} else {
			log.Info("request")
		}
	}
}

// Recovery recovers from panics and returns 500 without crashing the process.
func Recovery() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if r := recover(); r != nil {
				observability.Logger(c.Request.Context()).Error("panic recovered",
					"panic", r, "stack", string(debug.Stack()))
				c.AbortWithStatusJSON(http.StatusInternalServerError, vtypes.NewError("INTERNAL_ERROR", "internal server error", observability.RequestID(c.Request.Context())))
			}
		}()
		c.Next()
	}
}

// CORS applies permissive-but-constrained CORS headers.
func CORS(allowedOrigins []string) gin.HandlerFunc {
	allowed := map[string]bool{}
	for _, o := range allowedOrigins {
		allowed[o] = true
	}
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if origin != "" && allowed[origin] {
			c.Writer.Header().Set("Access-Control-Allow-Origin", origin)
			c.Writer.Header().Set("Vary", "Origin")
			c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
			c.Writer.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Idempotency-Key, X-Request-ID")
			c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			c.Writer.Header().Set("Access-Control-Max-Age", "600")
		}
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

// JWTAuth guards routes and injects the authenticated user into context.
func JWTAuth(jwtm *auth.JWTManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		authz := c.GetHeader("Authorization")
		if authz == "" || !strings.HasPrefix(authz, "Bearer ") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, vtypes.NewError("UNAUTHORIZED", "missing or invalid bearer token", observability.RequestID(c.Request.Context())))
			return
		}
		tok := strings.TrimPrefix(authz, "Bearer ")
		claims, err := jwtm.ParseAccessToken(tok)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, vtypes.NewError("UNAUTHORIZED", "invalid token", observability.RequestID(c.Request.Context())))
			return
		}
		user := &vtypes.User{ID: claims.UserID, Role: claims.Role}
		c.Set("user_id", claims.UserID)
		c.Set("user_role", string(claims.Role))
		c.Set(string(userCtxKey), user)
		c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), userCtxKey, user))
		c.Next()
	}
}

// CurrentUser returns the user injected by JWTAuth, or nil.
func CurrentUser(c *gin.Context) *vtypes.User {
	v, ok := c.Get(string(userCtxKey))
	if !ok {
		return nil
	}
	u, _ := v.(*vtypes.User)
	return u
}

// RequireRole ensures the current user has at least the specified role.
func RequireRole(role vtypes.Role) gin.HandlerFunc {
	return func(c *gin.Context) {
		u := CurrentUser(c)
		if u == nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, vtypes.NewError("UNAUTHORIZED", "authentication required", observability.RequestID(c.Request.Context())))
			return
		}
		if u.Role != vtypes.RoleAdmin && u.Role != role {
			c.AbortWithStatusJSON(http.StatusForbidden, vtypes.NewError("FORBIDDEN", "insufficient role", observability.RequestID(c.Request.Context())))
			return
		}
		c.Next()
	}
}

// RequireRoleString is a convenience wrapper accepting a string role.
func RequireRoleString(role string) gin.HandlerFunc { return RequireRole(vtypes.Role(role)) }

// MetricsMiddleware records prometheus HTTP metrics.
func MetricsMiddleware(reqCnt *prometheus.CounterVec, reqDur *prometheus.HistogramVec) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.FullPath()
		c.Next()
		status := http.StatusText(c.Writer.Status())
		reqDur.WithLabelValues(c.Request.Method, path).Observe(time.Since(start).Seconds())
		reqCnt.WithLabelValues(c.Request.Method, path, status).Inc()
	}
}

// ClientIP returns a trustworthy client IP (after proxies).
func ClientIP(c *gin.Context) string {
	if xff := c.GetHeader("X-Forwarded-For"); xff != "" {
		if i := strings.Index(xff, ","); i >= 0 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	return c.ClientIP()
}
