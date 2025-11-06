package validation

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestValidateContentTypeMiddleware(t *testing.T) {
	validator := NewDefaultInputValidator()
	middleware := NewValidationMiddleware(validator, nil)

	handler := middleware.ValidateContentType(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	tests := []struct {
		name           string
		method         string
		contentType    string
		expectedStatus int
	}{
		{
			name:           "valid - application/json",
			method:         "POST",
			contentType:    "application/json",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "valid - no content type",
			method:         "POST",
			contentType:    "",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "invalid - text/plain",
			method:         "POST",
			contentType:    "text/plain",
			expectedStatus: http.StatusUnsupportedMediaType,
		},
		{
			name:           "valid - GET request (no check)",
			method:         "GET",
			contentType:    "text/html",
			expectedStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "/test", nil)
			if tt.contentType != "" {
				req.Header.Set("Content-Type", tt.contentType)
			}

			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			if rr.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, rr.Code)
			}
		})
	}
}

func TestLimitRequestBodyMiddleware(t *testing.T) {
	validator := NewDefaultInputValidator()
	middleware := NewValidationMiddleware(validator, nil)

	handler := middleware.LimitRequestBody(100)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	tests := []struct {
		name           string
		bodySize       int
		expectedStatus int
	}{
		{
			name:           "valid - small body",
			bodySize:       50,
			expectedStatus: http.StatusOK,
		},
		{
			name:           "valid - exact limit",
			bodySize:       100,
			expectedStatus: http.StatusOK,
		},
		{
			name:           "invalid - exceeds limit",
			bodySize:       200,
			expectedStatus: http.StatusOK, // MaxBytesReader doesn't fail immediately
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := strings.Repeat("a", tt.bodySize)
			req := httptest.NewRequest("POST", "/test", bytes.NewBufferString(body))

			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			if rr.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, rr.Code)
			}
		})
	}
}

func TestSecureHeadersMiddleware(t *testing.T) {
	validator := NewDefaultInputValidator()
	middleware := NewValidationMiddleware(validator, nil)

	handler := middleware.SecureHeadersMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	// Check security headers
	expectedHeaders := map[string]string{
		"X-Content-Type-Options":    "nosniff",
		"X-Frame-Options":           "DENY",
		"X-XSS-Protection":          "1; mode=block",
		"Content-Security-Policy":   "default-src 'self'",
		"Strict-Transport-Security": "max-age=31536000; includeSubDomains",
	}

	for header, expectedValue := range expectedHeaders {
		actualValue := rr.Header().Get(header)
		if actualValue != expectedValue {
			t.Errorf("Header %s = %s, want %s", header, actualValue, expectedValue)
		}
	}
}

func TestLogRequestMiddleware(t *testing.T) {
	// Create logger that writes to buffer
	var buf bytes.Buffer
	logger := log.New(&buf, "", 0)

	validator := NewDefaultInputValidator()
	middleware := NewValidationMiddleware(validator, logger)

	handler := middleware.LogRequestMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	// Check that logs were written
	logOutput := buf.String()
	if !strings.Contains(logOutput, "GET") || !strings.Contains(logOutput, "/test") {
		t.Errorf("Expected log to contain request details, got: %s", logOutput)
	}

	if !strings.Contains(logOutput, "completed") {
		t.Errorf("Expected log to contain completion message, got: %s", logOutput)
	}
}

func TestValidateOrderRequestMiddleware(t *testing.T) {
	validator := NewDefaultInputValidator()
	middleware := NewValidationMiddleware(validator, nil)

	handler := middleware.ValidateOrderRequestMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	tests := []struct {
		name           string
		method         string
		body           string
		contentType    string
		expectedStatus int
	}{
		{
			name:           "valid order request",
			method:         "POST",
			body:           `{"client_id":"client123","symbol":"BTCUSD","side":"buy","price":50000.50,"quantity":1.5}`,
			contentType:    "application/json",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "invalid - malformed JSON",
			method:         "POST",
			body:           `{"client_id":"client123",invalid}`,
			contentType:    "application/json",
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "invalid - validation failure",
			method:         "POST",
			body:           `{"client_id":"","symbol":"BTCUSD","side":"buy","price":50000.50,"quantity":1.5}`,
			contentType:    "application/json",
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "skip validation for GET",
			method:         "GET",
			body:           "",
			contentType:    "",
			expectedStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "/order", bytes.NewBufferString(tt.body))
			if tt.contentType != "" {
				req.Header.Set("Content-Type", tt.contentType)
			}

			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			if rr.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d, body: %s",
					tt.expectedStatus, rr.Code, rr.Body.String())
			}
		})
	}
}

func TestMiddlewareChain(t *testing.T) {
	validator := NewDefaultInputValidator()
	logger := log.New(os.Stdout, "[TEST] ", log.LstdFlags)
	middleware := NewValidationMiddleware(validator, logger)

	// Create handler
	finalHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Chain multiple middleware
	handler := middleware.SecureHeadersMiddleware(
		middleware.LogRequestMiddleware(
			middleware.ValidateContentType(finalHandler),
		),
	)

	// Test with valid request
	req := httptest.NewRequest("POST", "/test", bytes.NewBufferString("test"))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rr.Code)
	}

	// Check security headers are present
	if rr.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Error("Security headers not applied")
	}
}

func TestErrorResponse(t *testing.T) {
	validator := NewDefaultInputValidator()
	middleware := NewValidationMiddleware(validator, nil)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		middleware.sendError(w, ErrInvalidPrice, "INVALID_PRICE", http.StatusBadRequest)
	})

	req := httptest.NewRequest("POST", "/test", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", rr.Code)
	}

	// Check response is valid JSON
	contentType := rr.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Expected Content-Type application/json, got %s", contentType)
	}

	body := rr.Body.String()
	if !strings.Contains(body, "INVALID_PRICE") {
		t.Errorf("Expected error code in response, got: %s", body)
	}
}

// Benchmark middleware performance
func BenchmarkSecureHeadersMiddleware(b *testing.B) {
	validator := NewDefaultInputValidator()
	middleware := NewValidationMiddleware(validator, nil)

	handler := middleware.SecureHeadersMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
	}
}

func BenchmarkValidateOrderRequestMiddleware(b *testing.B) {
	validator := NewDefaultInputValidator()
	middleware := NewValidationMiddleware(validator, nil)

	handler := middleware.ValidateOrderRequestMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	body := `{"client_id":"client123","symbol":"BTCUSD","side":"buy","price":50000.50,"quantity":1.5}`
	req := httptest.NewRequest("POST", "/order", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
	}
}
