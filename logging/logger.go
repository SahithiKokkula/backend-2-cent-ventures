package logging

import (
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
)

var log *logrus.Logger

type ErrorRateLimiter struct {
	mu            sync.Mutex
	errorCounts   map[string]*errorEntry
	cleanupTicker *time.Ticker
}

type errorEntry struct {
	count      int
	firstSeen  time.Time
	lastLogged time.Time
	suppressed int
}

var (
	rateLimiter     *ErrorRateLimiter
	rateLimitWindow = 1 * time.Minute
	maxErrorsPerMin = 5
)

func NewErrorRateLimiter() *ErrorRateLimiter {
	limiter := &ErrorRateLimiter{
		errorCounts:   make(map[string]*errorEntry),
		cleanupTicker: time.NewTicker(5 * time.Minute),
	}

	go func() {
		for range limiter.cleanupTicker.C {
			limiter.cleanup()
		}
	}()

	return limiter
}

func (rl *ErrorRateLimiter) ShouldLog(errorKey string) (shouldLog bool, suppressedCount int) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	entry, exists := rl.errorCounts[errorKey]

	if !exists {
		rl.errorCounts[errorKey] = &errorEntry{
			count:      1,
			firstSeen:  now,
			lastLogged: now,
		}
		return true, 0
	}

	if now.Sub(entry.firstSeen) > rateLimitWindow {
		suppressedCount = entry.suppressed
		rl.errorCounts[errorKey] = &errorEntry{
			count:      1,
			firstSeen:  now,
			lastLogged: now,
		}
		return true, suppressedCount
	}

	entry.count++

	if entry.count <= maxErrorsPerMin {
		entry.lastLogged = now
		return true, 0
	}

	entry.suppressed++
	return false, 0
}

func (rl *ErrorRateLimiter) cleanup() {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	for key, entry := range rl.errorCounts {
		if now.Sub(entry.lastLogged) > 10*time.Minute {
			delete(rl.errorCounts, key)
		}
	}
}

// InitLogger initializes the structured logger with JSON format
func InitLogger() *logrus.Logger {
	log = logrus.New()

	// Set JSON formatter for structured logging
	log.SetFormatter(&logrus.JSONFormatter{
		TimestampFormat: time.RFC3339Nano,
		FieldMap: logrus.FieldMap{
			logrus.FieldKeyTime:  "ts",
			logrus.FieldKeyLevel: "level",
			logrus.FieldKeyMsg:   "message",
		},
	})

	// Output to stdout (can be redirected to file in production)
	log.SetOutput(os.Stdout)

	// Set log level from environment or default to Info
	logLevel := os.Getenv("LOG_LEVEL")
	switch logLevel {
	case "debug":
		log.SetLevel(logrus.DebugLevel)
	case "info":
		log.SetLevel(logrus.InfoLevel)
	case "warn":
		log.SetLevel(logrus.WarnLevel)
	case "error":
		log.SetLevel(logrus.ErrorLevel)
	default:
		log.SetLevel(logrus.InfoLevel)
	}

	// Initialize error rate limiter
	rateLimiter = NewErrorRateLimiter()

	log.WithFields(logrus.Fields{
		"event":              "logger_initialized",
		"level":              log.Level.String(),
		"rate_limit_enabled": true,
		"max_errors_per_min": maxErrorsPerMin,
	}).Info("Structured logging initialized")

	return log
}

// NewCorrelationID generates a new correlation ID for request tracing
func NewCorrelationID() string {
	return uuid.New().String()
}

// WithCorrelationID returns logger fields with correlation ID
func WithCorrelationID(correlationID string) logrus.Fields {
	return logrus.Fields{
		"correlation_id": correlationID,
	}
}

// GetLogger returns the global logger instance
func GetLogger() *logrus.Logger {
	if log == nil {
		return InitLogger()
	}
	return log
}

