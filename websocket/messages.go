package websocket

import (
	"github.com/shopspring/decimal"
)

type Message struct {
	Type      string      `json:"type"`
	Topic     string      `json:"topic,omitempty"`
	Timestamp int64       `json:"timestamp"`
	Data      interface{} `json:"data,omitempty"`
}

type ClientMessage struct {
	Action string `json:"action"`
	Topic  string `json:"topic,omitempty"`
}

type OrderbookDeltaMessage struct {
	Instrument string          `json:"instrument"`
	Side       string          `json:"side"`
	Price      decimal.Decimal `json:"price"`
	Size       decimal.Decimal `json:"size"`
	Timestamp  int64           `json:"timestamp"`
}

type TradeMessage struct {
	TradeID     string          `json:"trade_id"`
	Instrument  string          `json:"instrument"`
	BuyOrderID  string          `json:"buy_order_id"`
	SellOrderID string          `json:"sell_order_id"`
	Price       decimal.Decimal `json:"price"`
	Quantity    decimal.Decimal `json:"quantity"`
	Timestamp   int64           `json:"timestamp"`
}

type OrderMessage struct {
	OrderID           string          `json:"order_id"`
	ClientID          string          `json:"client_id"`
	Instrument        string          `json:"instrument"`
	Side              string          `json:"side"`
	Type              string          `json:"type"`
	Status            string          `json:"status"`
	Price             decimal.Decimal `json:"price"`
	Quantity          decimal.Decimal `json:"quantity"`
	FilledQuantity    decimal.Decimal `json:"filled_quantity"`
	RemainingQuantity decimal.Decimal `json:"remaining_quantity"`
	Timestamp         int64           `json:"timestamp"`
}

type OrderbookSnapshot struct {
	Instrument string           `json:"instrument"`
	Bids       []OrderbookLevel `json:"bids"`
	Asks       []OrderbookLevel `json:"asks"`
	Timestamp  int64            `json:"timestamp"`
}

type OrderbookLevel struct {
	Price decimal.Decimal `json:"price"`
	Size  decimal.Decimal `json:"size"`
}

type TradesSnapshot struct {
	Instrument string         `json:"instrument"`
	Trades     []TradeMessage `json:"trades"`
	Count      int            `json:"count"`
	Timestamp  int64          `json:"timestamp"`
}

type OrdersSnapshot struct {
	ClientID  string         `json:"client_id,omitempty"`
	Orders    []OrderMessage `json:"orders"`
	Count     int            `json:"count"`
	Timestamp int64          `json:"timestamp"`
}
