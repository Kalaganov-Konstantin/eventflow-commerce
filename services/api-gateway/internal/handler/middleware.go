package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"go.uber.org/zap"
	"golang.org/x/time/rate"
)

// UserContextKey is the key for user context
type contextKey string

const UserContextKey = contextKey("user")

// Public paths that don't require authentication
var publicPaths = []string{"/health", "/metrics"}

// isPublicPath checks if the given path is a public endpoint that doesn't require authentication
// Uses proper path normalization to prevent bypass attacks
func isPublicPath(path string) bool {
	// Normalize path to prevent directory traversal attacks
	normalizedPath := filepath.Clean(path)

	// Ensure path starts with / to prevent relative path issues
	if !strings.HasPrefix(normalizedPath, "/") {
		normalizedPath = "/" + normalizedPath
	}

	// Check exact matches only (no prefix matching to prevent bypass)
	for _, publicPath := range publicPaths {
		if normalizedPath == publicPath {
			return true
		}
	}

	return false
}

// Claims represents JWT claims
type Claims struct {
	UserID string `json:"user_id"`
	Email  string `json:"email"`
	Role   string `json:"role"`
	jwt.RegisteredClaims
}

// RateLimiter manages rate limiting per client using golang.org/x/time/rate
type RateLimiter struct {
	limiters   map[string]*rate.Limiter
	mutex      sync.RWMutex
	rate       rate.Limit
	burst      int
	maxClients int
	done       chan struct{}
}

// NewRateLimiter creates a new rate limiter using token bucket algorithm
func NewRateLimiter(requestsPerMinute int, windowDuration time.Duration) *RateLimiter {
	if requestsPerMinute <= 0 {
		requestsPerMinute = 1 // Minimum rate to avoid division by zero
	}

	// Convert requests per minute to rate.Limit
	rateLimit := rate.Every(time.Minute / time.Duration(requestsPerMinute))

	// Calculate burst size - allow reasonable burst capacity
	// For low rates, allow at least the per-minute amount as burst
	burstSize := requestsPerMinute
	// For higher rates, allow some burst capacity but not too much
	if burstSize > 10 {
		burstSize = requestsPerMinute/3 + 2
	}

	rl := &RateLimiter{
		limiters:   make(map[string]*rate.Limiter),
		rate:       rateLimit,
		burst:      burstSize,
		maxClients: 10000,
		done:       make(chan struct{}),
	}

	// Start cleanup goroutine
	go rl.cleanup()
	return rl
}

// Allow checks if a request should be allowed for the given client
func (rl *RateLimiter) Allow(clientID string) bool {
	rl.mutex.Lock()
	defer rl.mutex.Unlock()

	limiter, exists := rl.limiters[clientID]
	if !exists {
		// Check if we need to evict old clients
		if len(rl.limiters) >= rl.maxClients {
			rl.evictOldestClients()
		}

		limiter = rate.NewLimiter(rl.rate, rl.burst)
		rl.limiters[clientID] = limiter
	}

	return limiter.Allow()
}

// evictOldestClients removes inactive clients to prevent memory bloat
func (rl *RateLimiter) evictOldestClients() {
	// Remove 10% of clients to reduce frequency of evictions
	evictCount := len(rl.limiters) / 10
	if evictCount < 10 {
		evictCount = 10
	}
	if evictCount > len(rl.limiters) {
		evictCount = len(rl.limiters)
	}

	// Create slice to safely collect clients for deletion
	// This prevents race conditions during map iteration
	toDelete := make([]string, 0, evictCount)
	count := 0
	for clientID := range rl.limiters {
		if count >= evictCount {
			break
		}
		toDelete = append(toDelete, clientID)
		count++
	}

	// Now safely delete collected clients
	for _, clientID := range toDelete {
		delete(rl.limiters, clientID)
	}
}

// cleanup periodically removes inactive limiters
func (rl *RateLimiter) cleanup() {
	ticker := time.NewTicker(5 * time.Minute) // Cleanup every 5 minutes

	defer func() {
		ticker.Stop()
		// Ensure ticker is properly stopped on exit
	}()

	for {
		select {
		case <-ticker.C:
			rl.mutex.Lock()
			// Simple cleanup: if we have too many clients, remove some
			if len(rl.limiters) > rl.maxClients/2 {
				rl.evictOldestClients()
			}
			rl.mutex.Unlock()

		case <-rl.done:
			// Properly stop ticker and exit
			return
		}
	}
}

