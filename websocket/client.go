package websocket

import (
	"encoding/json"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = (pongWait * 9) / 10
	maxMessageSize = 8192
	sendBufferSize = 256
)

type Client struct {
	id            string
	conn          *websocket.Conn
	hub           *Hub
	send          chan []byte
	subscriptions map[Topic]bool
	clientID      string
}

// NewClient creates a new WebSocket client
func NewClient(hub *Hub, conn *websocket.Conn) *Client {
	return &Client{
		id:            uuid.New().String(),
		conn:          conn,
		hub:           hub,
		send:          make(chan []byte, sendBufferSize),
		subscriptions: make(map[Topic]bool),
	}
}

func (c *Client) readPump() {
	defer func() {
		c.hub.Unregister(c)
		_ = c.conn.Close()
	}()

	_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetReadLimit(maxMessageSize)
	c.conn.SetPongHandler(func(string) error {
		_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WebSocket error: %v", err)
			}
			break
		}

		c.handleMessage(message)
	}
}

func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		_ = c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				// Hub closed the channel
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			_, _ = w.Write(message)

			// Add queued messages to the current WebSocket message
			n := len(c.send)
			for i := 0; i < n; i++ {
				_, _ = w.Write([]byte{'\n'})
				_, _ = w.Write(<-c.send)
			}

			if err := w.Close(); err != nil {
				return
			}

		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// handleMessage processes incoming messages from the client
func (c *Client) handleMessage(data []byte) {
	// Update activity timestamp whenever we receive a message
	c.hub.UpdateActivity(c)

	var msg ClientMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		log.Printf("Error unmarshaling client message: %v", err)
		c.sendError("Invalid message format")
		return
	}

	switch msg.Action {
	case "subscribe":
		c.handleSubscribe(msg.Topic)
	case "unsubscribe":
		c.handleUnsubscribe(msg.Topic)
	case "ping":
		c.sendPong()
	case "resync":
		// Client requesting fresh snapshot (if they missed messages)
		c.handleResync(msg.Topic)
	default:
		c.sendError("Unknown action: " + msg.Action)
	}
}

// handleResync sends a fresh snapshot to help client recover from missed messages
func (c *Client) handleResync(topicStr string) {
	topic := Topic(topicStr)

	// Validate topic
	validTopics := map[Topic]bool{
		TopicOrderbookDeltas: true,
		TopicTrades:          true,
		TopicOrders:          true,
	}

	if !validTopics[topic] {
		c.sendError("Invalid topic for resync: " + topicStr)
		return
	}

	// Check if client is subscribed to this topic
	if !c.subscriptions[topic] {
		c.sendError("Not subscribed to topic: " + topicStr)
		return
	}

	log.Printf("Client %s requested resync for %s", c.id, topic)

	// Send fresh snapshot
	c.sendSnapshot(topic)

	// Send acknowledgment
	ack := Message{
		Type:      "resynced",
		Topic:     topicStr,
		Timestamp: time.Now().Unix(),
		Data: map[string]interface{}{
			"message": "Fresh snapshot sent for " + topicStr,
		},
	}
	c.sendMessage(ack)
}

// handleSubscribe processes a subscription request
func (c *Client) handleSubscribe(topicStr string) {
	topic := Topic(topicStr)

	// Validate topic
	validTopics := map[Topic]bool{
		TopicOrderbookDeltas: true,
		TopicTrades:          true,
		TopicOrders:          true,
	}

	if !validTopics[topic] {
		c.sendError("Invalid topic: " + topicStr)
		return
	}

	// Subscribe to hub
	c.hub.Subscribe(c, topic)
	c.subscriptions[topic] = true

	// Send acknowledgment
	ack := Message{
		Type:      "subscribed",
		Topic:     topicStr,
		Timestamp: time.Now().Unix(),
		Data: map[string]interface{}{
			"message": "Successfully subscribed to " + topicStr,
		},
	}
	c.sendMessage(ack)

	// Send snapshot for the topic
	c.sendSnapshot(topic)
}

// handleUnsubscribe processes an unsubscription request
func (c *Client) handleUnsubscribe(topicStr string) {
	topic := Topic(topicStr)

	// Unsubscribe from hub
	c.hub.Unsubscribe(c, topic)
	delete(c.subscriptions, topic)

	// Send acknowledgment
	ack := Message{
		Type:      "unsubscribed",
		Topic:     topicStr,
		Timestamp: time.Now().Unix(),
		Data: map[string]interface{}{
			"message": "Successfully unsubscribed from " + topicStr,
		},
	}
	c.sendMessage(ack)
}

// sendSnapshot sends initial snapshot when subscribing to a topic
func (c *Client) sendSnapshot(topic Topic) {
	provider := c.hub.GetSnapshotProvider()
	if provider == nil {
		log.Printf("Snapshot provider not available")
		return
	}

	var snapshotData interface{}

	switch topic {
	case TopicOrderbookDeltas:
		// Send full orderbook snapshot
		snapshotData = provider.GetOrderbookSnapshot("BTC-USD", 20)
	case TopicTrades:
		// Send recent trades snapshot (last 50 trades)
		snapshotData = provider.GetTradesSnapshot("BTC-USD", 50)
	case TopicOrders:
		// Send orders snapshot for this client
		snapshotData = provider.GetOrdersSnapshot(c.clientID)
	default:
		log.Printf("Unknown topic for snapshot: %s", topic)
		return
	}

	snapshot := Message{
		Type:      "snapshot",
		Topic:     string(topic),
		Timestamp: time.Now().Unix(),
		Data:      snapshotData,
	}
	c.sendMessage(snapshot)
}

// sendMessage sends a message to the client
func (c *Client) sendMessage(msg Message) {
	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("Error marshaling message: %v", err)
		return
	}

	select {
	case c.send <- data:
	default:
		log.Printf("Client %s send buffer full, dropping message", c.id)
	}
}

// sendError sends an error message to the client
func (c *Client) sendError(errorMsg string) {
	msg := Message{
		Type:      "error",
		Timestamp: time.Now().Unix(),
		Data: map[string]interface{}{
			"error": errorMsg,
		},
	}
	c.sendMessage(msg)
}

// sendPong sends a pong response to the client
func (c *Client) sendPong() {
	msg := Message{
		Type:      "pong",
		Timestamp: time.Now().Unix(),
	}
	c.sendMessage(msg)
}

// Start begins the read and write pumps for this client
func (c *Client) Start() {
	// Send welcome message
	welcome := Message{
		Type:      "welcome",
		Timestamp: time.Now().Unix(),
		Data: map[string]interface{}{
			"client_id": c.id,
			"message":   "Connected to trading engine WebSocket",
			"topics":    []string{"orderbook_deltas", "trades", "orders"},
		},
	}
	c.sendMessage(welcome)

	// Start goroutines
	go c.writePump()
	go c.readPump()
}
