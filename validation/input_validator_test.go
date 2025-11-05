package validation

import (
	"bytes"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestValidatePrice(t *testing.T) {
	validator := NewDefaultInputValidator()

	tests := []struct {
		name      string
		price     float64
		wantError bool
		errorType error
	}{
		{
			name:      "valid price with 2 decimals",
			price:     100.50,
			wantError: false,
		},
		{
			name:      "valid price with 8 decimals",
			price:     100.12345678,
			wantError: false,
		},
		{
			name:      "valid minimum price",
			price:     0.00000001,
			wantError: false,
		},
		{
			name:      "invalid - exceeds 8 decimal precision",
			price:     100.123456789,
			wantError: true,
			errorType: ErrPricePrecisionExceeded,
		},
		{
			name:      "invalid - price is zero",
			price:     0.0,
			wantError: true,
			errorType: ErrPriceOutOfRange,
		},
		{
			name:      "invalid - negative price",
			price:     -50.0,
			wantError: true,
			errorType: ErrPriceOutOfRange,
		},
		{
			name:      "invalid - price too high",
			price:     10000000000.0, // 10 billion
			wantError: true,
			errorType: ErrPriceOutOfRange,
		},
		{
			name:      "invalid - NaN",
			price:     math.NaN(),
			wantError: true,
			errorType: ErrInvalidPrice,
		},
		{
			name:      "invalid - positive infinity",
			price:     math.Inf(1),
			wantError: true,
			errorType: ErrInvalidPrice,
		},
		{
			name:      "invalid - negative infinity",
			price:     math.Inf(-1),
			wantError: true,
			errorType: ErrInvalidPrice,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidatePrice(tt.price)
			if (err != nil) != tt.wantError {
				t.Errorf("ValidatePrice() error = %v, wantError %v", err, tt.wantError)
			}
			if tt.wantError && tt.errorType != nil && !strings.Contains(err.Error(), tt.errorType.Error()) {
				t.Errorf("ValidatePrice() error type = %v, want %v", err, tt.errorType)
			}
		})
	}
}

func TestValidateQuantity(t *testing.T) {
	validator := NewDefaultInputValidator()

	tests := []struct {
		name      string
		quantity  float64
		wantError bool
		errorType error
	}{
		{
			name:      "valid quantity",
			quantity:  100.5,
			wantError: false,
		},
		{
			name:      "valid small quantity",
			quantity:  0.00000001,
			wantError: false,
		},
		{
			name:      "valid large quantity",
			quantity:  1000000.0,
			wantError: false,
		},
		{
			name:      "invalid - zero quantity",
			quantity:  0.0,
			wantError: true,
			errorType: ErrQuantityOutOfRange,
		},
		{
			name:      "invalid - negative quantity",
			quantity:  -10.0,
			wantError: true,
			errorType: ErrQuantityOutOfRange,
		},
		{
			name:      "invalid - exceeds maximum",
			quantity:  2000000000.0, // 2 billion
			wantError: true,
			errorType: ErrQuantityOutOfRange,
		},
		{
			name:      "invalid - NaN",
			quantity:  math.NaN(),
			wantError: true,
			errorType: ErrInvalidQuantity,
		},
		{
			name:      "invalid - infinity",
			quantity:  math.Inf(1),
			wantError: true,
			errorType: ErrInvalidQuantity,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateQuantity(tt.quantity)
			if (err != nil) != tt.wantError {
				t.Errorf("ValidateQuantity() error = %v, wantError %v", err, tt.wantError)
			}
			if tt.wantError && tt.errorType != nil && !strings.Contains(err.Error(), tt.errorType.Error()) {
				t.Errorf("ValidateQuantity() error type = %v, want %v", err, tt.errorType)
			}
		})
	}
}

