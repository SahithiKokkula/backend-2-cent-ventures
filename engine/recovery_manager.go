package engine

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"time"

	"github.com/SahithiKokkula/backend-2-cent-ventures/models"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type RecoveryManager struct {
	db              *sql.DB
	snapshotManager *SnapshotManager
	instrument      string
}

func NewRecoveryManager(db *sql.DB, snapshotManager *SnapshotManager, instrument string) *RecoveryManager {
	return &RecoveryManager{
		db:              db,
		snapshotManager: snapshotManager,
		instrument:      instrument,
	}
}

type RecoveryReport struct {
	StartTime      time.Time     `json:"start_time"`
	EndTime        time.Time     `json:"end_time"`
	Duration       time.Duration `json:"duration"`
	RecoveryMethod string        `json:"recovery_method"`
	DryRun         bool          `json:"dry_run"`

	SnapshotUsed      bool      `json:"snapshot_used"`
	SnapshotID        int64     `json:"snapshot_id,omitempty"`
	SnapshotTimestamp time.Time `json:"snapshot_timestamp,omitempty"`
	SnapshotBidLevels int       `json:"snapshot_bid_levels,omitempty"`
	SnapshotAskLevels int       `json:"snapshot_ask_levels,omitempty"`

	EventsReplayed      int           `json:"events_replayed"`
	OrdersRestored      int           `json:"orders_restored"`
	EventsSinceSnapshot time.Duration `json:"events_since_snapshot,omitempty"`

	BeforeTotalBidVolume decimal.Decimal `json:"before_total_bid_volume"`
	BeforeTotalAskVolume decimal.Decimal `json:"before_total_ask_volume"`
	BeforeBidLevels      int             `json:"before_bid_levels"`
	BeforeAskLevels      int             `json:"before_ask_levels"`

	AfterTotalBidVolume decimal.Decimal  `json:"after_total_bid_volume"`
	AfterTotalAskVolume decimal.Decimal  `json:"after_total_ask_volume"`
	AfterBidLevels      int              `json:"after_bid_levels"`
	AfterAskLevels      int              `json:"after_ask_levels"`
	AfterBestBid        *decimal.Decimal `json:"after_best_bid,omitempty"`
	AfterBestAsk        *decimal.Decimal `json:"after_best_ask,omitempty"`
	AfterSpread         *decimal.Decimal `json:"after_spread,omitempty"`

	DatabaseBidVolume decimal.Decimal `json:"database_bid_volume,omitempty"`
	DatabaseAskVolume decimal.Decimal `json:"database_ask_volume,omitempty"`
	DatabaseBidCount  int             `json:"database_bid_count,omitempty"`
	DatabaseAskCount  int             `json:"database_ask_count,omitempty"`

	Diffs []RecoveryDiff `json:"diffs,omitempty"`

	ValidationPassed bool     `json:"validation_passed"`
	ValidationErrors []string `json:"validation_errors,omitempty"`

	SnapshotLoadTime time.Duration `json:"snapshot_load_time,omitempty"`
	EventReplayTime  time.Duration `json:"event_replay_time,omitempty"`
	ValidationTime   time.Duration `json:"validation_time,omitempty"`
}

type RecoveryDiff struct {
	Field          string `json:"field"`
	RecoveredValue string `json:"recovered_value"`
	ExpectedValue  string `json:"expected_value"`
	Difference     string `json:"difference"`
	Severity       string `json:"severity"`
	Description    string `json:"description"`
}

type RecoveryOptions struct {
	DryRun      bool
	Verbose     bool
	FailOnError bool
}

// RecoverOrderbook performs full recovery from snapshot + events
func (rm *RecoveryManager) RecoverOrderbook(ctx context.Context, orderbook *OrderBook) (*RecoveryReport, error) {
	return rm.RecoverOrderbookWithOptions(ctx, orderbook, RecoveryOptions{DryRun: false})
}

