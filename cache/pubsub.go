package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"
)

type PubSubManager struct {
	redis          *RedisCache
	keyBuilder     *KeyBuilder
	orderbookCache *OrderbookCache
	tradesCache    *TradesCache
	subscribers    map[string]*redis.PubSub
	handlers       map[string]MessageHandler
	ctx            context.Context
	cancel         context.CancelFunc
	wg             sync.WaitGroup
	mu             sync.RWMutex
}

type MessageHandler func(channel string, message *CacheInvalidationMessage) error

func NewPubSubManager(redisCache *RedisCache, orderbookCache *OrderbookCache, tradesCache *TradesCache) *PubSubManager {
	ctx, cancel := context.WithCancel(context.Background())

	return &PubSubManager{
		redis:          redisCache,
		keyBuilder:     NewKeyBuilder("trading"),
		orderbookCache: orderbookCache,
		tradesCache:    tradesCache,
		subscribers:    make(map[string]*redis.PubSub),
		handlers:       make(map[string]MessageHandler),
		ctx:            ctx,
		cancel:         cancel,
	}
}

type CacheInvalidationMessage struct {
	Type      string                 `json:"type"`
	Action    string                 `json:"action"`
	Symbol    string                 `json:"symbol"`
	Timestamp time.Time              `json:"timestamp"`
	Data      map[string]interface{} `json:"data,omitempty"`
}

const (
	OrderbookInvalidationChannel = "orderbook:invalidation"
	TradesInvalidationChannel    = "trades:invalidation"
	OrderInvalidationChannel     = "order:invalidation"
	GlobalInvalidationChannel    = "cache:invalidation"
)

func (psm *PubSubManager) Start() error {
	psm.mu.Lock()
	defer psm.mu.Unlock()

	if err := psm.subscribeToChannel(OrderbookInvalidationChannel, psm.handleOrderbookInvalidation); err != nil {
		return fmt.Errorf("failed to subscribe to orderbook channel: %w", err)
	}

	if err := psm.subscribeToChannel(TradesInvalidationChannel, psm.handleTradesInvalidation); err != nil {
		return fmt.Errorf("failed to subscribe to trades channel: %w", err)
	}

	if err := psm.subscribeToChannel(GlobalInvalidationChannel, psm.handleGlobalInvalidation); err != nil {
		return fmt.Errorf("failed to subscribe to global channel: %w", err)
	}

	log.Println("PubSubManager started and subscribed to invalidation channels")
	return nil
}

func (psm *PubSubManager) Stop() error {
	psm.mu.Lock()
	defer psm.mu.Unlock()

	psm.cancel()

	for channel, pubsub := range psm.subscribers {
		if err := pubsub.Close(); err != nil {
			log.Printf("Warning: failed to close subscription for channel %s: %v", channel, err)
		}
	}

	// Wait for all goroutines to finish
	psm.wg.Wait()

	log.Println("PubSubManager stopped")
	return nil
}

// subscribeToChannel subscribes to a Redis pub/sub channel
func (psm *PubSubManager) subscribeToChannel(channel string, handler MessageHandler) error {
	client := psm.redis.GetClient()
	pubsub := client.Subscribe(psm.ctx, channel)

	// Wait for confirmation
	_, err := pubsub.Receive(psm.ctx)
	if err != nil {
		return fmt.Errorf("failed to receive subscription confirmation: %w", err)
	}

	psm.subscribers[channel] = pubsub
	psm.handlers[channel] = handler

	// Start listening goroutine
	psm.wg.Add(1)
	go psm.listen(channel, pubsub, handler)

	return nil
}

// listen listens for messages on a pub/sub channel
func (psm *PubSubManager) listen(channel string, pubsub *redis.PubSub, handler MessageHandler) {
	defer psm.wg.Done()

	ch := pubsub.Channel()

	for {
		select {
		case <-psm.ctx.Done():
			log.Printf("Stopping listener for channel: %s", channel)
			return

		case msg, ok := <-ch:
			if !ok {
				log.Printf("Channel closed for: %s", channel)
				return
			}

			// Parse message
			var invalidationMsg CacheInvalidationMessage
			if err := json.Unmarshal([]byte(msg.Payload), &invalidationMsg); err != nil {
				log.Printf("Failed to parse invalidation message from %s: %v", channel, err)
				continue
			}

			// Handle message
			if err := handler(channel, &invalidationMsg); err != nil {
				log.Printf("Failed to handle invalidation message from %s: %v", channel, err)
			}
		}
	}
}

