package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/SahithiKokkula/backend-2-cent-ventures/engine"
	"github.com/SahithiKokkula/backend-2-cent-ventures/logging"
	"github.com/SahithiKokkula/backend-2-cent-ventures/persistence"
	"github.com/SahithiKokkula/backend-2-cent-ventures/ratelimit"
	"github.com/SahithiKokkula/backend-2-cent-ventures/websocket"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
	gorilla_ws "github.com/gorilla/websocket"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"
	"github.com/shopspring/decimal"
)

// contextKey is a custom type for context keys to avoid collisions
type contextKey string

// Router holds the HTTP router and all handlers
type Router struct {
	router          *mux.Router
	matchingEngine  *engine.MatchingEngine
	snapshotManager *engine.SnapshotManager
	recoveryManager *engine.RecoveryManager
	db              *sql.DB
	postgresStore   *persistence.PostgresStore
	redisClient     *redis.Client
	wsHub           *websocket.Hub
	wsUpgrader      gorilla_ws.Upgrader
	rateLimiter     *ratelimit.TokenBucketLimiter
}

// NewRouter creates a new router with all API routes
func NewRouter(me *engine.MatchingEngine, sm *engine.SnapshotManager, rm *engine.RecoveryManager, db *sql.DB, redisClient *redis.Client) *Router {
	hub := websocket.NewHub()
	go hub.Run()

	r := &Router{
		router:          mux.NewRouter(),
		matchingEngine:  me,
		snapshotManager: sm,
		recoveryManager: rm,
		db:              db,
		redisClient:     redisClient,
		wsHub:           hub,
		wsUpgrader: gorilla_ws.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			CheckOrigin: func(r *http.Request) bool {
				return true
			},
		},
	}

	if db != nil {
		r.postgresStore = persistence.NewPostgresStore(db)
	}

	snapshotProvider := websocket.NewSnapshotProvider(me, db)
	hub.SetSnapshotProvider(snapshotProvider)

	rateLimitConfig := ratelimit.Config{
		MaxTokens:            100,
		RefillRate:           10,
		RefillInterval:       1 * time.Second,
		KeyPrefix:            "ratelimit:",
		ConservativeFallback: true,
		WhitelistedKeys: []string{
			"client:admin",
			"client:market-maker-1",
			"client:monitoring",
			"ip:127.0.0.1",
		},
	}
	r.rateLimiter = ratelimit.NewTokenBucketLimiter(redisClient, rateLimitConfig)

	r.setupRoutes()
	return r
}

func (r *Router) setupRoutes() {
	r.router.Use(correlationIDMiddleware)

	rateLimitMiddleware := ratelimit.NewMiddleware(ratelimit.MiddlewareConfig{
		Limiter:      r.rateLimiter,
		KeyExtractor: ratelimit.ClientIDAndIPKeyExtractor,
		ErrorHandler: ratelimit.DefaultErrorHandler,
		SkipPaths:    []string{"/healthz", "/metrics", "/stream"}, // Skip rate limiting for these paths
	})
	r.router.Use(rateLimitMiddleware.Handler)

	// Order management routes - using dedicated handler from handlers.go with idempotency support
	r.router.HandleFunc("/orders", HandleSubmitOrder(r.matchingEngine, r.redisClient)).Methods("POST")
	r.router.HandleFunc("/orders/{order_id}/cancel", r.CancelOrder).Methods("POST")
	r.router.HandleFunc("/orders/{order_id}", r.GetOrder).Methods("GET")

	// Market data routes
	r.router.HandleFunc("/orderbook", r.GetOrderBook).Methods("GET")
	r.router.HandleFunc("/trades", r.GetTrades).Methods("GET")

	// WebSocket streaming endpoint
	r.router.HandleFunc("/stream", r.HandleWebSocket).Methods("GET")

	if r.snapshotManager != nil {
		r.router.HandleFunc("/snapshots/trigger", r.TriggerSnapshot).Methods("POST")
		r.router.HandleFunc("/snapshots/latest", r.GetLatestSnapshot).Methods("GET")
		r.router.HandleFunc("/snapshots/history", r.GetSnapshotHistory).Methods("GET")
	}

	if r.recoveryManager != nil {
		r.router.HandleFunc("/recovery/trigger", r.TriggerRecovery).Methods("POST")
		r.router.HandleFunc("/recovery/status", r.GetRecoveryStatus).Methods("GET")
	}

	r.router.HandleFunc("/healthz", r.HealthCheck).Methods("GET")

	r.router.Handle("/metrics", promhttp.Handler()).Methods("GET")
}