// RecoverOrderbookWithOptions performs recovery with configurable options
func (rm *RecoveryManager) RecoverOrderbookWithOptions(ctx context.Context, orderbook *OrderBook, opts RecoveryOptions) (*RecoveryReport, error) {
	report := &RecoveryReport{
		StartTime:            time.Now(),
		DryRun:               opts.DryRun,
		BeforeTotalBidVolume: decimal.Zero,
		BeforeTotalAskVolume: decimal.Zero,
		BeforeBidLevels:      0,
		BeforeAskLevels:      0,
	}

	if opts.DryRun {
		log.Println("� Starting DRY-RUN recovery (no changes will be applied)...")
	} else {
		log.Println("�🔄 Starting orderbook recovery process...")
	}

	// Step 1: Try to load latest snapshot
	snapshotStart := time.Now()
	snapshot, err := rm.snapshotManager.GetLatestSnapshot(ctx)
	report.SnapshotLoadTime = time.Since(snapshotStart)

	var snapshotTimestamp time.Time

	if err == nil && snapshot != nil {
		report.SnapshotUsed = true
		report.SnapshotID = snapshot.SnapshotID
		report.SnapshotTimestamp = snapshot.SnapshotTimestamp
		report.SnapshotBidLevels = len(snapshot.BidLevels)
		report.SnapshotAskLevels = len(snapshot.AskLevels)
		snapshotTimestamp = snapshot.SnapshotTimestamp

		log.Printf("📸 Found snapshot: ID=%d, timestamp=%s, %d bid levels, %d ask levels",
			snapshot.SnapshotID, snapshot.SnapshotTimestamp, len(snapshot.BidLevels), len(snapshot.AskLevels))

		// Snapshot doesn't contain actual orders, just aggregate levels
		// We'll need to replay events/orders regardless
	} else {
		log.Println("📸 No snapshot found, will replay all events from beginning")
		report.RecoveryMethod = "full_replay"
	}

	// Step 2: Reload open orders from database
	replayStart := time.Now()
	orders, err := rm.loadOpenOrders(ctx, snapshotTimestamp)
	if err != nil {
		return report, fmt.Errorf("failed to load open orders: %w", err)
	}

	log.Printf("📚 Loaded %d open orders from database", len(orders))
	report.OrdersRestored = len(orders)

	// Step 3: Rebuild orderbook from orders
	var tempOrderbook *OrderBook
	if opts.DryRun {
		// Create a temporary orderbook for dry-run
		tempOrderbook = NewOrderBook(rm.instrument)
		log.Println("🔍 DRY-RUN: Building temporary orderbook (original unchanged)")
		for _, order := range orders {
			tempOrderbook.AddOrder(order)
		}
	} else {
		// Apply to real orderbook
		for _, order := range orders {
			orderbook.AddOrder(order)
		}
		tempOrderbook = orderbook
	}

	report.EventReplayTime = time.Since(replayStart)

	if report.SnapshotUsed {
		report.EventsSinceSnapshot = time.Since(snapshotTimestamp)
		report.RecoveryMethod = "snapshot_plus_orders"
	}

	// Step 4: Capture post-recovery state (from temp or real orderbook)
	rm.capturePostRecoveryState(tempOrderbook, report)

	// Step 5: Validate recovery and generate diffs
	validationStart := time.Now()
	rm.validateRecoveryWithDiffs(ctx, tempOrderbook, report, opts.Verbose)
	report.ValidationTime = time.Since(validationStart)

	report.EndTime = time.Now()
	report.Duration = report.EndTime.Sub(report.StartTime)

	// Step 6: Print diagnostic report
	rm.printRecoveryReport(report)

	if opts.DryRun {
		log.Printf("🔍 DRY-RUN completed in %v (no changes applied)", report.Duration)
	} else {
		log.Printf("Orderbook recovery completed in %v", report.Duration)
	}

	return report, nil
}

