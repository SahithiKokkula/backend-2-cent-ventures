package persistence

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/SahithiKokkula/backend-2-cent-ventures/models"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// EventRetryQueue manages failed DB write retries
type EventRetryQueue struct {
	store         *OrderEventStore
	queue         chan *QueuedEvent
	failedEvents  []*QueuedEvent
	mu            sync.RWMutex
	maxRetries    int
	retryInterval time.Duration
	running       bool
	stopCh        chan struct{}
	wg            sync.WaitGroup
}

// QueuedEvent represents an event pending retry
type QueuedEvent struct {
	EventType      OrderEventType
	Order          *models.Order
	OrderID        uuid.UUID
	FillQuantity   decimal.Decimal
	FilledQuantity decimal.Decimal
	Status         models.OrderStatus
	TradeID        *uuid.UUID
	Reason         *string
	Timestamp      time.Time
	Attempts       int
	LastError      error
	CreatedAt      time.Time
}

// NewEventRetryQueue creates a new retry queue
func NewEventRetryQueue(store *OrderEventStore, queueSize int, maxRetries int, retryInterval time.Duration) *EventRetryQueue {
	return &EventRetryQueue{
		store:         store,
		queue:         make(chan *QueuedEvent, queueSize),
		failedEvents:  make([]*QueuedEvent, 0),
		maxRetries:    maxRetries,
		retryInterval: retryInterval,
		stopCh:        make(chan struct{}),
	}
}

// Start starts the retry queue processor
func (eq *EventRetryQueue) Start() {
	eq.mu.Lock()
	if eq.running {
		eq.mu.Unlock()
		return
	}
	eq.running = true
	eq.mu.Unlock()

	eq.wg.Add(1)
	go eq.processQueue()

	log.Println("Event retry queue started")
}

// Stop stops the retry queue processor
func (eq *EventRetryQueue) Stop() {
	eq.mu.Lock()
	if !eq.running {
		eq.mu.Unlock()
		return
	}
	eq.running = false
	eq.mu.Unlock()

	close(eq.stopCh)
	eq.wg.Wait()

	log.Println("Event retry queue stopped")
}

// QueueOrderCreated queues an ORDER_CREATED event for retry
func (eq *EventRetryQueue) QueueOrderCreated(order *models.Order, err error) {
	event := &QueuedEvent{
		EventType: OrderEventCreated,
		Order:     order,
		Timestamp: order.CreatedAt,
		Attempts:  1,
		LastError: err,
		CreatedAt: time.Now(),
	}

	select {
	case eq.queue <- event:
		log.Printf(" DB write failed, queued ORDER_CREATED for retry: order=%s error=%v", order.ID, err)
	default:
		eq.recordFailedEvent(event)
		log.Printf("Retry queue full, ORDER_CREATED lost: order=%s error=%v", order.ID, err)
	}
}

// QueueOrderFill queues an ORDER_FILL event for retry
func (eq *EventRetryQueue) QueueOrderFill(
	orderID uuid.UUID,
	fillQuantity, filledQuantity decimal.Decimal,
	status models.OrderStatus,
	tradeID *uuid.UUID,
	timestamp time.Time,
	err error,
) {
	event := &QueuedEvent{
		EventType:      OrderEventFilled,
		OrderID:        orderID,
		FillQuantity:   fillQuantity,
		FilledQuantity: filledQuantity,
		Status:         status,
		TradeID:        tradeID,
		Timestamp:      timestamp,
		Attempts:       1,
		LastError:      err,
		CreatedAt:      time.Now(),
	}

	if status == models.OrderStatusPartiallyFilled {
		event.EventType = OrderEventPartiallyFilled
	}

	select {
	case eq.queue <- event:
		log.Printf(" DB write failed, queued %s for retry: order=%s error=%v", event.EventType, orderID, err)
	default:
		eq.recordFailedEvent(event)
		log.Printf("Retry queue full, %s lost: order=%s error=%v", event.EventType, orderID, err)
	}
}

// QueueOrderCancelled queues an ORDER_CANCELLED event for retry
func (eq *EventRetryQueue) QueueOrderCancelled(orderID uuid.UUID, reason *string, timestamp time.Time, err error) {
	event := &QueuedEvent{
		EventType: OrderEventCancelled,
		OrderID:   orderID,
		Reason:    reason,
		Timestamp: timestamp,
		Attempts:  1,
		LastError: err,
		CreatedAt: time.Now(),
	}

	select {
	case eq.queue <- event:
		log.Printf(" DB write failed, queued ORDER_CANCELLED for retry: order=%s error=%v", orderID, err)
	default:
		eq.recordFailedEvent(event)
		log.Printf("Retry queue full, ORDER_CANCELLED lost: order=%s error=%v", orderID, err)
	}
}

