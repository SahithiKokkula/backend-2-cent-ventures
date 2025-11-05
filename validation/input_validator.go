package validation

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	MaxPricePrecision = 8
	MinPrice          = 0.00000001
	MaxPrice          = 1000000000.0

	MinQuantity = 0.00000001
	MaxQuantity = 1000000000.0

	MaxClientIDLength = 64
	MaxSymbolLength   = 20
	MaxOrderIDLength  = 64
	MaxSideLength     = 10

	MaxRequestBodySize = 1024 * 1024
	MaxJSONDepth       = 10

	ClientIDPattern = `^[a-zA-Z0-9_-]+$`
	SymbolPattern   = `^[A-Z0-9]+$`
	OrderIDPattern  = `^[a-zA-Z0-9_-]+$`
)

var (
	clientIDRegex = regexp.MustCompile(ClientIDPattern)
	symbolRegex   = regexp.MustCompile(SymbolPattern)
	orderIDRegex  = regexp.MustCompile(OrderIDPattern)

	ErrInvalidPrice           = errors.New("invalid price")
	ErrPricePrecisionExceeded = errors.New("price precision exceeds 8 decimals")
	ErrPriceOutOfRange        = errors.New("price out of valid range")
	ErrInvalidQuantity        = errors.New("invalid quantity")
	ErrQuantityOutOfRange     = errors.New("quantity out of valid range")
	ErrInvalidClientID        = errors.New("invalid client_id format or length")
	ErrInvalidSymbol          = errors.New("invalid symbol format or length")
	ErrInvalidOrderID         = errors.New("invalid order_id format or length")
	ErrInvalidSide            = errors.New("invalid order side")
	ErrRequestBodyTooLarge    = errors.New("request body too large")
	ErrMalformedJSON          = errors.New("malformed JSON")
	ErrInvalidContentType     = errors.New("invalid content type, expected application/json")
)

type ValidationConfig struct {
	MaxPricePrecision  int
	MinPrice           float64
	MaxPrice           float64
	MinQuantity        float64
	MaxQuantity        float64
	MaxClientIDLength  int
	MaxSymbolLength    int
	MaxRequestBodySize int64
}

func DefaultValidationConfig() *ValidationConfig {
	return &ValidationConfig{
		MaxPricePrecision:  MaxPricePrecision,
		MinPrice:           MinPrice,
		MaxPrice:           MaxPrice,
		MinQuantity:        MinQuantity,
		MaxQuantity:        MaxQuantity,
		MaxClientIDLength:  MaxClientIDLength,
		MaxSymbolLength:    MaxSymbolLength,
		MaxRequestBodySize: MaxRequestBodySize,
	}
}

type InputValidator struct {
	config *ValidationConfig
}

func NewInputValidator(config *ValidationConfig) *InputValidator {
	if config == nil {
		config = DefaultValidationConfig()
	}
	return &InputValidator{config: config}
}

// NewDefaultInputValidator creates a validator with default configuration
func NewDefaultInputValidator() *InputValidator {
	return NewInputValidator(DefaultValidationConfig())
}

// ValidatePrice validates price with precision and range checks
func (iv *InputValidator) ValidatePrice(price float64) error {
	// Check for NaN, Inf
	if math.IsNaN(price) || math.IsInf(price, 0) {
		return fmt.Errorf("%w: not a valid number", ErrInvalidPrice)
	}

	// Check range
	if price < iv.config.MinPrice {
		return fmt.Errorf("%w: price %.10f is below minimum %.10f",
			ErrPriceOutOfRange, price, iv.config.MinPrice)
	}
	if price > iv.config.MaxPrice {
		return fmt.Errorf("%w: price %.2f exceeds maximum %.2f",
			ErrPriceOutOfRange, price, iv.config.MaxPrice)
	}

	// Check precision (decimal places)
	if err := iv.checkPrecision(price, iv.config.MaxPricePrecision); err != nil {
		return fmt.Errorf("%w: %v", ErrPricePrecisionExceeded, err)
	}

	return nil
}