// loadOpenOrders loads all open orders from the database
func (rm *RecoveryManager) loadOpenOrders(ctx context.Context, sinceTimestamp time.Time) ([]*models.Order, error) {
	// Query orders from orders_snapshot (materialized view with current state)
	// Only load orders that are still OPEN or PARTIALLY_FILLED
	query := `
		SELECT 
			order_id, 
			instrument, 
			side, 
			price, 
			quantity, 
			filled_quantity, 
			status,
			created_at,
			updated_at
		FROM orders_snapshot
		WHERE instrument = $1
		  AND status IN ('open', 'partially_filled')
		ORDER BY created_at ASC
	`

	rows, err := rm.db.QueryContext(ctx, query, rm.instrument)
	if err != nil {
		return nil, fmt.Errorf("failed to query orders: %w", err)
	}
	defer func() {
		_ = rows.Close() // Ignore error on defer close
	}()

	orders := make([]*models.Order, 0)

	for rows.Next() {
		var (
			orderID      string
			instrument   string
			side         string
			priceStr     string
			quantityStr  string
			filledQtyStr string
			status       string
			createdAt    time.Time
			updatedAt    time.Time
		)

		err := rows.Scan(
			&orderID,
			&instrument,
			&side,
			&priceStr,
			&quantityStr,
			&filledQtyStr,
			&status,
			&createdAt,
			&updatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan order: %w", err)
		}

		price, err := decimal.NewFromString(priceStr)
		if err != nil {
			log.Printf(" Invalid price for order %s: %s", orderID, priceStr)
			continue
		}

		quantity, err := decimal.NewFromString(quantityStr)
		if err != nil {
			log.Printf(" Invalid quantity for order %s: %s", orderID, quantityStr)
			continue
		}

		filledQty, err := decimal.NewFromString(filledQtyStr)
		if err != nil {
			log.Printf(" Invalid filled_quantity for order %s: %s", orderID, filledQtyStr)
			continue
		}

		// Convert side string to OrderSide
		var orderSide models.OrderSide
		if side == "buy" {
			orderSide = models.OrderSideBuy
		} else {
			orderSide = models.OrderSideSell
		}

		// Convert status string to OrderStatus
		var orderStatus models.OrderStatus
		switch status {
		case "open":
			orderStatus = models.OrderStatusOpen
		case "partially_filled":
			orderStatus = models.OrderStatusPartiallyFilled
		default:
			continue // Skip other statuses
		}

		// Parse order ID as UUID
		orderUUID, err := uuid.Parse(orderID)
		if err != nil {
			log.Printf(" Invalid order ID format: %s", orderID)
			continue
		}

		order := &models.Order{
			ID:             orderUUID,
			Instrument:     instrument,
			Side:           orderSide,
			Type:           models.OrderTypeLimit, // Assume limit orders
			Price:          price,
			Quantity:       quantity,
			FilledQuantity: filledQty,
			Status:         orderStatus,
			CreatedAt:      createdAt,
			UpdatedAt:      updatedAt,
		}

		orders = append(orders, order)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating orders: %w", err)
	}

	return orders, nil
}

// capturePostRecoveryState captures orderbook state after recovery
func (rm *RecoveryManager) capturePostRecoveryState(orderbook *OrderBook, report *RecoveryReport) {
	// Get top levels to calculate totals
	bidLevels, askLevels := orderbook.GetTopLevels(100) // Get up to 100 levels

	report.AfterBidLevels = len(bidLevels)
	report.AfterAskLevels = len(askLevels)

	// Calculate total volumes
	for _, level := range bidLevels {
		report.AfterTotalBidVolume = report.AfterTotalBidVolume.Add(level.Volume)
	}

	for _, level := range askLevels {
		report.AfterTotalAskVolume = report.AfterTotalAskVolume.Add(level.Volume)
	}

	// Get best bid/ask
	bestBid := orderbook.GetBestBid()
	if bestBid != nil {
		report.AfterBestBid = &bestBid.Price
	}

	bestAsk := orderbook.GetBestAsk()
	if bestAsk != nil {
		report.AfterBestAsk = &bestAsk.Price
	}

	// Calculate spread
	if report.AfterBestBid != nil && report.AfterBestAsk != nil {
		spread := report.AfterBestAsk.Sub(*report.AfterBestBid)
		report.AfterSpread = &spread
	}
}