// Event types as constants
const (
	EventOrderReceived         = "order_received"
	EventOrderMatched          = "order_matched"
	EventOrderCancelled        = "order_cancelled"
	EventOrderRejected         = "order_rejected"
	EventTradeExecuted         = "trade_executed"
	EventDBError               = "db_error"
	EventDBSuccess             = "db_success"
	EventServerStarted         = "server_started"
	EventServerStopped         = "server_stopped"
	EventWebSocketConnected    = "websocket_connected"
	EventWebSocketDisconnected = "websocket_disconnected"
	EventSnapshotCreated       = "snapshot_created"
	EventRecoveryStarted       = "recovery_started"
	EventRecoveryCompleted     = "recovery_completed"
)

// LogOrderReceived logs when an order is received
func LogOrderReceived(orderID, clientID, instrument, side, orderType string, price, quantity float64) {
	LogOrderReceivedWithCorrelation("", orderID, clientID, instrument, side, orderType, price, quantity)
}

// LogOrderReceivedWithCorrelation logs when an order is received with correlation ID
func LogOrderReceivedWithCorrelation(correlationID, orderID, clientID, instrument, side, orderType string, price, quantity float64) {
	fields := logrus.Fields{
		"event":      EventOrderReceived,
		"order_id":   orderID,
		"client_id":  clientID,
		"instrument": instrument,
		"side":       side,
		"type":       orderType,
		"price":      price,
		"quantity":   quantity,
	}
	if correlationID != "" {
		fields["correlation_id"] = correlationID
	}
	GetLogger().WithFields(fields).Info("Order received")
}

// LogOrderMatched logs when an order is matched/filled
func LogOrderMatched(orderID, clientID, instrument, side string, filledQty, remainingQty float64, status string) {
	LogOrderMatchedWithCorrelation("", orderID, clientID, instrument, side, filledQty, remainingQty, status)
}

// LogOrderMatchedWithCorrelation logs when an order is matched/filled with correlation ID
func LogOrderMatchedWithCorrelation(correlationID, orderID, clientID, instrument, side string, filledQty, remainingQty float64, status string) {
	fields := logrus.Fields{
		"event":         EventOrderMatched,
		"order_id":      orderID,
		"client_id":     clientID,
		"instrument":    instrument,
		"side":          side,
		"filled_qty":    filledQty,
		"remaining_qty": remainingQty,
		"status":        status,
	}
	if correlationID != "" {
		fields["correlation_id"] = correlationID
	}
	GetLogger().WithFields(fields).Info("Order matched")
}

// LogTradeExecuted logs when a trade is executed
func LogTradeExecuted(tradeID, buyOrderID, sellOrderID, instrument string, price, quantity float64, buyerClient, sellerClient string) {
	LogTradeExecutedWithCorrelation("", tradeID, buyOrderID, sellOrderID, instrument, price, quantity, buyerClient, sellerClient)
}

// LogTradeExecutedWithCorrelation logs when a trade is executed with correlation ID
func LogTradeExecutedWithCorrelation(correlationID, tradeID, buyOrderID, sellOrderID, instrument string, price, quantity float64, buyerClient, sellerClient string) {
	fields := logrus.Fields{
		"event":         EventTradeExecuted,
		"trade_id":      tradeID,
		"buy_order_id":  buyOrderID,
		"sell_order_id": sellOrderID,
		"instrument":    instrument,
		"price":         price,
		"quantity":      quantity,
		"buyer_client":  buyerClient,
		"seller_client": sellerClient,
	}
	if correlationID != "" {
		fields["correlation_id"] = correlationID
	}
	GetLogger().WithFields(fields).Info("Trade executed")
}

// LogOrderCancelled logs when an order is cancelled
func LogOrderCancelled(orderID, clientID, instrument string, reason string) {
	LogOrderCancelledWithCorrelation("", orderID, clientID, instrument, reason)
}