func TestValidateClientID(t *testing.T) {
	validator := NewDefaultInputValidator()

	tests := []struct {
		name      string
		clientID  string
		wantError bool
	}{
		{
			name:      "valid client_id",
			clientID:  "client123",
			wantError: false,
		},
		{
			name:      "valid with underscore",
			clientID:  "client_123",
			wantError: false,
		},
		{
			name:      "valid with hyphen",
			clientID:  "client-123",
			wantError: false,
		},
		{
			name:      "invalid - empty",
			clientID:  "",
			wantError: true,
		},
		{
			name:      "invalid - too long",
			clientID:  strings.Repeat("a", 65),
			wantError: true,
		},
		{
			name:      "invalid - special characters",
			clientID:  "client@123",
			wantError: true,
		},
		{
			name:      "invalid - spaces",
			clientID:  "client 123",
			wantError: true,
		},
		{
			name:      "invalid - SQL injection attempt",
			clientID:  "client'; DROP TABLE orders;--",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateClientID(tt.clientID)
			if (err != nil) != tt.wantError {
				t.Errorf("ValidateClientID() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}

func TestValidateSymbol(t *testing.T) {
	validator := NewDefaultInputValidator()

	tests := []struct {
		name      string
		symbol    string
		wantError bool
	}{
		{
			name:      "valid symbol",
			symbol:    "BTCUSD",
			wantError: false,
		},
		{
			name:      "valid with numbers",
			symbol:    "BTC2USD",
			wantError: false,
		},
		{
			name:      "invalid - empty",
			symbol:    "",
			wantError: true,
		},
		{
			name:      "invalid - lowercase",
			symbol:    "btcusd",
			wantError: true,
		},
		{
			name:      "invalid - special characters",
			symbol:    "BTC-USD",
			wantError: true,
		},
		{
			name:      "invalid - too long",
			symbol:    strings.Repeat("A", 21),
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateSymbol(tt.symbol)
			if (err != nil) != tt.wantError {
				t.Errorf("ValidateSymbol() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}

func TestValidateSide(t *testing.T) {
	validator := NewDefaultInputValidator()

	tests := []struct {
		name      string
		side      string
		wantError bool
	}{
		{
			name:      "valid - buy",
			side:      "buy",
			wantError: false,
		},
		{
			name:      "valid - sell",
			side:      "sell",
			wantError: false,
		},
		{
			name:      "valid - BUY uppercase",
			side:      "BUY",
			wantError: false,
		},
		{
			name:      "valid - SELL uppercase",
			side:      "SELL",
			wantError: false,
		},
		{
			name:      "invalid - empty",
			side:      "",
			wantError: true,
		},
		{
			name:      "invalid - wrong value",
			side:      "long",
			wantError: true,
		},
		{
			name:      "invalid - typo",
			side:      "buuy",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateSide(tt.side)
			if (err != nil) != tt.wantError {
				t.Errorf("ValidateSide() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}

func TestValidateRequestBody(t *testing.T) {
	validator := NewDefaultInputValidator()

	tests := []struct {
		name        string
		body        string
		contentType string
		maxSize     int64
		wantError   bool
		errorType   error
	}{
		{
			name:        "valid JSON body",
			body:        `{"test": "data"}`,
			contentType: "application/json",
			maxSize:     1024,
			wantError:   false,
		},
		{
			name:        "valid - no content type",
			body:        `{"test": "data"}`,
			contentType: "",
			maxSize:     1024,
			wantError:   false,
		},
		{
			name:        "invalid - wrong content type",
			body:        `{"test": "data"}`,
			contentType: "text/plain",
			maxSize:     1024,
			wantError:   true,
			errorType:   ErrInvalidContentType,
		},
		{
			name:        "invalid - body too large",
			body:        strings.Repeat("a", 2000),
			contentType: "application/json",
			maxSize:     1024,
			wantError:   true,
			errorType:   ErrRequestBodyTooLarge,
		},
		{
			name:        "valid - exact max size",
			body:        strings.Repeat("a", 1024),
			contentType: "application/json",
			maxSize:     1024,
			wantError:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/test", bytes.NewBufferString(tt.body))
			if tt.contentType != "" {
				req.Header.Set("Content-Type", tt.contentType)
			}

			body, err := validator.ValidateRequestBody(req, tt.maxSize)
			if (err != nil) != tt.wantError {
				t.Errorf("ValidateRequestBody() error = %v, wantError %v", err, tt.wantError)
			}

			if !tt.wantError && string(body) != tt.body {
				t.Errorf("ValidateRequestBody() body = %v, want %v", string(body), tt.body)
			}

			if tt.wantError && tt.errorType != nil && !strings.Contains(err.Error(), tt.errorType.Error()) {
				t.Errorf("ValidateRequestBody() error type = %v, want %v", err, tt.errorType)
			}
		})
	}
}

func TestValidateAndDecodeJSON(t *testing.T) {
	validator := NewDefaultInputValidator()

	type TestStruct struct {
		Name  string  `json:"name"`
		Value float64 `json:"value"`
	}

	tests := []struct {
		name      string
		body      string
		wantError bool
	}{
		{
			name:      "valid JSON",
			body:      `{"name": "test", "value": 123.45}`,
			wantError: false,
		},
		{
			name:      "invalid - malformed JSON",
			body:      `{"name": "test", "value": 123.45`,
			wantError: true,
		},
		{
			name:      "invalid - unknown field",
			body:      `{"name": "test", "value": 123.45, "extra": "field"}`,
			wantError: true,
		},
		{
			name:      "invalid - empty body",
			body:      ``,
			wantError: true,
		},
		{
			name:      "invalid - trailing data",
			body:      `{"name": "test", "value": 123.45}extra`,
			wantError: true,
		},
		{
			name:      "invalid - wrong type",
			body:      `{"name": "test", "value": "not a number"}`,
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var result TestStruct
			err := validator.ValidateAndDecodeJSON([]byte(tt.body), &result)
			if (err != nil) != tt.wantError {
				t.Errorf("ValidateAndDecodeJSON() error = %v, wantError %v", err, tt.wantError)
			}

			if !tt.wantError {
				if result.Name != "test" || result.Value != 123.45 {
					t.Errorf("ValidateAndDecodeJSON() decoded incorrectly: %+v", result)
				}
			}
		})
	}
}

func TestValidateOrderRequest(t *testing.T) {
	validator := NewDefaultInputValidator()

	tests := []struct {
		name      string
		request   *OrderRequest
		wantError bool
	}{
		{
			name: "valid order request",
			request: &OrderRequest{
				ClientID: "client123",
				Symbol:   "BTCUSD",
				Side:     "buy",
				Price:    50000.50,
				Quantity: 1.5,
			},
			wantError: false,
		},
		{
			name: "valid with max precision",
			request: &OrderRequest{
				ClientID: "client123",
				Symbol:   "BTCUSD",
				Side:     "sell",
				Price:    50000.12345678,
				Quantity: 0.00000001,
			},
			wantError: false,
		},
		{
			name: "invalid - empty client_id",
			request: &OrderRequest{
				ClientID: "",
				Symbol:   "BTCUSD",
				Side:     "buy",
				Price:    50000.50,
				Quantity: 1.5,
			},
			wantError: true,
		},
		{
			name: "invalid - exceeds price precision",
			request: &OrderRequest{
				ClientID: "client123",
				Symbol:   "BTCUSD",
				Side:     "buy",
				Price:    50000.123456789,
				Quantity: 1.5,
			},
			wantError: true,
		},
		{
			name: "invalid - negative price",
			request: &OrderRequest{
				ClientID: "client123",
				Symbol:   "BTCUSD",
				Side:     "buy",
				Price:    -50000.50,
				Quantity: 1.5,
			},
			wantError: true,
		},
		{
			name: "invalid - quantity too large",
			request: &OrderRequest{
				ClientID: "client123",
				Symbol:   "BTCUSD",
				Side:     "buy",
				Price:    50000.50,
				Quantity: 2000000000.0,
			},
			wantError: true,
		},
		{
			name: "invalid - multiple errors",
			request: &OrderRequest{
				ClientID: "",
				Symbol:   "btcusd",
				Side:     "invalid",
				Price:    -50000.50,
				Quantity: 0.0,
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateOrderRequest(tt.request)
			if (err != nil) != tt.wantError {
				t.Errorf("ValidateOrderRequest() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}

func TestValidateCancelOrderRequest(t *testing.T) {
	validator := NewDefaultInputValidator()

	tests := []struct {
		name      string
		request   *CancelOrderRequest
		wantError bool
	}{
		{
			name: "valid cancel request",
			request: &CancelOrderRequest{
				ClientID: "client123",
				OrderID:  "order-abc-123",
				Symbol:   "BTCUSD",
			},
			wantError: false,
		},
		{
			name: "invalid - empty order_id",
			request: &CancelOrderRequest{
				ClientID: "client123",
				OrderID:  "",
				Symbol:   "BTCUSD",
			},
			wantError: true,
		},
		{
			name: "invalid - order_id too long",
			request: &CancelOrderRequest{
				ClientID: "client123",
				OrderID:  strings.Repeat("a", 65),
				Symbol:   "BTCUSD",
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateCancelOrderRequest(tt.request)
			if (err != nil) != tt.wantError {
				t.Errorf("ValidateCancelOrderRequest() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}

func TestCheckPrecision(t *testing.T) {
	validator := NewDefaultInputValidator()

	tests := []struct {
		name        string
		value       float64
		maxDecimals int
		wantError   bool
	}{
		{
			name:        "valid - 2 decimals",
			value:       100.50,
			maxDecimals: 8,
			wantError:   false,
		},
		{
			name:        "valid - 8 decimals",
			value:       100.12345678,
			maxDecimals: 8,
			wantError:   false,
		},
		{
			name:        "valid - no decimals",
			value:       100.0,
			maxDecimals: 8,
			wantError:   false,
		},
		{
			name:        "invalid - 9 decimals",
			value:       100.123456789,
			maxDecimals: 8,
			wantError:   true,
		},
		{
			name:        "valid - trailing zeros ignored",
			value:       100.12000000,
			maxDecimals: 2,
			wantError:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.checkPrecision(tt.value, tt.maxDecimals)
			if (err != nil) != tt.wantError {
				t.Errorf("checkPrecision() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}

func TestSanitizeString(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		maxLen int
		want   string
	}{
		{
			name:   "normal string",
			input:  "normal text",
			maxLen: 100,
			want:   "normal text",
		},
		{
			name:   "truncate long string",
			input:  "this is a very long string",
			maxLen: 10,
			want:   "this is a ",
		},
		{
			name:   "remove control characters",
			input:  "text\x00with\x01control\x02chars",
			maxLen: 100,
			want:   "textwithcontrolchars",
		},
		{
			name:   "keep newline and tab",
			input:  "text\nwith\ttab",
			maxLen: 100,
			want:   "text\nwith\ttab",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SanitizeString(tt.input, tt.maxLen)
			if got != tt.want {
				t.Errorf("SanitizeString() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestValidateNumericRange(t *testing.T) {
	tests := []struct {
		name      string
		value     float64
		min       float64
		max       float64
		fieldName string
		wantError bool
	}{
		{
			name:      "valid - in range",
			value:     50.0,
			min:       0.0,
			max:       100.0,
			fieldName: "test",
			wantError: false,
		},
		{
			name:      "valid - at minimum",
			value:     0.0,
			min:       0.0,
			max:       100.0,
			fieldName: "test",
			wantError: false,
		},
		{
			name:      "valid - at maximum",
			value:     100.0,
			min:       0.0,
			max:       100.0,
			fieldName: "test",
			wantError: false,
		},
		{
			name:      "invalid - below minimum",
			value:     -1.0,
			min:       0.0,
			max:       100.0,
			fieldName: "test",
			wantError: true,
		},
		{
			name:      "invalid - above maximum",
			value:     101.0,
			min:       0.0,
			max:       100.0,
			fieldName: "test",
			wantError: true,
		},
		{
			name:      "invalid - NaN",
			value:     math.NaN(),
			min:       0.0,
			max:       100.0,
			fieldName: "test",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateNumericRange(tt.value, tt.min, tt.max, tt.fieldName)
			if (err != nil) != tt.wantError {
				t.Errorf("ValidateNumericRange() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}

func TestCustomValidationConfig(t *testing.T) {
	// Test with custom configuration
	customConfig := &ValidationConfig{
		MaxPricePrecision:  4, // Only 4 decimals allowed
		MinPrice:           1.0,
		MaxPrice:           100000.0,
		MinQuantity:        0.1,
		MaxQuantity:        10000.0,
		MaxClientIDLength:  32,
		MaxSymbolLength:    10,
		MaxRequestBodySize: 512,
	}

	validator := NewInputValidator(customConfig)

	// Test price precision with custom config
	err := validator.ValidatePrice(100.12345)
	if err == nil {
		t.Error("Expected error for 5 decimal places with max 4, got nil")
	}

	err = validator.ValidatePrice(100.1234)
	if err != nil {
		t.Errorf("Expected no error for 4 decimal places, got %v", err)
	}

	// Test quantity range with custom config
	err = validator.ValidateQuantity(0.05)
	if err == nil {
		t.Error("Expected error for quantity below min 0.1, got nil")
	}

	err = validator.ValidateQuantity(0.1)
	if err != nil {
		t.Errorf("Expected no error for quantity at min, got %v", err)
	}
}

// Benchmark tests
func BenchmarkValidatePrice(b *testing.B) {
	validator := NewDefaultInputValidator()
	price := 50000.12345678

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = validator.ValidatePrice(price)
	}
}

func BenchmarkValidateOrderRequest(b *testing.B) {
	validator := NewDefaultInputValidator()
	req := &OrderRequest{
		ClientID: "client123",
		Symbol:   "BTCUSD",
		Side:     "buy",
		Price:    50000.50,
		Quantity: 1.5,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = validator.ValidateOrderRequest(req)
	}
}

func BenchmarkValidateAndDecodeJSON(b *testing.B) {
	validator := NewDefaultInputValidator()
	jsonBody := []byte(`{"client_id":"client123","symbol":"BTCUSD","side":"buy","price":50000.50,"quantity":1.5}`)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var req OrderRequest
		_ = validator.ValidateAndDecodeJSON(jsonBody, &req)
	}
}

// Integration test with HTTP handler
func TestHTTPHandlerIntegration(t *testing.T) {
	validator := NewDefaultInputValidator()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Validate and read body
		body, err := validator.ValidateRequestBody(r, MaxRequestBodySize)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		// Decode JSON
		var req OrderRequest
		if err := validator.ValidateAndDecodeJSON(body, &req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		// Validate order
		if err := validator.ValidateOrderRequest(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "valid"})
	})

	tests := []struct {
		name           string
		method         string
		body           string
		contentType    string
		expectedStatus int
	}{
		{
			name:           "valid request",
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
			name:           "invalid - exceeds precision",
			method:         "POST",
			body:           `{"client_id":"client123","symbol":"BTCUSD","side":"buy","price":50000.123456789,"quantity":1.5}`,
			contentType:    "application/json",
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "invalid - body too large",
			method:         "POST",
			body:           strings.Repeat("a", 2*1024*1024),
			contentType:    "application/json",
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "/order", bytes.NewBufferString(tt.body))
			req.Header.Set("Content-Type", tt.contentType)
			
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			if rr.Code != tt.expectedStatus {
				t.Errorf("handler returned wrong status code: got %v want %v, body: %s", 
					rr.Code, tt.expectedStatus, rr.Body.String())
			}
		})
	}
}
