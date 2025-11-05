package engine

import (
	"container/list"
	"sync"

	"github.com/google/btree"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/yourusername/trading-engine/models"
)

type PriceLevel struct {
	Price  decimal.Decimal
	Orders *list.List
	Volume decimal.Decimal
}

// NewPriceLevel creates a new price level
func NewPriceLevel(price decimal.Decimal) *PriceLevel {
	return &PriceLevel{
		Price:  price,
		Orders: list.New(),
		Volume: decimal.Zero,
	}
}

func (pl *PriceLevel) AddOrder(order *models.Order) *list.Element {
	element := pl.Orders.PushBack(order)
	pl.Volume = pl.Volume.Add(order.RemainingQuantity())
	return element
}

func (pl *PriceLevel) RemoveOrder(element *list.Element) {
	if element == nil {
		return
	}
	order := element.Value.(*models.Order)
	pl.Volume = pl.Volume.Sub(order.RemainingQuantity())
	pl.Orders.Remove(element)
}

func (pl *PriceLevel) UpdateVolume() {
	pl.Volume = decimal.Zero
	for e := pl.Orders.Front(); e != nil; e = e.Next() {
		order := e.Value.(*models.Order)
		pl.Volume = pl.Volume.Add(order.RemainingQuantity())
	}
}

func (pl *PriceLevel) IsEmpty() bool {
	return pl.Orders.Len() == 0
}

func (pl *PriceLevel) Less(than btree.Item) bool {
	other := than.(*PriceLevel)
	return pl.Price.LessThan(other.Price)
}

type OrderBookSide struct {
	tree *btree.BTree
	mu   sync.RWMutex
}

func NewOrderBookSide() *OrderBookSide {
	return &OrderBookSide{
		tree: btree.New(32),
	}
}

func (obs *OrderBookSide) GetOrCreatePriceLevel(price decimal.Decimal) *PriceLevel {
	searchLevel := &PriceLevel{Price: price}

	if item := obs.tree.Get(searchLevel); item != nil {
		return item.(*PriceLevel)
	}

	newLevel := NewPriceLevel(price)
	obs.tree.ReplaceOrInsert(newLevel)
	return newLevel
}

func (obs *OrderBookSide) GetPriceLevel(price decimal.Decimal) *PriceLevel {
	searchLevel := &PriceLevel{Price: price}
	if item := obs.tree.Get(searchLevel); item != nil {
		return item.(*PriceLevel)
	}
	return nil
}

// RemovePriceLevel removes a price level from the tree
func (obs *OrderBookSide) RemovePriceLevel(price decimal.Decimal) {
	searchLevel := &PriceLevel{Price: price}
	obs.tree.Delete(searchLevel)
}

// GetBestPrice returns the best price level (highest for bids, lowest for asks)
func (obs *OrderBookSide) GetBestPrice(isBid bool) *PriceLevel {
	var item btree.Item
	if isBid {
		// For bids, get maximum (highest price)
		item = obs.tree.Max()
	} else {
		// For asks, get minimum (lowest price)
		item = obs.tree.Min()
	}

	if item != nil {
		return item.(*PriceLevel)
	}
	return nil
}

// Ascend iterates through price levels in ascending order
func (obs *OrderBookSide) Ascend(iterator btree.ItemIterator) {
	obs.tree.Ascend(iterator)
}

// Descend iterates through price levels in descending order
func (obs *OrderBookSide) Descend(iterator btree.ItemIterator) {
	obs.tree.Descend(iterator)
}

// Len returns the number of price levels
func (obs *OrderBookSide) Len() int {
	return obs.tree.Len()
}

// OrderLocation tracks where an order is in the order book
type OrderLocation struct {
	PriceLevel *PriceLevel
	Element    *list.Element
}

// OrderBook represents the complete order book for an instrument
type OrderBook struct {
	Instrument string
	Bids       *OrderBookSide            // Buy orders (descending price)
	Asks       *OrderBookSide            // Sell orders (ascending price)
	Orders     map[string]*OrderLocation // Fast O(1) lookup by order ID
	mu         sync.RWMutex              // Protects the entire order book
}

// NewOrderBook creates a new order book for an instrument
func NewOrderBook(instrument string) *OrderBook {
	return &OrderBook{
		Instrument: instrument,
		Bids:       NewOrderBookSide(),
		Asks:       NewOrderBookSide(),
		Orders:     make(map[string]*OrderLocation),
	}
}

// AddOrder adds an order to the order book
func (ob *OrderBook) AddOrder(order *models.Order) {
	ob.mu.Lock()
	defer ob.mu.Unlock()

	// Determine which side to add to
	var side *OrderBookSide
	if order.Side == models.OrderSideBuy {
		side = ob.Bids
	} else {
		side = ob.Asks
	}

	// Get or create price level
	priceLevel := side.GetOrCreatePriceLevel(order.Price)

	// Add order to price level
	element := priceLevel.AddOrder(order)

	// Track order location for fast lookup
	ob.Orders[order.ID.String()] = &OrderLocation{
		PriceLevel: priceLevel,
		Element:    element,
	}
}