// LogOrderCancelledWithCorrelation logs when an order is cancelled with correlation ID
func LogOrderCancelledWithCorrelation(correlationID, orderID, clientID, instrument string, reason string) {
	fields := logrus.Fields{
		"event":      EventOrderCancelled,
		"order_id":   orderID,
		"client_id":  clientID,
		"instrument": instrument,
		"reason":     reason,
	}
	if correlationID != "" {
		fields["correlation_id"] = correlationID
	}
	GetLogger().WithFields(fields).Info("Order cancelled")
}

// LogOrderRejected logs when an order is rejected
func LogOrderRejected(orderID, clientID, instrument, reason string, details interface{}) {
	LogOrderRejectedWithCorrelation("", orderID, clientID, instrument, reason, details)
}

// LogOrderRejectedWithCorrelation logs when an order is rejected with correlation ID
func LogOrderRejectedWithCorrelation(correlationID, orderID, clientID, instrument, reason string, details interface{}) {
	fields := logrus.Fields{
		"event":      EventOrderRejected,
		"order_id":   orderID,
		"client_id":  clientID,
		"instrument": instrument,
		"reason":     reason,
		"details":    details,
	}
	if correlationID != "" {
		fields["correlation_id"] = correlationID
	}
	GetLogger().WithFields(fields).Warn("Order rejected")
}

// LogDBError logs database errors with rate limiting
func LogDBError(operation, table string, err error, details interface{}) {
	// Create error key for rate limiting
	errorKey := fmt.Sprintf("%s:%s:%s", operation, table, err.Error())

	shouldLog, suppressedCount := rateLimiter.ShouldLog(errorKey)

	if !shouldLog {
		return // Suppressed due to rate limiting
	}

	fields := logrus.Fields{
		"event":     EventDBError,
		"operation": operation,
		"table":     table,
		"error":     err.Error(),
		"details":   details,
	}

	// Add suppression notice if errors were suppressed
	if suppressedCount > 0 {
		fields["suppressed_count"] = suppressedCount
		fields["suppressed_msg"] = fmt.Sprintf("%d identical errors were suppressed in the last minute", suppressedCount)
	}

	GetLogger().WithFields(fields).Error("Database error")
}

// LogDBSuccess logs successful database operations
func LogDBSuccess(operation, table string, recordCount int, details interface{}) {
	GetLogger().WithFields(logrus.Fields{
		"event":        EventDBSuccess,
		"operation":    operation,
		"table":        table,
		"record_count": recordCount,
		"details":      details,
	}).Debug("Database operation successful")
}

// LogServerStarted logs server startup
func LogServerStarted(port int, features interface{}) {
	GetLogger().WithFields(logrus.Fields{
		"event":    EventServerStarted,
		"port":     port,
		"features": features,
	}).Info("Trading engine server started")
}

// LogWebSocketEvent logs WebSocket connection events
func LogWebSocketEvent(event, clientID string, topics []string) {
	GetLogger().WithFields(logrus.Fields{
		"event":     event,
		"client_id": clientID,
		"topics":    topics,
	}).Info("WebSocket event")
}

// LogSnapshot logs snapshot creation
func LogSnapshotCreated(snapshotID int64, instrument string, bidLevels, askLevels int, compressed bool, sizeBytes int64) {
	GetLogger().WithFields(logrus.Fields{
		"event":       EventSnapshotCreated,
		"snapshot_id": snapshotID,
		"instrument":  instrument,
		"bid_levels":  bidLevels,
		"ask_levels":  askLevels,
		"compressed":  compressed,
		"size_bytes":  sizeBytes,
	}).Info("Snapshot created")
}

// LogRecovery logs recovery events
func LogRecovery(event, instrument string, ordersRecovered int, duration time.Duration) {
	GetLogger().WithFields(logrus.Fields{
		"event":            event,
		"instrument":       instrument,
		"orders_recovered": ordersRecovered,
		"duration_ms":      duration.Milliseconds(),
	}).Info("Recovery event")
}

// LogWithFields provides a flexible logging method
func LogWithFields(level logrus.Level, message string, fields logrus.Fields) {
	GetLogger().WithFields(fields).Log(level, message)
}
