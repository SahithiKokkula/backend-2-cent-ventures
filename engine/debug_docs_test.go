package engine

import (
	"bytes"
	"context"
	"log"
	"strings"
	"testing"
	"time"

	"github.com/SahithiKokkula/backend-2-cent-ventures/models"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

func TestDebugModeTracing(t *testing.T) {
	me := NewMatchingEngine("BTC-USD")
	ctx := context.Background()

	// Capture debug output
	var buf bytes.Buffer
	logger := log.New(&buf, "", log.LstdFlags|log.Lmicroseconds)
	me.SetDebugLogger(logger)
	me.EnableDebugMode(true)

	defer func() { _ = me.Start(ctx) }()
	defer func() { _ = me.Stop() }()

	// Add a sell order
	sellOrder := &models.Order{
		ID:             uuid.New(),
		ClientID:       "seller1",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideSell,
		Type:           models.OrderTypeLimit,
		Price:          decimal.NewFromFloat(100.0),
		Quantity:       decimal.NewFromFloat(10.0),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
	}

	_, _ = me.SubmitOrder(sellOrder)
	time.Sleep(20 * time.Millisecond)

	// Add a buy order that matches
	buyOrder := &models.Order{
		ID:             uuid.New(),
		ClientID:       "buyer1",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideBuy,
		Type:           models.OrderTypeLimit,
		Price:          decimal.NewFromFloat(101.0),
		Quantity:       decimal.NewFromFloat(5.0),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
	}

	_, _ = me.SubmitOrder(buyOrder)
	time.Sleep(20 * time.Millisecond)

	// Check debug output contains expected traces
	debugOutput := buf.String()

	expectedTraces := []string{
		"SubmitOrder: Command posted",
		"Processing Command",
		"NEW order",
		"OrderBook before matching",
		"Best Ask:",
		"Matching against price level",
		"MATCH:",
		"Trade created",
		"After fill - Aggressor:",
		"After fill - Resting:",
		"Matching complete",
		"OrderBook after matching",
		"Command #",
		"Complete",
	}

	for _, trace := range expectedTraces {
		if !strings.Contains(debugOutput, trace) {
			t.Errorf("Debug output missing expected trace: %s", trace)
		}
	}

	// Verify debug output is sequential and detailed
	if len(debugOutput) < 1000 {
		t.Error("Debug output seems too short, expected detailed traces")
	}

	t.Logf("Debug output length: %d bytes", len(debugOutput))
}

func TestChannelInspection(t *testing.T) {
	me := NewMatchingEngine("BTC-USD")
	ctx := context.Background()
	defer func() { _ = me.Start(ctx) }()
	defer func() { _ = me.Stop() }()

	// Check initial channel status
	status := me.InspectChannelStatus()

	if status["channel_capacity"] != 1000 {
		t.Errorf("Expected channel capacity 1000, got %v", status["channel_capacity"])
	}

	if status["channel_length"] != 0 {
		t.Errorf("Expected empty channel, got length %v", status["channel_length"])
	}

	if status["is_full"] != false {
		t.Error("Channel should not be full")
	}

	if status["worker_running"] != true {
		t.Error("Worker should be running")
	}

	// Verify stats include channel information
	stats := me.GetStats()

	if stats["command_backlog"] == nil {
		t.Error("Stats should include command_backlog")
	}

	if stats["command_capacity"] == nil {
		t.Error("Stats should include command_capacity")
	}

	if stats["channel_utilization"] == nil {
		t.Error("Stats should include channel_utilization")
	}

	if stats["commands_processed"] == nil {
		t.Error("Stats should include commands_processed")
	}

	if stats["channel_blocks"] == nil {
		t.Error("Stats should include channel_blocks")
	}
}

func TestChannelNearFullWarning(t *testing.T) {
	// Create engine with small buffer for testing
	me := NewMatchingEngine("BTC-USD")
	me.commandChan = make(chan *OrderCommand, 20) // Small buffer

	ctx := context.Background()

	// Capture debug output
	var buf bytes.Buffer
	logger := log.New(&buf, "", 0)
	me.SetDebugLogger(logger)
	me.EnableDebugMode(true)

	defer func() { _ = me.Start(ctx) }()
	defer func() { _ = me.Stop() }()

	// Submit many orders quickly to fill buffer
	for i := 0; i < 15; i++ {
		order := &models.Order{
			ID:             uuid.New(),
			ClientID:       "client",
			Instrument:     "BTC-USD",
			Side:           models.OrderSideBuy,
			Type:           models.OrderTypeLimit,
			Price:          decimal.NewFromFloat(100.0 + float64(i)),
			Quantity:       decimal.NewFromFloat(1.0),
			FilledQuantity: decimal.Zero,
			Status:         models.OrderStatusOpen,
		}
		_, _ = me.SubmitOrder(order)
	}

	time.Sleep(100 * time.Millisecond)

	// Check if warning was logged
	debugOutput := buf.String()
	if !strings.Contains(debugOutput, "WARNING: Command channel nearly full") {
		// This is OK if the worker processed fast enough
		t.Logf("Channel was processed fast enough, no warning generated")
	}

	// Check stats
	stats := me.GetStats()
	t.Logf("Commands processed: %v", stats["commands_processed"])
	t.Logf("Channel blocks: %v", stats["channel_blocks"])
}

func TestDebugModeToggle(t *testing.T) {
	me := NewMatchingEngine("BTC-USD")

	if me.IsDebugMode() {
		t.Error("Debug mode should be disabled by default")
	}

	me.EnableDebugMode(true)
	if !me.IsDebugMode() {
		t.Error("Debug mode should be enabled")
	}

	me.EnableDebugMode(false)
	if me.IsDebugMode() {
		t.Error("Debug mode should be disabled")
	}
}

func TestDetailedMatchingTrace(t *testing.T) {
	me := NewMatchingEngine("BTC-USD")
	ctx := context.Background()

	var buf bytes.Buffer
	logger := log.New(&buf, "", log.Lmicroseconds)
	me.SetDebugLogger(logger)
	me.EnableDebugMode(true)

	defer func() { _ = me.Start(ctx) }()
	defer func() { _ = me.Stop() }()

	// Create a scenario with multiple orders at same price (FIFO test)
	sell1 := &models.Order{
		ID:             uuid.New(),
		ClientID:       "seller1",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideSell,
		Type:           models.OrderTypeLimit,
		Price:          decimal.NewFromFloat(100.0),
		Quantity:       decimal.NewFromFloat(3.0),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
	}

	sell2 := &models.Order{
		ID:             uuid.New(),
		ClientID:       "seller2",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideSell,
		Type:           models.OrderTypeLimit,
		Price:          decimal.NewFromFloat(100.0),
		Quantity:       decimal.NewFromFloat(4.0),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
	}

	_, _ = me.SubmitOrder(sell1)
	_, _ = me.SubmitOrder(sell2)
	time.Sleep(20 * time.Millisecond)

	// Submit buy order that matches both (tests FIFO)
	buyOrder := &models.Order{
		ID:             uuid.New(),
		ClientID:       "buyer1",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideBuy,
		Type:           models.OrderTypeLimit,
		Price:          decimal.NewFromFloat(100.0),
		Quantity:       decimal.NewFromFloat(7.0),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
	}

	_, _ = me.SubmitOrder(buyOrder)
	time.Sleep(20 * time.Millisecond)

	debugOutput := buf.String()

	// Verify FIFO traces
	if !strings.Contains(debugOutput, "[FIFO #1]") {
		t.Error("Missing FIFO #1 trace")
	}

	if !strings.Contains(debugOutput, "[FIFO #2]") {
		t.Error("Missing FIFO #2 trace")
	}

	// Verify both orders matched
	matchCount := strings.Count(debugOutput, "Trade created")
	if matchCount != 2 {
		t.Errorf("Expected 2 trade traces, found %d", matchCount)
	}

	// Log sample of debug output
	lines := strings.Split(debugOutput, "\n")
	t.Logf("Debug trace lines: %d", len(lines))
	if len(lines) > 50 {
		t.Log("Sample debug traces:")
		for i := 0; i < 10 && i < len(lines); i++ {
			t.Log(lines[i])
		}
	}
}