// AddLimitOrder inserts a limit order into the book using price-time priority
// This function does NOT perform matching - the matching worker handles that
// It only inserts the order into the appropriate price level in FIFO order
// Returns the order that was added to the book
func (ob *OrderBook) AddLimitOrder(order *models.Order) *models.Order {
	ob.mu.Lock()
	defer ob.mu.Unlock()

	// Validate it's a limit order
	if order.Type != models.OrderTypeLimit {
		return nil
	}

	// Determine which side to add to
	var side *OrderBookSide
	if order.Side == models.OrderSideBuy {
		side = ob.Bids
	} else {
		side = ob.Asks
	}

	// Get or create price level for this price
	priceLevel := side.GetOrCreatePriceLevel(order.Price)

	// Add order to the END of the queue at this price level (FIFO / time priority)
	element := priceLevel.AddOrder(order)

	// Track order location for fast O(1) lookup by ID
	ob.Orders[order.ID.String()] = &OrderLocation{
		PriceLevel: priceLevel,
		Element:    element,
	}

	// Set order status to open if not already set
	if order.Status == "" {
		order.Status = models.OrderStatusOpen
	}

	return order
}

// RemoveOrder removes an order from the order book
func (ob *OrderBook) RemoveOrder(orderID string) *models.Order {
	ob.mu.Lock()
	defer ob.mu.Unlock()

	location, exists := ob.Orders[orderID]
	if !exists {
		return nil
	}

	// Get the order
	order := location.Element.Value.(*models.Order)

	// Remove from price level
	location.PriceLevel.RemoveOrder(location.Element)

	// If price level is empty, remove it from the tree
	if location.PriceLevel.IsEmpty() {
		var side *OrderBookSide
		if order.Side == models.OrderSideBuy {
			side = ob.Bids
		} else {
			side = ob.Asks
		}
		side.RemovePriceLevel(location.PriceLevel.Price)
	}

	// Remove from order map
	delete(ob.Orders, orderID)

	return order
}

// CancelOrder cancels an order by its UUID
// Uses hash map for O(1) lookup, removes from price level queue,
// removes from hash map, and removes empty price levels from tree
func (ob *OrderBook) CancelOrder(orderID uuid.UUID) *models.Order {
	ob.mu.Lock()
	defer ob.mu.Unlock()

	orderIDStr := orderID.String()
	location, exists := ob.Orders[orderIDStr]
	if !exists {
		return nil
	}

	// Get the order
	order := location.Element.Value.(*models.Order)

	// Remove from price level
	location.PriceLevel.RemoveOrder(location.Element)

	// If price level is empty, remove it from the tree
	if location.PriceLevel.IsEmpty() {
		var side *OrderBookSide
		if order.Side == models.OrderSideBuy {
			side = ob.Bids
		} else {
			side = ob.Asks
		}
		side.RemovePriceLevel(location.PriceLevel.Price)
	}

	// Remove from order map
	delete(ob.Orders, orderIDStr)

	// Mark order as cancelled
	order.Status = models.OrderStatusCancelled

	return order
}

// GetOrder retrieves an order by ID (O(1) lookup)
func (ob *OrderBook) GetOrder(orderID string) *models.Order {
	ob.mu.RLock()
	defer ob.mu.RUnlock()

	location, exists := ob.Orders[orderID]
	if !exists {
		return nil
	}

	return location.Element.Value.(*models.Order)
}

// UpdateOrder updates an order's filled quantity and recalculates price level volume
func (ob *OrderBook) UpdateOrder(orderID string, newFilledQty decimal.Decimal) {
	ob.mu.Lock()
	defer ob.mu.Unlock()

	location, exists := ob.Orders[orderID]
	if !exists {
		return
	}

	order := location.Element.Value.(*models.Order)
	order.FilledQuantity = newFilledQty

	// Update status based on filled quantity
	if order.IsFilled() {
		order.Status = models.OrderStatusFilled
	} else if order.IsPartiallyFilled() {
		order.Status = models.OrderStatusPartiallyFilled
	}

	// Recalculate price level volume
	location.PriceLevel.UpdateVolume()
}

// GetBestBid returns the highest bid price level
func (ob *OrderBook) GetBestBid() *PriceLevel {
	ob.mu.RLock()
	defer ob.mu.RUnlock()
	return ob.Bids.GetBestPrice(true)
}

// GetBestAsk returns the lowest ask price level
func (ob *OrderBook) GetBestAsk() *PriceLevel {
	ob.mu.RLock()
	defer ob.mu.RUnlock()
	return ob.Asks.GetBestPrice(false)
}

// GetBestBidPrice returns the highest bid price as decimal (nil if no bids)
func (ob *OrderBook) GetBestBidPrice() *decimal.Decimal {
	bestBid := ob.GetBestBid()
	if bestBid == nil {
		return nil
	}
	return &bestBid.Price
}