// Removed unused validateRecovery function - use validateRecoveryWithDiffs directly

// validateVolumeWithTolerance is a helper to validate bid/ask volume against database
func validateVolumeWithTolerance(report *RecoveryReport, volumeType string, recoveredVol, dbVol decimal.Decimal, verbose bool) {
	diff := recoveredVol.Sub(dbVol)
	tolerance := decimal.NewFromFloat(0.00000001) // Allow tiny floating point differences

	// Capitalize volume type for display
	displayType := volumeType
	if len(volumeType) > 0 {
		displayType = string(volumeType[0]-32) + volumeType[1:] // Simple uppercase first char
	}

	if diff.Abs().GreaterThan(tolerance) {
		err := fmt.Sprintf("%s volume mismatch: orderbook=%s, database=%s, diff=%s",
			displayType, recoveredVol, dbVol, diff)
		report.ValidationErrors = append(report.ValidationErrors, err)
		report.ValidationPassed = false
		log.Printf("Validation error: %s", err)

		diffPercent := decimal.Zero
		if !dbVol.IsZero() {
			diffPercent = diff.Div(dbVol).Mul(decimal.NewFromInt(100))
		}

		report.Diffs = append(report.Diffs, RecoveryDiff{
			Field:          volumeType + "_volume",
			RecoveredValue: recoveredVol.String(),
			ExpectedValue:  dbVol.String(),
			Difference:     fmt.Sprintf("%s (%.2f%%)", diff.String(), diffPercent.InexactFloat64()),
			Severity:       "error",
			Description:    fmt.Sprintf("Recovered %s volume does not match database total", volumeType),
		})
	} else if verbose && !diff.IsZero() {
		// Info-level diff for small differences
		report.Diffs = append(report.Diffs, RecoveryDiff{
			Field:          volumeType + "_volume",
			RecoveredValue: recoveredVol.String(),
			ExpectedValue:  dbVol.String(),
			Difference:     diff.String(),
			Severity:       "info",
			Description:    fmt.Sprintf("Tiny %s volume difference within tolerance", volumeType),
		})
	}
}