// QueueOrderRejected queues an ORDER_REJECTED event for retry
func (eq *EventRetryQueue) QueueOrderRejected(orderID uuid.UUID, reason string, timestamp time.Time, err error) {
	reasonPtr := &reason
	event := &QueuedEvent{
		EventType: OrderEventRejected,
		OrderID:   orderID,
		Reason:    reasonPtr,
		Timestamp: timestamp,
		Attempts:  1,
		LastError: err,
		CreatedAt: time.Now(),
	}

	select {
	case eq.queue <- event:
		log.Printf(" DB write failed, queued ORDER_REJECTED for retry: order=%s error=%v", orderID, err)
	default:
		eq.recordFailedEvent(event)
		log.Printf("Retry queue full, ORDER_REJECTED lost: order=%s error=%v", orderID, err)
	}
}

// processQueue processes queued events
func (eq *EventRetryQueue) processQueue() {
	defer eq.wg.Done()

	ticker := time.NewTicker(eq.retryInterval)
	defer ticker.Stop()

	for {
		select {
		case <-eq.stopCh:
			return

		case event := <-eq.queue:
			eq.retryEvent(event)

		case <-ticker.C:
			// Retry failed events periodically
			eq.retryFailedEvents()
		}
	}
}

// retryEvent attempts to persist an event
func (eq *EventRetryQueue) retryEvent(event *QueuedEvent) {
	ctx := context.Background()
	var err error

	switch event.EventType {
	case OrderEventCreated:
		_, err = eq.store.RecordOrderCreated(ctx, event.Order)

	case OrderEventFilled, OrderEventPartiallyFilled:
		_, err = eq.store.RecordOrderFill(
			ctx,
			event.OrderID,
			event.FillQuantity,
			event.FilledQuantity,
			event.Status,
			event.TradeID,
			event.Timestamp,
		)

	case OrderEventCancelled:
		_, err = eq.store.RecordOrderCancelled(ctx, event.OrderID, event.Reason, event.Timestamp)

	case OrderEventRejected:
		reason := ""
		if event.Reason != nil {
			reason = *event.Reason
		}
		_, err = eq.store.RecordOrderRejected(ctx, event.OrderID, reason, event.Timestamp)
	}

	if err != nil {
		event.Attempts++
		event.LastError = err

		if event.Attempts >= eq.maxRetries {
			eq.recordFailedEvent(event)
			log.Printf("Event retry exhausted after %d attempts: type=%s order=%s error=%v",
				event.Attempts, event.EventType, event.getOrderID(), err)
		} else {
			// Requeue for another attempt
			select {
			case eq.queue <- event:
				log.Printf("🔄 Event retry failed (attempt %d/%d), requeued: type=%s order=%s error=%v",
					event.Attempts, eq.maxRetries, event.EventType, event.getOrderID(), err)
			default:
				eq.recordFailedEvent(event)
				log.Printf("Retry queue full on requeue: type=%s order=%s", event.EventType, event.getOrderID())
			}
		}
	} else {
		log.Printf("Event persisted after %d attempt(s): type=%s order=%s",
			event.Attempts, event.EventType, event.getOrderID())
	}
}

// retryFailedEvents attempts to retry permanently failed events
func (eq *EventRetryQueue) retryFailedEvents() {
	eq.mu.Lock()
	if len(eq.failedEvents) == 0 {
		eq.mu.Unlock()
		return
	}

	// Copy failed events
	toRetry := make([]*QueuedEvent, len(eq.failedEvents))
	copy(toRetry, eq.failedEvents)
	eq.failedEvents = eq.failedEvents[:0] // Clear list
	eq.mu.Unlock()

	for _, event := range toRetry {
		// Reset attempts for periodic retry
		event.Attempts = 0
		select {
		case eq.queue <- event:
			log.Printf("🔄 Retrying previously failed event: type=%s order=%s", event.EventType, event.getOrderID())
		default:
			eq.recordFailedEvent(event)
		}
	}
}

