package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/shopspring/decimal"
	"github.com/yourusername/trading-engine/engine"
	"github.com/yourusername/trading-engine/logging"
	"github.com/yourusername/trading-engine/metrics"
	"github.com/yourusername/trading-engine/models"
)

// OrderRequest represents the incoming order request
type OrderRequest struct {
	ClientID   string          `json:"client_id"`
	Instrument string          `json:"instrument"`
	Side       string          `json:"side"` // "buy" or "sell"
	Type       string          `json:"type"` // "limit" or "market"
	Price      decimal.Decimal `json:"price"`
	Quantity   decimal.Decimal `json:"quantity"`
}

// OrderResponse represents the response after submitting an order
type OrderResponse struct {
	Success   bool            `json:"success"`
	OrderID   string          `json:"order_id"`
	Order     *models.Order   `json:"order,omitempty"`
	Trades    []*engine.Trade `json:"trades,omitempty"`
	Message   string          `json:"message,omitempty"`
	Error     string          `json:"error,omitempty"`
	Timestamp int64           `json:"timestamp"`
	Replayed  bool            `json:"replayed,omitempty"` // True if response was from cache
}

// ValidationError represents a validation error
type ValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

func HandleSubmitOrder(me *engine.MatchingEngine, redisClient *redis.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		correlationID := GetCorrelationID(r)

		if r.Method != http.MethodPost {
			respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
			return
		}

		idempotencyKey := r.Header.Get("Idempotency-Key")
		if idempotencyKey != "" && redisClient != nil {
			cachedResponse, err := checkIdempotencyKey(r.Context(), redisClient, idempotencyKey)
			if err == nil && cachedResponse != nil {
				cachedResponse.Replayed = true
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("X-Idempotency-Key", idempotencyKey)
				w.Header().Set("X-Idempotency-Replayed", "true")
				w.WriteHeader(http.StatusAccepted)
				json.NewEncoder(w).Encode(cachedResponse)
				return
			}
		}

		var req OrderRequest
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()

		if err := decoder.Decode(&req); err != nil {
			respondError(w, http.StatusBadRequest, fmt.Sprintf("Invalid JSON: %v", err))
			return
		}
		defer r.Body.Close()

		if validationErrors := validateOrderRequest(&req); len(validationErrors) > 0 {
			metrics.RecordOrderRejected(req.Instrument, "validation_failed")

			logging.LogOrderRejectedWithCorrelation(correlationID, "", req.ClientID, req.Instrument, "validation_failed", validationErrors)

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"error":   "Validation failed",
				"errors":  validationErrors,
			})
			return
		}

		var side models.OrderSide
		switch req.Side {
		case "buy":
			side = models.OrderSideBuy
		case "sell":
			side = models.OrderSideSell
		default:
			respondError(w, http.StatusBadRequest, "Invalid side: must be 'buy' or 'sell'")
			return
		}

		var orderType models.OrderType
		switch req.Type {
		case "limit":
			orderType = models.OrderTypeLimit
		case "market":
			orderType = models.OrderTypeMarket
		default:
			respondError(w, http.StatusBadRequest, "Invalid type: must be 'limit' or 'market'")
			return
		}

		order := &models.Order{
			ID:             uuid.New(),
			ClientID:       req.ClientID,
			Instrument:     req.Instrument,
			Side:           side,
			Type:           orderType,
			Price:          req.Price,
			Quantity:       req.Quantity,
			FilledQuantity: decimal.Zero,
			Status:         models.OrderStatusOpen,
			CreatedAt:      time.Now(),
		}

		metrics.RecordOrderReceived(req.Instrument, req.Side, req.Type)

		priceFloat, _ := req.Price.Float64()
		qtyFloat, _ := req.Quantity.Float64()
		logging.LogOrderReceivedWithCorrelation(correlationID, order.ID.String(), req.ClientID, req.Instrument, req.Side, req.Type, priceFloat, qtyFloat)

		startTime := time.Now()

		response, err := me.SubmitOrder(order)

		latency := time.Since(startTime).Seconds()
		metrics.RecordOrderLatency(req.Instrument, req.Type, latency)

		if err != nil {
			metrics.RecordOrderRejected(req.Instrument, "engine_error")
			logging.LogOrderRejectedWithCorrelation(correlationID, order.ID.String(), req.ClientID, req.Instrument, "engine_error", err.Error())
			respondError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to submit order: %v", err))
			return
		}

		if response.Error != nil {
			metrics.RecordOrderRejected(req.Instrument, "engine_rejection")
			logging.LogOrderRejectedWithCorrelation(correlationID, order.ID.String(), req.ClientID, req.Instrument, "engine_rejection", response.Error.Error())
			respondError(w, http.StatusBadRequest, response.Error.Error())
			return
		}

		if response.Order.Status == models.OrderStatusFilled || response.Order.Status == models.OrderStatusPartiallyFilled {
			metrics.RecordOrderMatched(req.Instrument, req.Side)

			filledQty, _ := response.Order.FilledQuantity.Float64()
			remainingQty, _ := response.Order.RemainingQuantity().Float64()
			logging.LogOrderMatchedWithCorrelation(correlationID, order.ID.String(), req.ClientID, req.Instrument, req.Side, filledQty, remainingQty, string(response.Order.Status))
		}

		for _, trade := range response.Trades {
			qty, _ := trade.Quantity.Float64()
			metrics.RecordTrade(req.Instrument, qty)

			price, _ := trade.Price.Float64()
			logging.LogTradeExecutedWithCorrelation(correlationID, trade.TradeID.String(), trade.BuyOrderID.String(), trade.SellOrderID.String(), trade.Instrument, price, qty, "", "")
		}

		// Build successful response
		orderResponse := OrderResponse{
			Success:   true,
			OrderID:   order.ID.String(),
			Order:     response.Order,
			Trades:    response.Trades,
			Message:   "Order accepted and processed",
			Timestamp: time.Now().UnixMilli(),
		}

		// Cache response if idempotency key was provided
		if idempotencyKey != "" && redisClient != nil {
			if err := cacheIdempotencyResponse(r.Context(), redisClient, idempotencyKey, &orderResponse); err != nil {
				// Log error but don't fail the request
				// The order was successfully processed
			}
		}

		// Return 202 Accepted (order has been accepted for processing)
		w.Header().Set("Content-Type", "application/json")
		if idempotencyKey != "" {
			w.Header().Set("X-Idempotency-Key", idempotencyKey)
		}
		w.WriteHeader(http.StatusAccepted)
		json.NewEncoder(w).Encode(orderResponse)
	}
}