// ValidateQuantity validates quantity with range checks
func (iv *InputValidator) ValidateQuantity(quantity float64) error {
	// Check for NaN, Inf
	if math.IsNaN(quantity) || math.IsInf(quantity, 0) {
		return fmt.Errorf("%w: not a valid number", ErrInvalidQuantity)
	}

	// Check range
	if quantity < iv.config.MinQuantity {
		return fmt.Errorf("%w: quantity %.10f is below minimum %.10f",
			ErrQuantityOutOfRange, quantity, iv.config.MinQuantity)
	}
	if quantity > iv.config.MaxQuantity {
		return fmt.Errorf("%w: quantity %.2f exceeds maximum %.2f",
			ErrQuantityOutOfRange, quantity, iv.config.MaxQuantity)
	}

	return nil
}

// ValidateClientID validates client ID format and length
func (iv *InputValidator) ValidateClientID(clientID string) error {
	if clientID == "" {
		return fmt.Errorf("%w: client_id cannot be empty", ErrInvalidClientID)
	}

	// Check length
	if len(clientID) > iv.config.MaxClientIDLength {
		return fmt.Errorf("%w: client_id length %d exceeds maximum %d",
			ErrInvalidClientID, len(clientID), iv.config.MaxClientIDLength)
	}

	// Check UTF-8 validity
	if !utf8.ValidString(clientID) {
		return fmt.Errorf("%w: client_id contains invalid UTF-8", ErrInvalidClientID)
	}

	// Check format (alphanumeric, underscore, hyphen only)
	if !clientIDRegex.MatchString(clientID) {
		return fmt.Errorf("%w: client_id must contain only alphanumeric characters, underscores, and hyphens",
			ErrInvalidClientID)
	}

	return nil
}

// ValidateSymbol validates trading symbol format and length
func (iv *InputValidator) ValidateSymbol(symbol string) error {
	if symbol == "" {
		return fmt.Errorf("%w: symbol cannot be empty", ErrInvalidSymbol)
	}

	// Check length
	if len(symbol) > iv.config.MaxSymbolLength {
		return fmt.Errorf("%w: symbol length %d exceeds maximum %d",
			ErrInvalidSymbol, len(symbol), iv.config.MaxSymbolLength)
	}

	// Check format (uppercase alphanumeric only)
	if !symbolRegex.MatchString(symbol) {
		return fmt.Errorf("%w: symbol must contain only uppercase letters and numbers",
			ErrInvalidSymbol)
	}

	return nil
}

// ValidateOrderID validates order ID format and length
func (iv *InputValidator) ValidateOrderID(orderID string) error {
	if orderID == "" {
		return fmt.Errorf("%w: order_id cannot be empty", ErrInvalidOrderID)
	}

	// Check length
	if len(orderID) > MaxOrderIDLength {
		return fmt.Errorf("%w: order_id length %d exceeds maximum %d",
			ErrInvalidOrderID, len(orderID), MaxOrderIDLength)
	}

	// Check UTF-8 validity
	if !utf8.ValidString(orderID) {
		return fmt.Errorf("%w: order_id contains invalid UTF-8", ErrInvalidOrderID)
	}

	// Check format
	if !orderIDRegex.MatchString(orderID) {
		return fmt.Errorf("%w: order_id must contain only alphanumeric characters, underscores, and hyphens",
			ErrInvalidOrderID)
	}

	return nil
}

// ValidateSide validates order side (buy/sell)
func (iv *InputValidator) ValidateSide(side string) error {
	side = strings.ToLower(strings.TrimSpace(side))

	if side != "buy" && side != "sell" {
		return fmt.Errorf("%w: side must be 'buy' or 'sell', got '%s'", ErrInvalidSide, side)
	}

	return nil
}

// ValidateRequestBody validates and reads request body with size limit
func (iv *InputValidator) ValidateRequestBody(r *http.Request, maxSize int64) ([]byte, error) {
	// Check Content-Type
	contentType := r.Header.Get("Content-Type")
	if contentType != "" && !strings.Contains(contentType, "application/json") {
		return nil, ErrInvalidContentType
	}

	// Use configured max size if not specified
	if maxSize <= 0 {
		maxSize = iv.config.MaxRequestBodySize
	}

	// Limit reader to max size + 1 (to detect oversized requests)
	limitedReader := io.LimitReader(r.Body, maxSize+1)

	body, err := io.ReadAll(limitedReader)
	if err != nil {
		return nil, fmt.Errorf("failed to read request body: %w", err)
	}

	// Check if body exceeds limit
	if int64(len(body)) > maxSize {
		return nil, fmt.Errorf("%w: body size %d exceeds maximum %d bytes",
			ErrRequestBodyTooLarge, len(body), maxSize)
	}

	return body, nil
}