// ServeHTTP implements http.Handler interface
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.router.ServeHTTP(w, req)
}

func (r *Router) CancelOrder(w http.ResponseWriter, req *http.Request) {
	vars := mux.Vars(req)
	orderIDStr := vars["order_id"]

	orderID, err := parseUUID(orderIDStr)
	if err != nil {
		http.Error(w, "Invalid order_id", http.StatusBadRequest)
		return
	}

	resp, err := r.matchingEngine.CancelOrder(orderID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if resp.Error != nil {
		http.Error(w, resp.Error.Error(), http.StatusNotFound)
		return
	}

	correlationID := GetCorrelationID(req)

	if resp.Order != nil {
		logging.LogOrderCancelledWithCorrelation(correlationID, resp.Order.ID.String(), resp.Order.ClientID, resp.Order.Instrument, "user_requested")
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"order":   resp.Order,
	})
}

func (r *Router) GetOrder(w http.ResponseWriter, req *http.Request) {
	vars := mux.Vars(req)
	orderIDStr := vars["order_id"]

	orderID, err := uuid.Parse(orderIDStr)
	if err != nil {
		respondError(w, http.StatusBadRequest, "Invalid order ID format")
		return
	}

	if r.postgresStore != nil {
		ctx, cancel := context.WithTimeout(req.Context(), 3*time.Second)
		defer cancel()

		order, err := r.postgresStore.GetOrder(ctx, orderID)
		if err == nil {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-Data-Source", "database")
			_ = json.NewEncoder(w).Encode(order)
			return
		}
	}

	order := r.matchingEngine.GetOrderBook().GetOrder(orderIDStr)
	if order == nil {
		respondError(w, http.StatusNotFound, "Order not found")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Data-Source", "in-memory")
	_ = json.NewEncoder(w).Encode(order)
}

// OrderBookLevel represents a price level in the orderbook response
type OrderBookLevel struct {
	Price           decimal.Decimal `json:"price"`
	Size            decimal.Decimal `json:"size"`
	CumulativeDepth decimal.Decimal `json:"cumulative_depth"`
}

type OrderBookResponse struct {
	Instrument     string            `json:"instrument"`
	Bids           []OrderBookLevel  `json:"bids"`
	Asks           []OrderBookLevel  `json:"asks"`
	TotalBidVolume decimal.Decimal   `json:"total_bid_volume"`
	TotalAskVolume decimal.Decimal   `json:"total_ask_volume"`
	BidDepth       []decimal.Decimal `json:"bid_depth"`
	AskDepth       []decimal.Decimal `json:"ask_depth"`
	Timestamp      int64             `json:"timestamp"`

	Freshness      *FreshnessMetadata `json:"freshness,omitempty"`
	ResponseTimeMs float64            `json:"response_time_ms"`
	Source         string             `json:"source"`
}

type FreshnessMetadata struct {
	LastUpdateTime    int64 `json:"last_update_time"`
	AgeMs             int64 `json:"age_ms"`
	SnapshotAvailable bool  `json:"snapshot_available"`
	LastSnapshotTime  int64 `json:"last_snapshot_time,omitempty"`
	SnapshotAgeMs     int64 `json:"snapshot_age_ms,omitempty"`
	IsStale           bool  `json:"is_stale"`
	StaleThresholdMs  int64 `json:"stale_threshold_ms"`
}

func (r *Router) GetOrderBook(w http.ResponseWriter, req *http.Request) {
	startTime := time.Now()

	instrument := req.URL.Query().Get("instrument")
	if instrument == "" {
		instrument = "BTC-USD"
	}

	levelsStr := req.URL.Query().Get("levels")
	levels := 20
	if levelsStr != "" {
		if parsed, err := strconv.Atoi(levelsStr); err == nil && parsed > 0 {
			levels = parsed
		}
	}

	includeFreshness := req.URL.Query().Get("freshness") == "true"

	staleThresholdMs := int64(5000)
	if thresholdStr := req.URL.Query().Get("stale_threshold_ms"); thresholdStr != "" {
		if parsed, err := strconv.ParseInt(thresholdStr, 10, 64); err == nil && parsed > 0 {
			staleThresholdMs = parsed
		}
	}

	orderBook := r.matchingEngine.GetOrderBook()
	if orderBook == nil {
		http.Error(w, "Orderbook not available", http.StatusServiceUnavailable)
		return
	}

	bidLevels, askLevels := orderBook.GetTopLevels(levels)

	currentTime := getCurrentTimestamp()

	response := OrderBookResponse{
		Instrument:     instrument,
		Bids:           make([]OrderBookLevel, 0, len(bidLevels)),
		Asks:           make([]OrderBookLevel, 0, len(askLevels)),
		TotalBidVolume: decimal.Zero,
		TotalAskVolume: decimal.Zero,
		BidDepth:       make([]decimal.Decimal, 0, len(bidLevels)),
		AskDepth:       make([]decimal.Decimal, 0, len(askLevels)),
		Timestamp:      currentTime,
		Source:         "in-memory",
	}

	cumulativeBidDepth := decimal.Zero
	for _, level := range bidLevels {
		size := level.Volume
		cumulativeBidDepth = cumulativeBidDepth.Add(size)

		response.Bids = append(response.Bids, OrderBookLevel{
			Price:           level.Price,
			Size:            size,
			CumulativeDepth: cumulativeBidDepth,
		})
		response.BidDepth = append(response.BidDepth, cumulativeBidDepth)
		response.TotalBidVolume = response.TotalBidVolume.Add(size)
	}

	// Process asks (lowest to highest price)
	// Single pass, O(N) complexity
	cumulativeAskDepth := decimal.Zero
	for _, level := range askLevels {
		size := level.Volume
		cumulativeAskDepth = cumulativeAskDepth.Add(size)

		response.Asks = append(response.Asks, OrderBookLevel{
			Price:           level.Price,
			Size:            size,
			CumulativeDepth: cumulativeAskDepth,
		})
		response.AskDepth = append(response.AskDepth, cumulativeAskDepth)
		response.TotalAskVolume = response.TotalAskVolume.Add(size)
	}

	// Add freshness metadata if requested
	if includeFreshness {
		freshness := &FreshnessMetadata{
			LastUpdateTime:    currentTime,
			AgeMs:             0, // In-memory data is always fresh
			SnapshotAvailable: r.snapshotManager != nil,
			IsStale:           false,
			StaleThresholdMs:  staleThresholdMs,
		}

		// Check if snapshot is available for comparison
		if r.snapshotManager != nil {
			// Note: This would require adding a GetLastSnapshotTime method to SnapshotManager
			// For now, we indicate that snapshots are available but not the timing
			freshness.SnapshotAvailable = true
		}

		response.Freshness = freshness
	}

	// Calculate response time
	response.ResponseTimeMs = float64(time.Since(startTime).Microseconds()) / 1000.0

	w.Header().Set("Content-Type", "application/json")

	// Add cache headers to indicate freshness
	w.Header().Set("Cache-Control", "no-cache, must-revalidate")
	w.Header().Set("X-Data-Source", "in-memory")
	w.Header().Set("X-Response-Time-Ms", strconv.FormatFloat(response.ResponseTimeMs, 'f', 3, 64))

	_ = json.NewEncoder(w).Encode(response)
}

// GetTrades handles GET /trades?limit=50&instrument=BTC-USD&offset=0
// Returns most recent trades with pagination support
// Uses stable sort (timestamp DESC, trade_id DESC) for consistent pagination
func (r *Router) GetTrades(w http.ResponseWriter, req *http.Request) {
	// Parse query parameters
	instrument := req.URL.Query().Get("instrument")
	if instrument == "" {
		instrument = "BTC-USD" // Default instrument
	}

	// Parse limit (default 50, max 1000)
	limitStr := req.URL.Query().Get("limit")
	limit := 50
	if limitStr != "" {
		if parsedLimit, err := strconv.Atoi(limitStr); err == nil {
			limit = parsedLimit
			if limit > 1000 {
				limit = 1000 // Cap at 1000
			}
			if limit < 1 {
				limit = 1
			}
		}
	}

	// Parse offset for pagination (default 0)
	offsetStr := req.URL.Query().Get("offset")
	offset := 0
	if offsetStr != "" {
		if parsedOffset, err := strconv.Atoi(offsetStr); err == nil && parsedOffset >= 0 {
			offset = parsedOffset
		}
	}

	// If no database, return empty array
	if r.postgresStore == nil {
		respondError(w, http.StatusServiceUnavailable, "Database not available")
		return
	}

	// Query database with timeout
	ctx, cancel := context.WithTimeout(req.Context(), 5*time.Second)
	defer cancel()

	// Get trades from database with pagination
	trades, err := r.getTradesWithPagination(ctx, instrument, limit, offset)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to retrieve trades: "+err.Error())
		return
	}

	// Build response with pagination metadata
	response := map[string]interface{}{
		"instrument": instrument,
		"trades":     trades,
		"limit":      limit,
		"offset":     offset,
		"count":      len(trades),
		"has_more":   len(trades) == limit, // If we got full limit, there might be more
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Data-Source", "database")
	_ = json.NewEncoder(w).Encode(response)
}

// getTradesWithPagination retrieves trades with stable sorting for consistent pagination
// Uses ORDER BY timestamp DESC, trade_id DESC for stability
func (r *Router) getTradesWithPagination(ctx context.Context, instrument string, limit, offset int) ([]map[string]interface{}, error) {
	// SQL query with stable sort (timestamp DESC, trade_id DESC)
	// This ensures consistent pagination even if multiple trades have the same timestamp
	query := `
		SELECT 
			trade_id, 
			buy_order_id, 
			sell_order_id, 
			instrument,
			price, 
			quantity, 
			timestamp
		FROM trades
		WHERE instrument = $1
		ORDER BY timestamp DESC, trade_id DESC
		LIMIT $2 OFFSET $3
	`

	rows, err := r.db.QueryContext(ctx, query, instrument, limit, offset)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var trades []map[string]interface{}
	for rows.Next() {
		var tradeID, buyOrderID, sellOrderID uuid.UUID
		var instrumentStr string
		var priceStr, quantityStr string
		var timestamp time.Time

		err := rows.Scan(
			&tradeID,
			&buyOrderID,
			&sellOrderID,
			&instrumentStr,
			&priceStr,
			&quantityStr,
			&timestamp,
		)
		if err != nil {
			return nil, err
		}

		// Parse decimals
		price, _ := decimal.NewFromString(priceStr)
		quantity, _ := decimal.NewFromString(quantityStr)

		trade := map[string]interface{}{
			"trade_id":      tradeID.String(),
			"buy_order_id":  buyOrderID.String(),
			"sell_order_id": sellOrderID.String(),
			"instrument":    instrumentStr,
			"price":         price,
			"quantity":      quantity,
			"timestamp":     timestamp.UnixMilli(),
			"timestamp_iso": timestamp.Format(time.RFC3339Nano),
		}

		trades = append(trades, trade)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return trades, nil
}

// HealthCheck handles GET /healthz
func (r *Router) HealthCheck(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status": "healthy",
	})
}