// validateRecoveryWithDiffs performs validation and generates diagnostic diffs
func (rm *RecoveryManager) validateRecoveryWithDiffs(ctx context.Context, orderbook *OrderBook, report *RecoveryReport, verbose bool) {
	report.ValidationPassed = true
	report.ValidationErrors = make([]string, 0)
	report.Diffs = make([]RecoveryDiff, 0)

	// Validation 1: Check if we have any orders
	if report.OrdersRestored == 0 && (report.AfterBidLevels > 0 || report.AfterAskLevels > 0) {
		err := "Orderbook has levels but no orders were loaded"
		report.ValidationErrors = append(report.ValidationErrors, err)
		report.ValidationPassed = false
		log.Printf("Validation error: %s", err)
	}

	// Validation 2: Verify volumes are non-negative
	if report.AfterTotalBidVolume.IsNegative() {
		err := fmt.Sprintf("Negative bid volume: %s", report.AfterTotalBidVolume)
		report.ValidationErrors = append(report.ValidationErrors, err)
		report.ValidationPassed = false
		log.Printf("Validation error: %s", err)

		report.Diffs = append(report.Diffs, RecoveryDiff{
			Field:          "total_bid_volume",
			RecoveredValue: report.AfterTotalBidVolume.String(),
			ExpectedValue:  ">= 0",
			Difference:     report.AfterTotalBidVolume.String(),
			Severity:       "error",
			Description:    "Bid volume must be non-negative",
		})
	}

	if report.AfterTotalAskVolume.IsNegative() {
		err := fmt.Sprintf("Negative ask volume: %s", report.AfterTotalAskVolume)
		report.ValidationErrors = append(report.ValidationErrors, err)
		report.ValidationPassed = false
		log.Printf("Validation error: %s", err)

		report.Diffs = append(report.Diffs, RecoveryDiff{
			Field:          "total_ask_volume",
			RecoveredValue: report.AfterTotalAskVolume.String(),
			ExpectedValue:  ">= 0",
			Difference:     report.AfterTotalAskVolume.String(),
			Severity:       "error",
			Description:    "Ask volume must be non-negative",
		})
	}

	// Validation 3: Verify spread is positive if both sides exist
	if report.AfterSpread != nil && report.AfterSpread.IsNegative() {
		err := fmt.Sprintf("Negative spread: %s (bid=%s, ask=%s)",
			report.AfterSpread, report.AfterBestBid, report.AfterBestAsk)
		report.ValidationErrors = append(report.ValidationErrors, err)
		report.ValidationPassed = false
		log.Printf("Validation error: %s", err)

		report.Diffs = append(report.Diffs, RecoveryDiff{
			Field:          "spread",
			RecoveredValue: report.AfterSpread.String(),
			ExpectedValue:  ">= 0",
			Difference:     report.AfterSpread.String(),
			Severity:       "error",
			Description:    "Crossed book detected - best ask < best bid",
		})
	}

	// Validation 4: Compare against database totals and generate diffs
	dbTotals, err := rm.getDatabaseTotals(ctx)
	if err == nil {
		// Store database values in report
		report.DatabaseBidVolume = dbTotals.TotalBidVolume
		report.DatabaseAskVolume = dbTotals.TotalAskVolume
		report.DatabaseBidCount = dbTotals.OpenBuyOrderCount
		report.DatabaseAskCount = dbTotals.OpenSellOrderCount

		// Validate bid and ask volumes using helper
		validateVolumeWithTolerance(report, "bid", report.AfterTotalBidVolume, dbTotals.TotalBidVolume, verbose)
		validateVolumeWithTolerance(report, "ask", report.AfterTotalAskVolume, dbTotals.TotalAskVolume, verbose)

		// Check order counts
		if verbose {
			if dbTotals.OpenBuyOrderCount > 0 {
				report.Diffs = append(report.Diffs, RecoveryDiff{
					Field:          "ask_order_count",
					RecoveredValue: fmt.Sprintf("%d", report.AfterAskLevels),
					ExpectedValue:  fmt.Sprintf("%d orders", dbTotals.OpenSellOrderCount),
					Difference:     "N/A",
					Severity:       "info",
					Description:    "Orderbook levels vs database orders (levels may aggregate multiple orders)",
				})
			}

			if dbTotals.OpenSellOrderCount > 0 {
				report.Diffs = append(report.Diffs, RecoveryDiff{
					Field:          "ask_order_count",
					RecoveredValue: fmt.Sprintf("%d", report.AfterAskLevels),
					ExpectedValue:  fmt.Sprintf("%d orders", dbTotals.OpenSellOrderCount),
					Difference:     "N/A",
					Severity:       "info",
					Description:    "Orderbook levels vs database orders (levels may aggregate multiple orders)",
				})
			}
		}
	} else {
		log.Printf(" Could not fetch database totals for validation: %v", err)
	}

	// Print diff summary if there are any
	if len(report.Diffs) > 0 {
		log.Println("\n┌─────────────────────────────────────────────────────────────┐")
		log.Println("│             DIAGNOSTIC DIFF OUTPUT                          │")
		log.Println("└─────────────────────────────────────────────────────────────┘")

		for _, diff := range report.Diffs {
			severityIcon := "ℹ️"
			switch diff.Severity {
			case "error":
				severityIcon = "❌"
			case "warning":
				severityIcon = "⚠️"
			}

			log.Printf("%s %s:", severityIcon, diff.Field)
			log.Printf("  Recovered: %s", diff.RecoveredValue)
			log.Printf("  Expected:  %s", diff.ExpectedValue)
			if diff.Difference != "N/A" {
				log.Printf("  Diff:      %s", diff.Difference)
			}
			log.Printf("  Info:      %s\n", diff.Description)
		}
	}

	if report.ValidationPassed {
		log.Println("All validation checks passed")
	} else {
		log.Printf(" Validation failed with %d errors", len(report.ValidationErrors))
	}
}