// ValidateAndDecodeJSON validates and decodes JSON with security checks
func (iv *InputValidator) ValidateAndDecodeJSON(body []byte, v interface{}) error {
	// Check for empty body
	if len(body) == 0 {
		return fmt.Errorf("%w: empty request body", ErrMalformedJSON)
	}

	// Decode JSON with strict validation
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.DisallowUnknownFields() // Reject unknown fields for security

	if err := decoder.Decode(v); err != nil {
		return fmt.Errorf("%w: %v", ErrMalformedJSON, err)
	}

	// Ensure no trailing data after JSON
	if decoder.More() {
		return fmt.Errorf("%w: trailing data after JSON", ErrMalformedJSON)
	}

	return nil
}

// checkPrecision checks if a number has more than maxDecimals decimal places
func (iv *InputValidator) checkPrecision(value float64, maxDecimals int) error {
	// Multiply by 10^maxDecimals to shift decimal places
	multiplier := math.Pow(10, float64(maxDecimals))
	shifted := value * multiplier

	// Check if there are more decimal places than allowed
	// by comparing the truncated value with the original
	truncated := math.Trunc(shifted)
	difference := math.Abs(shifted - truncated)

	// Allow small floating point errors (epsilon)
	epsilon := 1e-9
	if difference > epsilon {
		return fmt.Errorf("value %.15f has more than %d decimal places", value, maxDecimals)
	}

	return nil
}

// OrderRequest represents a validated order request
type OrderRequest struct {
	ClientID string  `json:"client_id"`
	Symbol   string  `json:"symbol"`
	Side     string  `json:"side"`
	Price    float64 `json:"price"`
	Quantity float64 `json:"quantity"`
}

// ValidateOrderRequest performs comprehensive validation on an order request
func (iv *InputValidator) ValidateOrderRequest(req *OrderRequest) error {
	var errs []error

	// Validate client_id
	if err := iv.ValidateClientID(req.ClientID); err != nil {
		errs = append(errs, err)
	}

	// Validate symbol
	if err := iv.ValidateSymbol(req.Symbol); err != nil {
		errs = append(errs, err)
	}

	// Validate side
	if err := iv.ValidateSide(req.Side); err != nil {
		errs = append(errs, err)
	}

	// Validate price
	if err := iv.ValidatePrice(req.Price); err != nil {
		errs = append(errs, err)
	}

	// Validate quantity
	if err := iv.ValidateQuantity(req.Quantity); err != nil {
		errs = append(errs, err)
	}

	// Combine errors
	if len(errs) > 0 {
		return fmt.Errorf("validation failed: %v", errs)
	}

	return nil
}

// CancelOrderRequest represents a cancel order request
type CancelOrderRequest struct {
	ClientID string `json:"client_id"`
	OrderID  string `json:"order_id"`
	Symbol   string `json:"symbol"`
}

// ValidateCancelOrderRequest validates a cancel order request
func (iv *InputValidator) ValidateCancelOrderRequest(req *CancelOrderRequest) error {
	var errs []error

	// Validate client_id
	if err := iv.ValidateClientID(req.ClientID); err != nil {
		errs = append(errs, err)
	}

	// Validate order_id
	if err := iv.ValidateOrderID(req.OrderID); err != nil {
		errs = append(errs, err)
	}

	// Validate symbol
	if err := iv.ValidateSymbol(req.Symbol); err != nil {
		errs = append(errs, err)
	}

	// Combine errors
	if len(errs) > 0 {
		return fmt.Errorf("validation failed: %v", errs)
	}

	return nil
}

// SanitizeString removes control characters and limits length
func SanitizeString(s string, maxLen int) string {
	// Remove control characters except newline and tab
	var result strings.Builder
	for _, r := range s {
		if r >= 32 || r == '\n' || r == '\t' {
			result.WriteRune(r)
		}
	}

	str := result.String()
	if len(str) > maxLen {
		str = str[:maxLen]
	}

	return str
}

// ValidateNumericRange checks if a numeric value is within acceptable range
func ValidateNumericRange(value, min, max float64, fieldName string) error {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return fmt.Errorf("%s is not a valid number", fieldName)
	}

	if value < min || value > max {
		return fmt.Errorf("%s %.2f is out of valid range [%.2f, %.2f]",
			fieldName, value, min, max)
	}

	return nil
}