// GetBestAskPrice returns the lowest ask price as decimal (nil if no asks)
func (ob *OrderBook) GetBestAskPrice() *decimal.Decimal {
	bestAsk := ob.GetBestAsk()
	if bestAsk == nil {
		return nil
	}
	return &bestAsk.Price
}

// PeekBest returns the best bid and ask price levels without removing them
func (ob *OrderBook) PeekBest() (bestBid, bestAsk *PriceLevel) {
	ob.mu.RLock()
	defer ob.mu.RUnlock()

	bestBid = ob.Bids.GetBestPrice(true)
	bestAsk = ob.Asks.GetBestPrice(false)
	return bestBid, bestAsk
}

// PopBestLevel removes and returns the entire best price level from the specified side
// For bids: removes the highest price level (max-heap behavior)
// For asks: removes the lowest price level (min-heap behavior)
// Returns nil if the side is empty
func (ob *OrderBook) PopBestLevel(isBid bool) *PriceLevel {
	ob.mu.Lock()
	defer ob.mu.Unlock()

	var side *OrderBookSide
	if isBid {
		side = ob.Bids
	} else {
		side = ob.Asks
	}

	// Get the best price level
	bestLevel := side.GetBestPrice(isBid)
	if bestLevel == nil {
		return nil
	}

	// Remove all orders from this price level from the hash map
	for element := bestLevel.Orders.Front(); element != nil; element = element.Next() {
		order := element.Value.(*models.Order)
		delete(ob.Orders, order.ID.String())
	}

	// Remove the price level from the tree
	side.RemovePriceLevel(bestLevel.Price)

	return bestLevel
}

// GetSpread returns the bid-ask spread
func (ob *OrderBook) GetSpread() decimal.Decimal {
	ob.mu.RLock()
	defer ob.mu.RUnlock()

	bestBid := ob.Bids.GetBestPrice(true)
	bestAsk := ob.Asks.GetBestPrice(false)

	if bestBid == nil || bestAsk == nil {
		return decimal.Zero
	}

	return bestAsk.Price.Sub(bestBid.Price)
}

// GetDepth returns the total volume on each side
func (ob *OrderBook) GetDepth() (bidVolume, askVolume decimal.Decimal) {
	ob.mu.RLock()
	defer ob.mu.RUnlock()

	bidVolume = decimal.Zero
	askVolume = decimal.Zero

	// Sum bid volumes
	ob.Bids.Ascend(func(item btree.Item) bool {
		level := item.(*PriceLevel)
		bidVolume = bidVolume.Add(level.Volume)
		return true
	})

	// Sum ask volumes
	ob.Asks.Ascend(func(item btree.Item) bool {
		level := item.(*PriceLevel)
		askVolume = askVolume.Add(level.Volume)
		return true
	})

	return bidVolume, askVolume
}

// GetTopLevels returns the top N price levels for bids and asks
func (ob *OrderBook) GetTopLevels(n int) (bids, asks []*PriceLevel) {
	ob.mu.RLock()
	defer ob.mu.RUnlock()

	bids = make([]*PriceLevel, 0, n)
	asks = make([]*PriceLevel, 0, n)

	// Get top N bids (highest prices first)
	count := 0
	ob.Bids.Descend(func(item btree.Item) bool {
		if count >= n {
			return false
		}
		bids = append(bids, item.(*PriceLevel))
		count++
		return true
	})

	// Get top N asks (lowest prices first)
	count = 0
	ob.Asks.Ascend(func(item btree.Item) bool {
		if count >= n {
			return false
		}
		asks = append(asks, item.(*PriceLevel))
		count++
		return true
	})

	return bids, asks
}

// Clear removes all orders from the order book
func (ob *OrderBook) Clear() {
	ob.mu.Lock()
	defer ob.mu.Unlock()

	ob.Bids = NewOrderBookSide()
	ob.Asks = NewOrderBookSide()
	ob.Orders = make(map[string]*OrderLocation)
}

// Size returns the total number of orders in the order book
func (ob *OrderBook) Size() int {
	ob.mu.RLock()
	defer ob.mu.RUnlock()
	return len(ob.Orders)
}

// GetBidDepth returns the number of buy orders in the orderbook
func (ob *OrderBook) GetBidDepth() int {
	ob.mu.RLock()
	defer ob.mu.RUnlock()

	count := 0
	ob.Bids.tree.Descend(func(i btree.Item) bool {
		pl := i.(*PriceLevel)
		count += pl.Orders.Len()
		return true
	})
	return count
}

// GetAskDepth returns the number of sell orders in the orderbook
func (ob *OrderBook) GetAskDepth() int {
	ob.mu.RLock()
	defer ob.mu.RUnlock()

	count := 0
	ob.Asks.tree.Ascend(func(i btree.Item) bool {
		pl := i.(*PriceLevel)
		count += pl.Orders.Len()
		return true
	})
	return count
}
