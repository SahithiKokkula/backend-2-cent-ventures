// Package validation provides HTTP middleware for input validation
package validation

import (
	"encoding/json"
	"log"
	"net/http"
	"time"
)

// ValidationMiddleware provides HTTP middleware for request validation
type ValidationMiddleware struct {
	validator *InputValidator
	logger    *log.Logger
}

// NewValidationMiddleware creates a new validation middleware
func NewValidationMiddleware(validator *InputValidator, logger *log.Logger) *ValidationMiddleware {
	if validator == nil {
		validator = NewDefaultInputValidator()
	}
	return &ValidationMiddleware{
		validator: validator,
		logger:    logger,
	}
}

// ErrorResponse represents an API error response
type ErrorResponse struct {
	Error   string    `json:"error"`
	Code    string    `json:"code"`
	Details string    `json:"details,omitempty"`
	Time    time.Time `json:"timestamp"`
}

// ValidateContentType middleware ensures Content-Type is application/json
func (vm *ValidationMiddleware) ValidateContentType(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only check POST, PUT, PATCH requests
		if r.Method == "POST" || r.Method == "PUT" || r.Method == "PATCH" {
			contentType := r.Header.Get("Content-Type")
			if contentType != "" && contentType != "application/json" {
				vm.sendError(w, ErrInvalidContentType, "INVALID_CONTENT_TYPE",
					http.StatusUnsupportedMediaType)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// LimitRequestBody middleware enforces request body size limits
func (vm *ValidationMiddleware) LimitRequestBody(maxSize int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Limit body size
			r.Body = http.MaxBytesReader(w, r.Body, maxSize)
			next.ServeHTTP(w, r)
		})
	}
}

// ValidateOrderRequestMiddleware validates order request bodies
func (vm *ValidationMiddleware) ValidateOrderRequestMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only validate POST requests
		if r.Method != "POST" {
			next.ServeHTTP(w, r)
			return
		}

		// Read and validate body
		body, err := vm.validator.ValidateRequestBody(r, MaxRequestBodySize)
		if err != nil {
			vm.sendError(w, err, "INVALID_REQUEST_BODY", http.StatusBadRequest)
			return
		}

		// Decode JSON
		var req OrderRequest
		if err := vm.validator.ValidateAndDecodeJSON(body, &req); err != nil {
			vm.sendError(w, err, "INVALID_JSON", http.StatusBadRequest)
			return
		}

		// Validate order
		if err := vm.validator.ValidateOrderRequest(&req); err != nil {
			vm.sendError(w, err, "VALIDATION_FAILED", http.StatusBadRequest)
			return
		}

		// Store validated request in context for handler
		// (In production, use context.WithValue)
		next.ServeHTTP(w, r)
	})
}

// SecureHeadersMiddleware adds security headers
func (vm *ValidationMiddleware) SecureHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Security headers
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-XSS-Protection", "1; mode=block")
		w.Header().Set("Content-Security-Policy", "default-src 'self'")
		w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")

		next.ServeHTTP(w, r)
	})
}

// LogRequestMiddleware logs incoming requests
func (vm *ValidationMiddleware) LogRequestMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Log request
		if vm.logger != nil {
			vm.logger.Printf("[%s] %s %s from %s",
				time.Now().Format(time.RFC3339),
				r.Method,
				r.URL.Path,
				r.RemoteAddr,
			)
		}

		// Call next handler
		next.ServeHTTP(w, r)

		// Log duration
		duration := time.Since(start)
		if vm.logger != nil {
			vm.logger.Printf("[%s] %s %s completed in %v",
				time.Now().Format(time.RFC3339),
				r.Method,
				r.URL.Path,
				duration,
			)
		}
	})
}

// sendError sends a standardized error response
func (vm *ValidationMiddleware) sendError(w http.ResponseWriter, err error, code string, statusCode int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	response := ErrorResponse{
		Error: err.Error(),
		Code:  code,
		Time:  time.Now(),
	}

	if vm.logger != nil {
		vm.logger.Printf("Error [%s]: %s", code, err.Error())
	}

	_ = json.NewEncoder(w).Encode(response)
}

// RateLimitConfig holds rate limiting configuration
type RateLimitConfig struct {
	RequestsPerSecond int
	Burst             int
}

// Example usage in main.go:
/*
func main() {
	// Create validator
	validator := validation.NewDefaultInputValidator()
	middleware := validation.NewValidationMiddleware(validator, log.Default())

	// Create router
	mux := http.NewServeMux()

	// Add routes
	mux.HandleFunc("/order", handleOrder)
	mux.HandleFunc("/cancel", handleCancel)

	// Apply middleware chain
	handler := middleware.SecureHeadersMiddleware(
		middleware.LogRequestMiddleware(
			middleware.LimitRequestBody(MaxRequestBodySize)(
				middleware.ValidateContentType(mux),
			),
		),
	)

	// Start server
	log.Println("Server starting on :8080")
	http.ListenAndServe(":8080", handler)
}

func handleOrder(w http.ResponseWriter, r *http.Request) {
	validator := validation.NewDefaultInputValidator()

	// Read and validate body
	body, err := validator.ValidateRequestBody(r, validation.MaxRequestBodySize)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Decode and validate
	var req validation.OrderRequest
	if err := validator.ValidateAndDecodeJSON(body, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := validator.ValidateOrderRequest(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Process order...
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":   "success",
		"order_id": "generated-order-id",
	})
}
*/