// recordFailedEvent records a permanently failed event
func (eq *EventRetryQueue) recordFailedEvent(event *QueuedEvent) {
	eq.mu.Lock()
	defer eq.mu.Unlock()
	eq.failedEvents = append(eq.failedEvents, event)
}

// GetFailedEvents returns all permanently failed events
func (eq *EventRetryQueue) GetFailedEvents() []*QueuedEvent {
	eq.mu.RLock()
	defer eq.mu.RUnlock()

	result := make([]*QueuedEvent, len(eq.failedEvents))
	copy(result, eq.failedEvents)
	return result
}

// GetQueueSize returns current queue size
func (eq *EventRetryQueue) GetQueueSize() int {
	return len(eq.queue)
}

// GetFailedEventCount returns count of permanently failed events
func (eq *EventRetryQueue) GetFailedEventCount() int {
	eq.mu.RLock()
	defer eq.mu.RUnlock()
	return len(eq.failedEvents)
}

// getOrderID returns the order ID from a queued event
func (event *QueuedEvent) getOrderID() uuid.UUID {
	if event.Order != nil {
		return event.Order.ID
	}
	return event.OrderID
}

// SafeOrderEventStore wraps OrderEventStore with retry queue
type SafeOrderEventStore struct {
	store      *OrderEventStore
	retryQueue *EventRetryQueue
}

// NewSafeOrderEventStore creates a store with automatic retry on failures
func NewSafeOrderEventStore(store *OrderEventStore, queueSize, maxRetries int, retryInterval time.Duration) *SafeOrderEventStore {
	retryQueue := NewEventRetryQueue(store, queueSize, maxRetries, retryInterval)
	retryQueue.Start()

	return &SafeOrderEventStore{
		store:      store,
		retryQueue: retryQueue,
	}
}

// RecordOrderCreated records order creation with retry on failure
func (s *SafeOrderEventStore) RecordOrderCreated(ctx context.Context, order *models.Order) (int64, error) {
	eventID, err := s.store.RecordOrderCreated(ctx, order)
	if err != nil {
		s.retryQueue.QueueOrderCreated(order, err)
		return 0, err
	}
	return eventID, nil
}

// RecordOrderFill records order fill with retry on failure
func (s *SafeOrderEventStore) RecordOrderFill(
	ctx context.Context,
	orderID uuid.UUID,
	fillQuantity, filledQuantity decimal.Decimal,
	status models.OrderStatus,
	tradeID *uuid.UUID,
	timestamp time.Time,
) (int64, error) {
	eventID, err := s.store.RecordOrderFill(ctx, orderID, fillQuantity, filledQuantity, status, tradeID, timestamp)
	if err != nil {
		s.retryQueue.QueueOrderFill(orderID, fillQuantity, filledQuantity, status, tradeID, timestamp, err)
		return 0, err
	}
	return eventID, nil
}

// RecordOrderCancelled records order cancellation with retry on failure
func (s *SafeOrderEventStore) RecordOrderCancelled(ctx context.Context, orderID uuid.UUID, reason *string, timestamp time.Time) (int64, error) {
	eventID, err := s.store.RecordOrderCancelled(ctx, orderID, reason, timestamp)
	if err != nil {
		s.retryQueue.QueueOrderCancelled(orderID, reason, timestamp, err)
		return 0, err
	}
	return eventID, nil
}

// RecordOrderRejected records order rejection with retry on failure
func (s *SafeOrderEventStore) RecordOrderRejected(ctx context.Context, orderID uuid.UUID, reason string, timestamp time.Time) (int64, error) {
	eventID, err := s.store.RecordOrderRejected(ctx, orderID, reason, timestamp)
	if err != nil {
		s.retryQueue.QueueOrderRejected(orderID, reason, timestamp, err)
		return 0, err
	}
	return eventID, nil
}

// Stop stops the retry queue
func (s *SafeOrderEventStore) Stop() {
	s.retryQueue.Stop()
}

// GetRetryQueue returns the retry queue for monitoring
func (s *SafeOrderEventStore) GetRetryQueue() *EventRetryQueue {
	return s.retryQueue
}

// Stats returns statistics about the store
func (s *SafeOrderEventStore) Stats() map[string]interface{} {
	return map[string]interface{}{
		"queue_size":         s.retryQueue.GetQueueSize(),
		"failed_event_count": s.retryQueue.GetFailedEventCount(),
		"max_retries":        s.retryQueue.maxRetries,
		"retry_interval_ms":  s.retryQueue.retryInterval.Milliseconds(),
	}
}
