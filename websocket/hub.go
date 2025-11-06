package websocket

import (
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/shopspring/decimal"
)

// Topic represents a WebSocket subscription topic
type Topic string

const (
	TopicOrderbookDeltas Topic = "orderbook_deltas"
	TopicTrades          Topic = "trades"
	TopicOrders          Topic = "orders"
)

// Hub maintains the set of active clients and broadcasts messages to them
type Hub struct {
	// Registered clients mapped by client pointer
	clients map[*Client]bool

	// Client subscriptions: topic -> set of clients
	subscriptions map[Topic]map[*Client]bool

	// Inbound messages from clients
	register   chan *Client
	unregister chan *Client

	// Broadcast channels for different topics
	broadcastOrderbookDelta chan *OrderbookDeltaMessage
	broadcastTrade          chan *TradeMessage
	broadcastOrder          chan *OrderMessage

	// Batching for orderbook updates
	batchMutex    sync.Mutex
	pendingDeltas []*OrderbookDeltaMessage
	batchTimer    *time.Timer
	batchInterval time.Duration

	// Snapshot provider for initial data
	snapshotProvider *SnapshotProvider

	// Idle client cleanup
	idleCheckInterval time.Duration
	idleTimeout       time.Duration
	lastActivity      map[*Client]time.Time
	activityMutex     sync.RWMutex

	// Mutex for thread-safe operations
	mu sync.RWMutex
}

// NewHub creates a new Hub instance
func NewHub() *Hub {
	return &Hub{
		clients:                 make(map[*Client]bool),
		subscriptions:           make(map[Topic]map[*Client]bool),
		register:                make(chan *Client),
		unregister:              make(chan *Client),
		broadcastOrderbookDelta: make(chan *OrderbookDeltaMessage, 256),
		broadcastTrade:          make(chan *TradeMessage, 256),
		broadcastOrder:          make(chan *OrderMessage, 256),
		pendingDeltas:           make([]*OrderbookDeltaMessage, 0, 100),
		batchInterval:           100 * time.Millisecond, // Batch orderbook updates every 100ms
		idleCheckInterval:       30 * time.Second,       // Check for idle clients every 30s
		idleTimeout:             5 * time.Minute,        // Disconnect clients idle for 5+ minutes
		lastActivity:            make(map[*Client]time.Time),
	}
}

// Run starts the hub's main event loop
func (h *Hub) Run() {
	log.Println("WebSocket Hub started")

	// Start idle client cleanup goroutine
	go h.cleanupIdleClients()

	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			h.mu.Unlock()

			h.activityMutex.Lock()
			h.lastActivity[client] = time.Now()
			h.activityMutex.Unlock()

			log.Printf("Client registered: %s (total clients: %d)", client.id, len(h.clients))

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				// Remove from all subscriptions
				for topic := range h.subscriptions {
					delete(h.subscriptions[topic], client)
				}
				delete(h.clients, client)
				close(client.send)
				log.Printf("Client unregistered: %s (total clients: %d)", client.id, len(h.clients))
			}
			h.mu.Unlock()

			h.activityMutex.Lock()
			delete(h.lastActivity, client)
			h.activityMutex.Unlock()

		case delta := <-h.broadcastOrderbookDelta:
			// Batch orderbook deltas to reduce message frequency
			h.batchMutex.Lock()
			h.pendingDeltas = append(h.pendingDeltas, delta)

			// Start or reset batch timer
			if h.batchTimer == nil {
				h.batchTimer = time.AfterFunc(h.batchInterval, func() {
					h.flushOrderbookDeltas()
				})
			}
			h.batchMutex.Unlock()

		case trade := <-h.broadcastTrade:
			// Broadcast trades immediately (high value, less frequent)
			message := Message{
				Type:      "trade",
				Topic:     string(TopicTrades),
				Data:      trade,
				Timestamp: time.Now().Unix(),
			}
			h.broadcastToTopic(TopicTrades, message)

		case order := <-h.broadcastOrder:
			// Broadcast order updates immediately
			message := Message{
				Type:      "order",
				Topic:     string(TopicOrders),
				Data:      order,
				Timestamp: time.Now().Unix(),
			}
			h.broadcastToTopic(TopicOrders, message)
		}
	}
}

// flushOrderbookDeltas sends all pending orderbook deltas as a batch
func (h *Hub) flushOrderbookDeltas() {
	h.batchMutex.Lock()
	defer h.batchMutex.Unlock()

	if len(h.pendingDeltas) == 0 {
		h.batchTimer = nil
		return
	}

	// Create batch message
	message := Message{
		Type:      "orderbook_delta_batch",
		Topic:     string(TopicOrderbookDeltas),
		Data:      h.pendingDeltas,
		Timestamp: time.Now().Unix(),
	}

	h.broadcastToTopic(TopicOrderbookDeltas, message)

	// Clear pending deltas
	h.pendingDeltas = h.pendingDeltas[:0]
	h.batchTimer = nil
}