// DatabaseTotals represents aggregate data from database
type DatabaseTotals struct {
	TotalBidVolume     decimal.Decimal
	TotalAskVolume     decimal.Decimal
	OpenOrderCount     int
	OpenBuyOrderCount  int
	OpenSellOrderCount int
}

// getDatabaseTotals queries database for aggregate totals
func (rm *RecoveryManager) getDatabaseTotals(ctx context.Context) (*DatabaseTotals, error) {
	query := `
		SELECT 
			COALESCE(SUM(CASE WHEN side = 'buy' THEN quantity - filled_quantity ELSE 0 END), 0) as total_bid_volume,
			COALESCE(SUM(CASE WHEN side = 'sell' THEN quantity - filled_quantity ELSE 0 END), 0) as total_ask_volume,
			COUNT(*) as open_order_count,
			COUNT(CASE WHEN side = 'buy' THEN 1 END) as open_buy_order_count,
			COUNT(CASE WHEN side = 'sell' THEN 1 END) as open_sell_order_count
		FROM orders_snapshot
		WHERE instrument = $1
		  AND status IN ('open', 'partially_filled')
	`

	var bidVolumeStr, askVolumeStr string
	var count, buyCount, sellCount int

	err := rm.db.QueryRowContext(ctx, query, rm.instrument).Scan(&bidVolumeStr, &askVolumeStr, &count, &buyCount, &sellCount)
	if err != nil {
		return nil, fmt.Errorf("failed to query totals: %w", err)
	}

	bidVolume, _ := decimal.NewFromString(bidVolumeStr)
	askVolume, _ := decimal.NewFromString(askVolumeStr)

	return &DatabaseTotals{
		TotalBidVolume:     bidVolume,
		TotalAskVolume:     askVolume,
		OpenOrderCount:     count,
		OpenBuyOrderCount:  buyCount,
		OpenSellOrderCount: sellCount,
	}, nil
}

