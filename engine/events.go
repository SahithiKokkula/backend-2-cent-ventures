package engine

import (
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type EventType string

const (
	EventTypeNewTrade        EventType = "NewTrade"
	EventTypeOrderPlaced     EventType = "OrderPlaced"
	EventTypeOrderFilled     EventType = "OrderFilled"
	EventTypeOrderCancelled  EventType = "OrderCancelled"
	EventTypeOrderbookChange EventType = "OrderbookChange"
)

type Event struct {
	Type      EventType
	Timestamp time.Time
	Data      interface{}
}

type NewTradeEvent struct {
	TradeID     uuid.UUID
	Instrument  string
	BuyOrderID  uuid.UUID
	SellOrderID uuid.UUID
	Price       decimal.Decimal
	Quantity    decimal.Decimal
	Timestamp   time.Time
}

type OrderEvent struct {
	OrderID           uuid.UUID
	ClientID          string
	Instrument        string
	Side              string
	Type              string
	Status            string
	Price             decimal.Decimal
	Quantity          decimal.Decimal
	FilledQuantity    decimal.Decimal
	RemainingQuantity decimal.Decimal
	Timestamp         time.Time
}

type OrderbookChangeEvent struct {
	Instrument string
	Side       string
	Action     string
	Price      decimal.Decimal
	NewSize    decimal.Decimal
	OldSize    decimal.Decimal
	Timestamp  time.Time
}

type EventListener func(event Event)

type EventBus struct {
	listeners map[EventType][]EventListener
	mu        sync.RWMutex
}

func NewEventBus() *EventBus {
	return &EventBus{
		listeners: make(map[EventType][]EventListener),
	}
}

func (eb *EventBus) Subscribe(eventType EventType, listener EventListener) {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	if eb.listeners[eventType] == nil {
		eb.listeners[eventType] = make([]EventListener, 0)
	}
	eb.listeners[eventType] = append(eb.listeners[eventType], listener)
}

func (eb *EventBus) Publish(event Event) {
	eb.mu.RLock()
	listeners := eb.listeners[event.Type]
	eb.mu.RUnlock()

	for _, listener := range listeners {
		go listener(event)
	}
}

// Unsubscribe removes all listeners for a specific event type
func (eb *EventBus) Unsubscribe(eventType EventType) {
	eb.mu.Lock()
	defer eb.mu.Unlock()
	delete(eb.listeners, eventType)
}

// GetListenerCount returns the number of listeners for an event type
func (eb *EventBus) GetListenerCount(eventType EventType) int {
	eb.mu.RLock()
	defer eb.mu.RUnlock()
	return len(eb.listeners[eventType])
}