// validateOrderRequest performs input validation on the order request
func validateOrderRequest(req *OrderRequest) []ValidationError {
	var errors []ValidationError

	// Validate client_id
	if req.ClientID == "" {
		errors = append(errors, ValidationError{
			Field:   "client_id",
			Message: "client_id is required",
		})
	}

	// Validate instrument
	if req.Instrument == "" {
		errors = append(errors, ValidationError{
			Field:   "instrument",
			Message: "instrument is required",
		})
	}

	// Validate side
	if req.Side != "buy" && req.Side != "sell" {
		errors = append(errors, ValidationError{
			Field:   "side",
			Message: "side must be 'buy' or 'sell'",
		})
	}

	// Validate type
	if req.Type != "limit" && req.Type != "market" {
		errors = append(errors, ValidationError{
			Field:   "type",
			Message: "type must be 'limit' or 'market'",
		})
	}

	// Validate quantity (must be positive)
	if req.Quantity.IsZero() || req.Quantity.IsNegative() {
		errors = append(errors, ValidationError{
			Field:   "quantity",
			Message: "quantity must be positive",
		})
	}

	// Validate price for limit orders (must be positive)
	if req.Type == "limit" {
		if req.Price.IsZero() || req.Price.IsNegative() {
			errors = append(errors, ValidationError{
				Field:   "price",
				Message: "price must be positive for limit orders",
			})
		}
	}

	// Additional business logic validations

	// Check for reasonable maximum quantity (e.g., 1000 BTC)
	maxQuantity := decimal.NewFromInt(1000)
	if req.Quantity.GreaterThan(maxQuantity) {
		errors = append(errors, ValidationError{
			Field:   "quantity",
			Message: fmt.Sprintf("quantity exceeds maximum allowed (%s)", maxQuantity),
		})
	}

	// Check for reasonable minimum quantity (e.g., 0.0001 BTC)
	minQuantity := decimal.NewFromFloat(0.0001)
	if req.Quantity.LessThan(minQuantity) {
		errors = append(errors, ValidationError{
			Field:   "quantity",
			Message: fmt.Sprintf("quantity below minimum allowed (%s)", minQuantity),
		})
	}

	// Check for reasonable price range for limit orders
	if req.Type == "limit" {
		maxPrice := decimal.NewFromInt(1000000) // $1M per BTC max
		minPrice := decimal.NewFromInt(1)       // $1 per BTC min

		if req.Price.GreaterThan(maxPrice) {
			errors = append(errors, ValidationError{
				Field:   "price",
				Message: fmt.Sprintf("price exceeds maximum allowed (%s)", maxPrice),
			})
		}

		if req.Price.LessThan(minPrice) {
			errors = append(errors, ValidationError{
				Field:   "price",
				Message: fmt.Sprintf("price below minimum allowed (%s)", minPrice),
			})
		}
	}

	return errors
}

// respondError is a helper to send error responses
func respondError(w http.ResponseWriter, statusCode int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":   false,
		"error":     message,
		"timestamp": time.Now().UnixMilli(),
	})
}

// checkIdempotencyKey checks if an idempotency key has been seen before
// Returns the cached response if found, nil otherwise
func checkIdempotencyKey(ctx context.Context, redisClient *redis.Client, key string) (*OrderResponse, error) {
	// Hash the key for consistent storage
	hashedKey := hashIdempotencyKey(key)
	redisKey := fmt.Sprintf("idempotency:%s", hashedKey)

	// Get cached response from Redis
	cachedData, err := redisClient.Get(ctx, redisKey).Result()
	if err == redis.Nil {
		// Key not found - this is a new request
		return nil, nil
	}
	if err != nil {
		// Redis error - log but don't fail
		return nil, err
	}

	// Deserialize cached response
	var response OrderResponse
	if err := json.Unmarshal([]byte(cachedData), &response); err != nil {
		return nil, err
	}

	return &response, nil
}

// cacheIdempotencyResponse stores the order response in Redis with 24-hour expiration
func cacheIdempotencyResponse(ctx context.Context, redisClient *redis.Client, key string, response *OrderResponse) error {
	// Hash the key for consistent storage
	hashedKey := hashIdempotencyKey(key)
	redisKey := fmt.Sprintf("idempotency:%s", hashedKey)

	// Serialize response to JSON
	responseData, err := json.Marshal(response)
	if err != nil {
		return fmt.Errorf("failed to serialize response: %w", err)
	}

	// Store in Redis with 24-hour expiration
	err = redisClient.Set(ctx, redisKey, responseData, 24*time.Hour).Err()
	if err != nil {
		return fmt.Errorf("failed to cache response: %w", err)
	}

	return nil
}

// hashIdempotencyKey creates a SHA-256 hash of the idempotency key
// This ensures consistent key length and prevents injection attacks
func hashIdempotencyKey(key string) string {
	hash := sha256.Sum256([]byte(key))
	return hex.EncodeToString(hash[:])
}