// TriggerSnapshot handles POST /snapshots/trigger
func (r *Router) TriggerSnapshot(w http.ResponseWriter, req *http.Request) {
	if r.snapshotManager == nil {
		http.Error(w, "Snapshot manager not available", http.StatusServiceUnavailable)
		return
	}

	snapshotHandler := NewSnapshotHandler(r.snapshotManager)
	snapshotHandler.HandleTriggerSnapshot(w, req)
}

// GetLatestSnapshot handles GET /snapshots/latest
func (r *Router) GetLatestSnapshot(w http.ResponseWriter, req *http.Request) {
	if r.snapshotManager == nil {
		http.Error(w, "Snapshot manager not available", http.StatusServiceUnavailable)
		return
	}

	snapshotHandler := NewSnapshotHandler(r.snapshotManager)
	snapshotHandler.HandleGetLatestSnapshot(w, req)
}

// GetSnapshotHistory handles GET /snapshots/history
func (r *Router) GetSnapshotHistory(w http.ResponseWriter, req *http.Request) {
	if r.snapshotManager == nil {
		http.Error(w, "Snapshot manager not available", http.StatusServiceUnavailable)
		return
	}

	snapshotHandler := NewSnapshotHandler(r.snapshotManager)
	snapshotHandler.HandleGetSnapshot(w, req)
}