// broadcastToTopic sends a message to all clients subscribed to a topic
func (h *Hub) broadcastToTopic(topic Topic, message Message) {
	h.mu.RLock()
	subscribers, exists := h.subscriptions[topic]
	h.mu.RUnlock()

	if !exists || len(subscribers) == 0 {
		return
	}

	// Marshal once, send to all subscribers
	data, err := json.Marshal(message)
	if err != nil {
		log.Printf("Error marshaling message: %v", err)
		return
	}

	h.mu.RLock()
	defer h.mu.RUnlock()

	sentCount := 0
	for client := range subscribers {
		select {
		case client.send <- data:
			sentCount++
		default:
			// Client's send buffer is full, skip
			log.Printf("Client %s send buffer full, skipping message", client.id)
		}
	}

	if sentCount > 0 {
		log.Printf("Broadcasted %s message to %d clients", topic, sentCount)
	}
}

// Subscribe adds a client to a topic subscription
func (h *Hub) Subscribe(client *Client, topic Topic) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.subscriptions[topic] == nil {
		h.subscriptions[topic] = make(map[*Client]bool)
	}
	h.subscriptions[topic][client] = true

	log.Printf("Client %s subscribed to %s (subscribers: %d)",
		client.id, topic, len(h.subscriptions[topic]))
}

// Unsubscribe removes a client from a topic subscription
func (h *Hub) Unsubscribe(client *Client, topic Topic) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if subscribers, exists := h.subscriptions[topic]; exists {
		delete(subscribers, client)
		log.Printf("Client %s unsubscribed from %s (subscribers: %d)",
			client.id, topic, len(subscribers))
	}
}

// BroadcastOrderbookDelta sends an orderbook delta to subscribed clients
func (h *Hub) BroadcastOrderbookDelta(instrument string, side string, price, size decimal.Decimal) {
	delta := &OrderbookDeltaMessage{
		Instrument: instrument,
		Side:       side,
		Price:      price,
		Size:       size,
		Timestamp:  time.Now().UnixMilli(),
	}

	select {
	case h.broadcastOrderbookDelta <- delta:
	default:
		log.Println("Warning: Orderbook delta channel full, dropping message")
	}
}

// BroadcastTrade sends a trade notification to subscribed clients
func (h *Hub) BroadcastTrade(tradeID string, instrument string, buyOrderID, sellOrderID string,
	price, quantity decimal.Decimal, timestamp int64) {

	trade := &TradeMessage{
		TradeID:     tradeID,
		Instrument:  instrument,
		BuyOrderID:  buyOrderID,
		SellOrderID: sellOrderID,
		Price:       price,
		Quantity:    quantity,
		Timestamp:   timestamp,
	}

	select {
	case h.broadcastTrade <- trade:
	default:
		log.Println("Warning: Trade channel full, dropping message")
	}
}

// BroadcastOrder sends an order update to subscribed clients
func (h *Hub) BroadcastOrder(orderID, clientID, instrument, side, orderType, status string,
	price, quantity, filledQuantity, remainingQuantity decimal.Decimal, timestamp int64) {

	order := &OrderMessage{
		OrderID:           orderID,
		ClientID:          clientID,
		Instrument:        instrument,
		Side:              side,
		Type:              orderType,
		Status:            status,
		Price:             price,
		Quantity:          quantity,
		FilledQuantity:    filledQuantity,
		RemainingQuantity: remainingQuantity,
		Timestamp:         timestamp,
	}

	select {
	case h.broadcastOrder <- order:
	default:
		log.Println("Warning: Order channel full, dropping message")
	}
}

// GetStats returns hub statistics
func (h *Hub) GetStats() map[string]interface{} {
	h.mu.RLock()
	defer h.mu.RUnlock()

	stats := map[string]interface{}{
		"total_clients": len(h.clients),
		"subscriptions": make(map[string]int),
	}

	for topic, subscribers := range h.subscriptions {
		stats["subscriptions"].(map[string]int)[string(topic)] = len(subscribers)
	}

	return stats
}

// SetSnapshotProvider sets the snapshot provider for the hub
func (h *Hub) SetSnapshotProvider(provider *SnapshotProvider) {
	h.snapshotProvider = provider
}

// GetSnapshotProvider returns the snapshot provider
func (h *Hub) GetSnapshotProvider() *SnapshotProvider {
	return h.snapshotProvider
}

// Register adds a client to the hub
func (h *Hub) Register(client *Client) {
	h.register <- client
}

// Unregister removes a client from the hub
func (h *Hub) Unregister(client *Client) {
	h.unregister <- client
}

// UpdateActivity updates the last activity time for a client
func (h *Hub) UpdateActivity(client *Client) {
	h.activityMutex.Lock()
	h.lastActivity[client] = time.Now()
	h.activityMutex.Unlock()
}

// cleanupIdleClients periodically checks for and disconnects idle clients
func (h *Hub) cleanupIdleClients() {
	ticker := time.NewTicker(h.idleCheckInterval)
	defer ticker.Stop()

	for range ticker.C {
		now := time.Now()
		var idleClients []*Client

		h.activityMutex.RLock()
		h.mu.RLock()
		for client := range h.clients {
			if lastActive, exists := h.lastActivity[client]; exists {
				if now.Sub(lastActive) > h.idleTimeout {
					idleClients = append(idleClients, client)
				}
			}
		}
		h.mu.RUnlock()
		h.activityMutex.RUnlock()

		// Disconnect idle clients
		for _, client := range idleClients {
			log.Printf("Disconnecting idle client: %s (idle for %v)", client.id, h.idleTimeout)
			_ = client.conn.Close() // This will trigger unregister in readPump
		}

		if len(idleClients) > 0 {
			log.Printf("Cleaned up %d idle clients", len(idleClients))
		}
	}
}