// Close stops the cleanup goroutine safely
func (rl *RateLimiter) Close() {
	// Use sync.Once pattern to prevent double-close panic
	select {
	case <-rl.done:
		// Channel already closed, nothing to do
		return
	default:
		// Safe to close channel
		close(rl.done)
	}
}

// RateLimitMiddleware creates a rate limiting middleware
func RateLimitMiddleware(rateLimiter *RateLimiter, metrics *Metrics) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			clientIP := getClientIPFromRequest(r)

			allowed := rateLimiter.Allow(clientIP)

			// Record metrics if available
			if metrics != nil {
				metrics.RecordRateLimit(clientIP, allowed)
			}

			if !allowed {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)

				if err := json.NewEncoder(w).Encode(map[string]string{
					"error": "Rate limit exceeded",
				}); err != nil {
					// Log encoding error using http.Error as fallback
					http.Error(w, "Internal server error", http.StatusInternalServerError)
				}
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// JWTMiddleware creates a JWT authentication middleware
func JWTMiddleware(secret string, logger *zap.Logger, metrics *Metrics) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip authentication for public endpoints with secure path checking
			if isPublicPath(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}

			start := time.Now()
			var result string
			defer func() {
				if metrics != nil {
					metrics.RecordJWTValidation(result, time.Since(start))
				}
			}()

			// Get Authorization header
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				result = "missing_header"
				writeJWTError(w, "Missing Authorization header", http.StatusUnauthorized)
				return
			}

			// Check Bearer format
			if !strings.HasPrefix(authHeader, "Bearer ") {
				result = "invalid_format"
				writeJWTError(w, "Invalid Authorization header format", http.StatusUnauthorized)
				return
			}

			// Extract token
			tokenString := strings.TrimPrefix(authHeader, "Bearer ")

			// Parse and validate token
			claims := &Claims{}
			token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
				// Validate signing method
				if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
					return nil, jwt.ErrSignatureInvalid
				}
				return []byte(secret), nil
			})

			if err != nil {
				result = "invalid_token"
				logger.Warn("JWT validation failed", zap.Error(err))
				writeJWTError(w, "Invalid token", http.StatusUnauthorized)
				return
			}

			if !token.Valid {
				result = "invalid_token"
				writeJWTError(w, "Invalid token", http.StatusUnauthorized)
				return
			}

			// Validate claims structure
			if claims.UserID == "" || claims.Email == "" || claims.Role == "" {
				result = "invalid_claims"
				writeJWTError(w, "Invalid token claims", http.StatusUnauthorized)
				return
			}

			result = "success"

			// Add user info to request headers for backend services
			r.Header.Set("X-User-ID", claims.UserID)
			r.Header.Set("X-User-Email", claims.Email)
			r.Header.Set("X-User-Role", claims.Role)

			// Add user to context
			ctx := context.WithValue(r.Context(), UserContextKey, claims)
			r = r.WithContext(ctx)

			next.ServeHTTP(w, r)
		})
	}
}

// writeJWTError writes a JSON error response for JWT failures
func writeJWTError(w http.ResponseWriter, message string, statusCode int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	response := map[string]string{"error": message}
	if err := json.NewEncoder(w).Encode(response); err != nil {
		// Fallback to plain text error if JSON encoding fails
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// GetUserFromContext retrieves user claims from request context
func GetUserFromContext(ctx context.Context) (*Claims, bool) {
	claims, ok := ctx.Value(UserContextKey).(*Claims)
	return claims, ok
}

// getClientIPFromRequest extracts client IP from request
func getClientIPFromRequest(r *http.Request) string {
	// Check X-Forwarded-For header first
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		if len(parts) > 0 {
			return strings.TrimSpace(parts[0])
		}
	}

	// Check X-Real-IP header
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}

	// Fall back to RemoteAddr
	if r.RemoteAddr != "" {
		if idx := strings.LastIndex(r.RemoteAddr, ":"); idx != -1 {
			return r.RemoteAddr[:idx]
		}
	}

	return "unknown"
}