// handleOrderbookInvalidation handles orderbook invalidation messages
func (psm *PubSubManager) handleOrderbookInvalidation(channel string, msg *CacheInvalidationMessage) error {
	switch msg.Action {
	case "invalidate":
		log.Printf("Invalidating orderbook cache for symbol: %s", msg.Symbol)
		return psm.orderbookCache.InvalidateOrderbook(msg.Symbol)

	case "update":
		// For update, we invalidate to force re-fetch
		log.Printf("Invalidating orderbook cache due to update: %s", msg.Symbol)
		return psm.orderbookCache.InvalidateOrderbook(msg.Symbol)

	case "delete":
		log.Printf("Deleting orderbook cache for symbol: %s", msg.Symbol)
		return psm.orderbookCache.InvalidateOrderbook(msg.Symbol)

	default:
		return fmt.Errorf("unknown action: %s", msg.Action)
	}
}

// handleTradesInvalidation handles trades invalidation messages
func (psm *PubSubManager) handleTradesInvalidation(channel string, msg *CacheInvalidationMessage) error {
	switch msg.Action {
	case "invalidate":
		log.Printf("Invalidating trades cache for symbol: %s", msg.Symbol)
		return psm.tradesCache.InvalidateTrades(msg.Symbol)

	case "update":
		// For update with new trade data, we could append to cache
		log.Printf("Invalidating trades cache due to update: %s", msg.Symbol)
		return psm.tradesCache.InvalidateTrades(msg.Symbol)

	case "delete":
		log.Printf("Deleting trades cache for symbol: %s", msg.Symbol)
		return psm.tradesCache.InvalidateTrades(msg.Symbol)

	default:
		return fmt.Errorf("unknown action: %s", msg.Action)
	}
}

// handleGlobalInvalidation handles global invalidation messages
func (psm *PubSubManager) handleGlobalInvalidation(channel string, msg *CacheInvalidationMessage) error {
	switch msg.Action {
	case "invalidate_all":
		log.Println("Invalidating all caches")

		if err := psm.orderbookCache.InvalidateAllOrderbooks(); err != nil {
			log.Printf("Failed to invalidate orderbook caches: %v", err)
		}

		if err := psm.tradesCache.InvalidateAllTrades(); err != nil {
			log.Printf("Failed to invalidate trades caches: %v", err)
		}

		return nil

	case "flush_all":
		log.Println("Flushing entire Redis database")
		return psm.redis.FlushDB()

	default:
		return fmt.Errorf("unknown global action: %s", msg.Action)
	}
}

// PublishOrderbookInvalidation publishes an orderbook invalidation message
func (psm *PubSubManager) PublishOrderbookInvalidation(symbol string, action string) error {
	msg := &CacheInvalidationMessage{
		Type:      "orderbook",
		Action:    action,
		Symbol:    symbol,
		Timestamp: time.Now(),
	}

	return psm.publishMessage(OrderbookInvalidationChannel, msg)
}

// PublishTradesInvalidation publishes a trades invalidation message
func (psm *PubSubManager) PublishTradesInvalidation(symbol string, action string) error {
	msg := &CacheInvalidationMessage{
		Type:      "trades",
		Action:    action,
		Symbol:    symbol,
		Timestamp: time.Now(),
	}

	return psm.publishMessage(TradesInvalidationChannel, msg)
}

// PublishGlobalInvalidation publishes a global invalidation message
func (psm *PubSubManager) PublishGlobalInvalidation(action string) error {
	msg := &CacheInvalidationMessage{
		Type:      "global",
		Action:    action,
		Timestamp: time.Now(),
	}

	return psm.publishMessage(GlobalInvalidationChannel, msg)
}

// publishMessage publishes a message to a channel
func (psm *PubSubManager) publishMessage(channel string, msg *CacheInvalidationMessage) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal invalidation message: %w", err)
	}

	client := psm.redis.GetClient()
	err = client.Publish(psm.ctx, channel, data).Err()
	if err != nil {
		return fmt.Errorf("failed to publish message to channel %s: %w", channel, err)
	}

	return nil
}

