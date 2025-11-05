package ratelimit

import (
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/yourusername/trading-engine/logging"
)

// Middleware creates HTTP middleware for rate limiting
type Middleware struct {
	limiter       *TokenBucketLimiter
	keyExtractor  KeyExtractor
	errorHandler  ErrorHandler
	skipPaths     map[string]bool
}

// KeyExtractor extracts the rate limit key from the request
type KeyExtractor func(r *http.Request) string

// ErrorHandler handles rate limit exceeded errors
type ErrorHandler func(w http.ResponseWriter, r *http.Request, result *RateLimitResult)

// MiddlewareConfig configures the rate limiting middleware
type MiddlewareConfig struct {
	Limiter      *TokenBucketLimiter
	KeyExtractor KeyExtractor
	ErrorHandler ErrorHandler
	SkipPaths    []string // Paths to skip rate limiting (e.g., /healthz, /metrics)
}

// NewMiddleware creates a new rate limiting middleware
func NewMiddleware(config MiddlewareConfig) *Middleware {
	// Use default key extractor if not provided
	if config.KeyExtractor == nil {
		config.KeyExtractor = ClientIDAndIPKeyExtractor
	}
	
	// Use default error handler if not provided
	if config.ErrorHandler == nil {
		config.ErrorHandler = DefaultErrorHandler
	}
	
	// Build skip paths map
	skipPaths := make(map[string]bool)
	for _, path := range config.SkipPaths {
		skipPaths[path] = true
	}
	
	return &Middleware{
		limiter:      config.Limiter,
		keyExtractor: config.KeyExtractor,
		errorHandler: config.ErrorHandler,
		skipPaths:    skipPaths,
	}
}

// Handler returns an HTTP middleware handler
func (m *Middleware) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip rate limiting for specified paths
		if m.skipPaths[r.URL.Path] {
			next.ServeHTTP(w, r)
			return
		}
		
		// Extract client key
		clientKey := m.keyExtractor(r)
		
		// Check rate limit
		result, err := m.limiter.Allow(r.Context(), clientKey)
		if err != nil {
			// Log error but allow request (fail open)
			logging.GetLogger().WithField("error", err.Error()).Warn("Rate limiter error, allowing request")
			next.ServeHTTP(w, r)
			return
		}
		
		// Add rate limit headers
		w.Header().Set("X-RateLimit-Limit", fmt.Sprintf("%d", m.limiter.maxTokens))
		w.Header().Set("X-RateLimit-Remaining", fmt.Sprintf("%d", result.Remaining))
		
		if !result.Allowed {
			// Rate limit exceeded
			m.errorHandler(w, r, result)
			return
		}
		
		// Allow request
		next.ServeHTTP(w, r)
	})
}

// Key Extractors

// ClientIDAndIPKeyExtractor uses client_id from request body (if available) or IP address
func ClientIDAndIPKeyExtractor(r *http.Request) string {
	// Try to get client_id from query params (for GET requests)
	if clientID := r.URL.Query().Get("client_id"); clientID != "" {
		return "client:" + clientID
	}
	
	// Try to get client_id from header
	if clientID := r.Header.Get("X-Client-ID"); clientID != "" {
		return "client:" + clientID
	}
	
	// Fall back to IP address
	ip := GetClientIP(r)
	return "ip:" + ip
}

// IPKeyExtractor uses only IP address for rate limiting
func IPKeyExtractor(r *http.Request) string {
	ip := GetClientIP(r)
	return "ip:" + ip
}

// ClientIDKeyExtractor uses client_id from header (strict - returns error if missing)
func ClientIDKeyExtractor(r *http.Request) string {
	clientID := r.Header.Get("X-Client-ID")
	if clientID == "" {
		// Fallback to IP if client ID not provided
		return "ip:" + GetClientIP(r)
	}
	return "client:" + clientID
}

// GetClientIP extracts the client's IP address from the request
func GetClientIP(r *http.Request) string {
	// Check X-Forwarded-For header (common in load balancers/proxies)
	forwarded := r.Header.Get("X-Forwarded-For")
	if forwarded != "" {
		// X-Forwarded-For can contain multiple IPs, take the first one
		ips := strings.Split(forwarded, ",")
		if len(ips) > 0 {
			return strings.TrimSpace(ips[0])
		}
	}
	
	// Check X-Real-IP header
	realIP := r.Header.Get("X-Real-IP")
	if realIP != "" {
		return realIP
	}
	
	// Fall back to RemoteAddr
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	
	return ip
}

// Error Handlers

// DefaultErrorHandler returns HTTP 429 with Retry-After header
func DefaultErrorHandler(w http.ResponseWriter, r *http.Request, result *RateLimitResult) {
	retryAfterSeconds := int(result.RetryAfter.Seconds()) + 1
	
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Retry-After", fmt.Sprintf("%d", retryAfterSeconds))
	w.Header().Set("X-RateLimit-Reset", fmt.Sprintf("%d", result.ResetAt.Unix()))
	w.WriteHeader(http.StatusTooManyRequests)
	
	// Log rate limit exceeded
	correlationID := r.Context().Value("correlation_id")
	if correlationID != nil {
		logging.GetLogger().WithField("correlation_id", correlationID).WithField("retry_after", retryAfterSeconds).Warn("Rate limit exceeded")
	} else {
		logging.GetLogger().WithField("retry_after", retryAfterSeconds).Warn("Rate limit exceeded")
	}
	
	// Return JSON error response
	fmt.Fprintf(w, `{
		"success": false,
		"error": "Rate limit exceeded",
		"message": "Too many requests. Please slow down.",
		"retry_after_seconds": %d,
		"reset_at": "%s"
	}`, retryAfterSeconds, result.ResetAt.Format(time.RFC3339))
}

// CustomErrorHandler allows for custom error responses
func CustomErrorHandler(message string, statusCode int) ErrorHandler {
	return func(w http.ResponseWriter, r *http.Request, result *RateLimitResult) {
		retryAfterSeconds := int(result.RetryAfter.Seconds()) + 1
		
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Retry-After", fmt.Sprintf("%d", retryAfterSeconds))
		w.Header().Set("X-RateLimit-Reset", fmt.Sprintf("%d", result.ResetAt.Unix()))
		w.WriteHeader(statusCode)
		
		fmt.Fprintf(w, `{
			"success": false,
			"error": "%s",
			"retry_after_seconds": %d,
			"reset_at": "%s"
		}`, message, retryAfterSeconds, result.ResetAt.Format(time.RFC3339))
	}
}