// printRecoveryReport prints a formatted diagnostic report
func (rm *RecoveryManager) printRecoveryReport(report *RecoveryReport) {
	log.Println("\n" + "═══════════════════════════════════════════════════════════════")
	log.Println("                    RECOVERY DIAGNOSTIC REPORT                     ")
	log.Println("═══════════════════════════════════════════════════════════════════")

	log.Printf("Start Time:        %s", report.StartTime.Format("2006-01-02 15:04:05.000"))
	log.Printf("End Time:          %s", report.EndTime.Format("2006-01-02 15:04:05.000"))
	log.Printf("Total Duration:    %v", report.Duration)
	log.Printf("Recovery Method:   %s", report.RecoveryMethod)

	log.Println("\n--- SNAPSHOT INFORMATION ---")
	if report.SnapshotUsed {
		log.Printf("Snapshot Used:     YES (ID: %d)", report.SnapshotID)
		log.Printf("Snapshot Time:     %s", report.SnapshotTimestamp.Format("2006-01-02 15:04:05"))
		log.Printf("Snapshot Age:      %v", report.EventsSinceSnapshot)
		log.Printf("Bid Levels:        %d", report.SnapshotBidLevels)
		log.Printf("Ask Levels:        %d", report.SnapshotAskLevels)
		log.Printf("Load Time:         %v", report.SnapshotLoadTime)
	} else {
		log.Println("Snapshot Used:     NO (full replay)")
	}

	log.Println("\n--- ORDER REPLAY ---")
	log.Printf("Orders Restored:   %d", report.OrdersRestored)
	log.Printf("Replay Time:       %v", report.EventReplayTime)

	log.Println("\n--- BEFORE RECOVERY (Empty Orderbook) ---")
	log.Printf("Bid Levels:        %d", report.BeforeBidLevels)
	log.Printf("Ask Levels:        %d", report.BeforeAskLevels)
	log.Printf("Bid Volume:        %s", report.BeforeTotalBidVolume)
	log.Printf("Ask Volume:        %s", report.BeforeTotalAskVolume)

	log.Println("\n--- AFTER RECOVERY ---")
	log.Printf("Bid Levels:        %d", report.AfterBidLevels)
	log.Printf("Ask Levels:        %d", report.AfterAskLevels)
	log.Printf("Bid Volume:        %s", report.AfterTotalBidVolume)
	log.Printf("Ask Volume:        %s", report.AfterTotalAskVolume)

	if report.AfterBestBid != nil {
		log.Printf("Best Bid:          %s", report.AfterBestBid)
	} else {
		log.Println("Best Bid:          N/A (no bids)")
	}

	if report.AfterBestAsk != nil {
		log.Printf("Best Ask:          %s", report.AfterBestAsk)
	} else {
		log.Println("Best Ask:          N/A (no asks)")
	}

	if report.AfterSpread != nil {
		log.Printf("Spread:            %s", report.AfterSpread)
	} else {
		log.Println("Spread:            N/A")
	}

	log.Println("\n--- CHANGES (Before → After) ---")
	bidLevelChange := report.AfterBidLevels - report.BeforeBidLevels
	askLevelChange := report.AfterAskLevels - report.BeforeAskLevels
	bidVolumeChange := report.AfterTotalBidVolume.Sub(report.BeforeTotalBidVolume)
	askVolumeChange := report.AfterTotalAskVolume.Sub(report.BeforeTotalAskVolume)

	log.Printf("Bid Levels:        %+d", bidLevelChange)
	log.Printf("Ask Levels:        %+d", askLevelChange)
	log.Printf("Bid Volume:        %+s", bidVolumeChange)
	log.Printf("Ask Volume:        %+s", askVolumeChange)

	log.Println("\n--- VALIDATION ---")
	if report.ValidationPassed {
		log.Println("Status:            PASSED")
	} else {
		log.Printf("Status:            FAILED (%d errors)", len(report.ValidationErrors))
		for i, err := range report.ValidationErrors {
			log.Printf("  Error %d:         %s", i+1, err)
		}
	}
	log.Printf("Validation Time:   %v", report.ValidationTime)

	log.Println("\n--- PERFORMANCE BREAKDOWN ---")
	log.Printf("Snapshot Load:     %v (%.1f%%)", report.SnapshotLoadTime,
		float64(report.SnapshotLoadTime)/float64(report.Duration)*100)
	log.Printf("Event Replay:      %v (%.1f%%)", report.EventReplayTime,
		float64(report.EventReplayTime)/float64(report.Duration)*100)
	log.Printf("Validation:        %v (%.1f%%)", report.ValidationTime,
		float64(report.ValidationTime)/float64(report.Duration)*100)

	log.Println("═══════════════════════════════════════════════════════════════")
}

// QuickRecoveryCheck performs a fast validation without full recovery
func (rm *RecoveryManager) QuickRecoveryCheck(ctx context.Context) error {
	// Check database connectivity
	if err := rm.db.PingContext(ctx); err != nil {
		return fmt.Errorf("database not available: %w", err)
	}

	// Check if we have data to recover
	totals, err := rm.getDatabaseTotals(ctx)
	if err != nil {
		return fmt.Errorf("failed to query database totals: %w", err)
	}

	log.Printf(">Database check: %d open orders, bid_volume=%s, ask_volume=%s",
		totals.OpenOrderCount, totals.TotalBidVolume, totals.TotalAskVolume)

	// Check for latest snapshot
	snapshot, err := rm.snapshotManager.GetLatestSnapshot(ctx)
	if err == nil && snapshot != nil {
		age := time.Since(snapshot.SnapshotTimestamp)
		log.Printf("📸 Latest snapshot: ID=%d, age=%v", snapshot.SnapshotID, age)

		if age > 24*time.Hour {
			log.Printf(" Warning: Snapshot is %v old", age)
		}
	} else {
		log.Println("📸 No snapshot available")
	}

	return nil
}