// SubscribeToCustomChannel subscribes to a custom channel with a handler
func (psm *PubSubManager) SubscribeToCustomChannel(channel string, handler MessageHandler) error {
	psm.mu.Lock()
	defer psm.mu.Unlock()

	// Check if already subscribed
	if _, exists := psm.subscribers[channel]; exists {
		return fmt.Errorf("already subscribed to channel: %s", channel)
	}

	return psm.subscribeToChannel(channel, handler)
}

// UnsubscribeFromChannel unsubscribes from a channel
func (psm *PubSubManager) UnsubscribeFromChannel(channel string) error {
	psm.mu.Lock()
	defer psm.mu.Unlock()

	pubsub, exists := psm.subscribers[channel]
	if !exists {
		return fmt.Errorf("not subscribed to channel: %s", channel)
	}

	if err := pubsub.Close(); err != nil {
		return fmt.Errorf("failed to close subscription: %w", err)
	}

	delete(psm.subscribers, channel)
	delete(psm.handlers, channel)

	return nil
}

// GetSubscribedChannels returns list of subscribed channels
func (psm *PubSubManager) GetSubscribedChannels() []string {
	psm.mu.RLock()
	defer psm.mu.RUnlock()

	channels := make([]string, 0, len(psm.subscribers))
	for channel := range psm.subscribers {
		channels = append(channels, channel)
	}

	return channels
}

// CacheInvalidator provides a simple interface for invalidating caches
type CacheInvalidator struct {
	pubsub *PubSubManager
}

// NewCacheInvalidator creates a new cache invalidator
func NewCacheInvalidator(pubsub *PubSubManager) *CacheInvalidator {
	return &CacheInvalidator{
		pubsub: pubsub,
	}
}

// InvalidateOrderbook invalidates orderbook cache for a symbol
func (ci *CacheInvalidator) InvalidateOrderbook(symbol string) error {
	return ci.pubsub.PublishOrderbookInvalidation(symbol, "invalidate")
}

// InvalidateTrades invalidates trades cache for a symbol
func (ci *CacheInvalidator) InvalidateTrades(symbol string) error {
	return ci.pubsub.PublishTradesInvalidation(symbol, "invalidate")
}

// InvalidateAll invalidates all caches
func (ci *CacheInvalidator) InvalidateAll() error {
	return ci.pubsub.PublishGlobalInvalidation("invalidate_all")
}

// NotifyOrderbookUpdate notifies about an orderbook update
func (ci *CacheInvalidator) NotifyOrderbookUpdate(symbol string) error {
	return ci.pubsub.PublishOrderbookInvalidation(symbol, "update")
}

// NotifyNewTrade notifies about a new trade
func (ci *CacheInvalidator) NotifyNewTrade(symbol string) error {
	return ci.pubsub.PublishTradesInvalidation(symbol, "update")
}

// BulkInvalidateOrderbooks invalidates multiple orderbook caches
func (ci *CacheInvalidator) BulkInvalidateOrderbooks(symbols []string) error {
	for _, symbol := range symbols {
		if err := ci.InvalidateOrderbook(symbol); err != nil {
			return fmt.Errorf("failed to invalidate orderbook for %s: %w", symbol, err)
		}
	}
	return nil
}

// BulkInvalidateTrades invalidates multiple trades caches
func (ci *CacheInvalidator) BulkInvalidateTrades(symbols []string) error {
	for _, symbol := range symbols {
		if err := ci.InvalidateTrades(symbol); err != nil {
			return fmt.Errorf("failed to invalidate trades for %s: %w", symbol, err)
		}
	}
	return nil
}

// Example usage in matching engine:
/*
// When an order is matched and orderbook changes:
func (me *MatchingEngine) OnOrderMatched(symbol string) {
    // Update orderbook in database/memory
    // ...

    // Invalidate cache via pub/sub
    if me.cacheInvalidator != nil {
        me.cacheInvalidator.NotifyOrderbookUpdate(symbol)
    }
}

// When a new trade is created:
func (me *MatchingEngine) OnTradeCreated(symbol string, trade *Trade) {
    // Store trade in database
    // ...

    // Invalidate cache via pub/sub
    if me.cacheInvalidator != nil {
        me.cacheInvalidator.NotifyNewTrade(symbol)
    }
}
*/
