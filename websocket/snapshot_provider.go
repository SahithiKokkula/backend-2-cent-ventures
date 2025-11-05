package websocket

import (
	"context"
	"database/sql"
	"log"
	"time"

	"github.com/yourusername/trading-engine/engine"
	"github.com/yourusername/trading-engine/persistence"
)

type SnapshotProvider struct {
	matchingEngine *engine.MatchingEngine
	db             *sql.DB
	postgresStore  *persistence.PostgresStore
}

func NewSnapshotProvider(me *engine.MatchingEngine, db *sql.DB) *SnapshotProvider {
	var store *persistence.PostgresStore
	if db != nil {
		store = persistence.NewPostgresStore(db)
	}

	return &SnapshotProvider{
		matchingEngine: me,
		db:             db,
		postgresStore:  store,
	}
}

func (sp *SnapshotProvider) GetOrderbookSnapshot(instrument string, levels int) *OrderbookSnapshot {
	orderbook := sp.matchingEngine.GetOrderBook()

	bids, asks := orderbook.GetTopLevels(levels)

	snapshot := &OrderbookSnapshot{
		Instrument: instrument,
		Bids:       make([]OrderbookLevel, 0, len(bids)),
		Asks:       make([]OrderbookLevel, 0, len(asks)),
		Timestamp:  time.Now().UnixMilli(),
	}

	for _, bid := range bids {
		snapshot.Bids = append(snapshot.Bids, OrderbookLevel{
			Price: bid.Price,
			Size:  bid.Volume,
		})
	}

	for _, ask := range asks {
		snapshot.Asks = append(snapshot.Asks, OrderbookLevel{
			Price: ask.Price,
			Size:  ask.Volume,
		})
	}

	return snapshot
}

func (sp *SnapshotProvider) GetTradesSnapshot(instrument string, limit int) *TradesSnapshot {
	if sp.postgresStore == nil {
		return &TradesSnapshot{
			Instrument: instrument,
			Trades:     []TradeMessage{},
			Count:      0,
			Timestamp:  time.Now().UnixMilli(),
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	trades, err := sp.postgresStore.GetTradesByInstrument(ctx, instrument, limit)
	if err != nil {
		log.Printf("Error fetching trades snapshot: %v", err)
		return &TradesSnapshot{
			Instrument: instrument,
			Trades:     []TradeMessage{},
			Count:      0,
			Timestamp:  time.Now().UnixMilli(),
		}
	}

	tradeMessages := make([]TradeMessage, 0, len(trades))
	for _, trade := range trades {
		tradeMessages = append(tradeMessages, TradeMessage{
			TradeID:     trade.TradeID.String(),
			Instrument:  trade.Instrument,
			BuyOrderID:  trade.BuyOrderID.String(),
			SellOrderID: trade.SellOrderID.String(),
			Price:       trade.Price,
			Quantity:    trade.Quantity,
			Timestamp:   trade.Timestamp.UnixMilli(),
		})
	}

	return &TradesSnapshot{
		Instrument: instrument,
		Trades:     tradeMessages,
		Count:      len(tradeMessages),
		Timestamp:  time.Now().UnixMilli(),
	}
}

// GetOrdersSnapshot returns orders for a specific client
func (sp *SnapshotProvider) GetOrdersSnapshot(clientID string) *OrdersSnapshot {
	// For now, return empty snapshot
	// This can be enhanced to query orders from database or in-memory store
	return &OrdersSnapshot{
		ClientID:  clientID,
		Orders:    []OrderMessage{},
		Count:     0,
		Timestamp: time.Now().UnixMilli(),
	}
}