// TriggerRecovery handles POST /recovery/trigger
func (r *Router) TriggerRecovery(w http.ResponseWriter, req *http.Request) {
	if r.recoveryManager == nil {
		http.Error(w, "Recovery manager not available", http.StatusServiceUnavailable)
		return
	}

	orderbook := r.matchingEngine.GetOrderBook()
	recoveryHandler := NewRecoveryHandler(r.recoveryManager, orderbook)
	recoveryHandler.HandleTriggerRecovery(w, req)
}

// GetRecoveryStatus handles GET /recovery/status
func (r *Router) GetRecoveryStatus(w http.ResponseWriter, req *http.Request) {
	if r.recoveryManager == nil {
		http.Error(w, "Recovery manager not available", http.StatusServiceUnavailable)
		return
	}

	orderbook := r.matchingEngine.GetOrderBook()
	recoveryHandler := NewRecoveryHandler(r.recoveryManager, orderbook)
	recoveryHandler.HandleRecoveryStatus(w, req)
}

// Helper functions

func parseUUID(s string) (uuid.UUID, error) {
	return uuid.Parse(s)
}

func getCurrentTimestamp() int64 {
	return time.Now().UnixMilli()
}

// HandleWebSocket handles the /stream WebSocket endpoint
func (r *Router) HandleWebSocket(w http.ResponseWriter, req *http.Request) {
	// Upgrade HTTP connection to WebSocket
	conn, err := r.wsUpgrader.Upgrade(w, req, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}

	// Create new client
	client := websocket.NewClient(r.wsHub, conn)

	// Register client with hub
	r.wsHub.Register(client)

	// Start client's read and write pumps
	client.Start()
}

// GetWebSocketHub returns the WebSocket hub (for integration with matching engine)
func (r *Router) GetWebSocketHub() *websocket.Hub {
	return r.wsHub
}

// correlationIDMiddleware adds a correlation ID to each request for tracing
func correlationIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check if correlation ID is provided in header
		correlationID := r.Header.Get("X-Correlation-ID")

		// If not provided, generate a new one
		if correlationID == "" {
			correlationID = logging.NewCorrelationID()
		}

		// Add correlation ID to response header
		w.Header().Set("X-Correlation-ID", correlationID)

		// Add correlation ID to request context
		ctx := context.WithValue(r.Context(), contextKey("correlation_id"), correlationID)

		// Call next handler with updated context
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// GetCorrelationID extracts correlation ID from request context
func GetCorrelationID(r *http.Request) string {
	if correlationID, ok := r.Context().Value(contextKey("correlation_id")).(string); ok {
		return correlationID
	}
	return ""
}
