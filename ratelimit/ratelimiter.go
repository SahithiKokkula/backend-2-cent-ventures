package ratelimit

import (
	"context"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

type TokenBucketLimiter struct {
	redisClient   *redis.Client
	inMemoryStore *InMemoryStore
	useRedis      bool

	maxTokens         int
	refillRate        int
	refillInterval    time.Duration
	keyPrefix         string
	whitelistedKeys   map[string]bool
	mu                sync.RWMutex
	originalMaxTokens int
}

type Config struct {
	MaxTokens            int
	RefillRate           int
	RefillInterval       time.Duration
	KeyPrefix            string
	WhitelistedKeys      []string
	ConservativeFallback bool
}

type InMemoryStore struct {
	mu      sync.RWMutex
	buckets map[string]*Bucket
	cleanup *time.Ticker
}

type Bucket struct {
	tokens     float64
	lastRefill time.Time
	maxTokens  int
	refillRate float64
}

type RateLimitResult struct {
	Allowed       bool
	Remaining     int
	RetryAfter    time.Duration
	ResetAt       time.Time
	CurrentTokens float64
}

func DefaultConfig() Config {
	return Config{
		MaxTokens:      100,
		RefillRate:     10,
		RefillInterval: 1 * time.Second,
		KeyPrefix:      "ratelimit:",
	}
}

func NewTokenBucketLimiter(redisClient *redis.Client, config Config) *TokenBucketLimiter {
	if config.MaxTokens == 0 {
		config.MaxTokens = 100
	}
	if config.RefillRate == 0 {
		config.RefillRate = 10
	}
	if config.RefillInterval == 0 {
		config.RefillInterval = 1 * time.Second
	}
	if config.KeyPrefix == "" {
		config.KeyPrefix = "ratelimit:"
	}

	limiter := &TokenBucketLimiter{
		redisClient:       redisClient,
		maxTokens:         config.MaxTokens,
		refillRate:        config.RefillRate,
		refillInterval:    config.RefillInterval,
		keyPrefix:         config.KeyPrefix,
		whitelistedKeys:   make(map[string]bool),
		originalMaxTokens: config.MaxTokens,
	}

	for _, key := range config.WhitelistedKeys {
		limiter.whitelistedKeys[key] = true
	}

	// Check if Redis is available
	if redisClient != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		if err := redisClient.Ping(ctx).Err(); err == nil {
			limiter.useRedis = true
		}
	}

	// Initialize in-memory store (used as fallback or when Redis unavailable)
	// If ConservativeFallback is enabled and Redis fails, use stricter limits
	if !limiter.useRedis {
		if config.ConservativeFallback {
			// Use 50% of configured limits as conservative fallback
			conservativeMaxTokens := config.MaxTokens / 2
			conservativeRefillRate := config.RefillRate / 2
			if conservativeMaxTokens < 10 {
				conservativeMaxTokens = 10 // Minimum 10 tokens
			}
			if conservativeRefillRate < 1 {
				conservativeRefillRate = 1 // Minimum 1 token/interval
			}
			limiter.maxTokens = conservativeMaxTokens
			limiter.refillRate = conservativeRefillRate
		}
		limiter.inMemoryStore = NewInMemoryStore()
	}

	return limiter
}

// Allow checks if a request should be allowed for the given client
func (tbl *TokenBucketLimiter) Allow(ctx context.Context, clientKey string) (*RateLimitResult, error) {
	// Check if client is whitelisted
	tbl.mu.RLock()
	isWhitelisted := tbl.whitelistedKeys[clientKey]
	tbl.mu.RUnlock()

	if isWhitelisted {
		// Whitelisted clients always allowed with original max tokens (not reduced by fallback)
		return &RateLimitResult{
			Allowed:       true,
			Remaining:     tbl.originalMaxTokens,
			CurrentTokens: float64(tbl.originalMaxTokens),
		}, nil
	}

	if tbl.useRedis {
		return tbl.allowRedis(ctx, clientKey)
	}
	return tbl.allowInMemory(clientKey)
}

// AddWhitelistedKey adds a client key to the whitelist (bypasses rate limiting)
func (tbl *TokenBucketLimiter) AddWhitelistedKey(clientKey string) {
	tbl.mu.Lock()
	defer tbl.mu.Unlock()
	tbl.whitelistedKeys[clientKey] = true
}

// RemoveWhitelistedKey removes a client key from the whitelist
func (tbl *TokenBucketLimiter) RemoveWhitelistedKey(clientKey string) {
	tbl.mu.Lock()
	defer tbl.mu.Unlock()
	delete(tbl.whitelistedKeys, clientKey)
}

// IsWhitelisted checks if a client key is whitelisted
func (tbl *TokenBucketLimiter) IsWhitelisted(clientKey string) bool {
	tbl.mu.RLock()
	defer tbl.mu.RUnlock()
	return tbl.whitelistedKeys[clientKey]
}

// allowRedis implements rate limiting using Redis
func (tbl *TokenBucketLimiter) allowRedis(ctx context.Context, clientKey string) (*RateLimitResult, error) {
	key := tbl.keyPrefix + clientKey
	now := time.Now()

	// Lua script for atomic token bucket operations
	script := `
		local key = KEYS[1]
		local max_tokens = tonumber(ARGV[1])
		local refill_rate = tonumber(ARGV[2])
		local refill_interval_ms = tonumber(ARGV[3])
		local now_ms = tonumber(ARGV[4])
		local cost = tonumber(ARGV[5])
		
		-- Get current bucket state
		local bucket = redis.call('HMGET', key, 'tokens', 'last_refill')
		local tokens = tonumber(bucket[1])
		local last_refill_ms = tonumber(bucket[2])
		
		-- Initialize if bucket doesn't exist
		if tokens == nil then
			tokens = max_tokens
			last_refill_ms = now_ms
		end
		
		-- Calculate tokens to add based on elapsed time
		local elapsed_ms = now_ms - last_refill_ms
		local intervals_passed = elapsed_ms / refill_interval_ms
		local tokens_to_add = intervals_passed * refill_rate
		
		-- Refill tokens (capped at max)
		tokens = math.min(max_tokens, tokens + tokens_to_add)
		last_refill_ms = now_ms
		
		-- Check if request can be allowed
		local allowed = tokens >= cost
		
		if allowed then
			tokens = tokens - cost
		end
		
		-- Update bucket state
		redis.call('HMSET', key, 'tokens', tokens, 'last_refill', last_refill_ms)
		redis.call('EXPIRE', key, 3600) -- Expire after 1 hour of inactivity
		
		-- Return: allowed (1/0), remaining tokens (after deduction), current tokens
		return {allowed and 1 or 0, math.floor(tokens), tokens}
	`

	refillIntervalMs := tbl.refillInterval.Milliseconds()
	nowMs := now.UnixMilli()
	cost := 1.0 // Each request costs 1 token

	result, err := tbl.redisClient.Eval(ctx, script, []string{key},
		tbl.maxTokens,
		tbl.refillRate,
		refillIntervalMs,
		nowMs,
		cost,
	).Result()

	if err != nil {
		// Fallback to in-memory on Redis error
		if tbl.inMemoryStore == nil {
			tbl.inMemoryStore = NewInMemoryStore()
		}
		tbl.useRedis = false
		return tbl.allowInMemory(clientKey)
	}

	values := result.([]interface{})
	allowed := values[0].(int64) == 1
	remaining := int(values[1].(int64))

	// Handle currentTokens - Redis may return int64 or float64
	var currentTokens float64
	switch v := values[2].(type) {
	case float64:
		currentTokens = v
	case int64:
		currentTokens = float64(v)
	default:
		// Fallback to in-memory on unexpected type
		if tbl.inMemoryStore == nil {
			tbl.inMemoryStore = NewInMemoryStore()
		}
		tbl.useRedis = false
		return tbl.allowInMemory(clientKey)
	}

	rateLimitResult := &RateLimitResult{
		Allowed:       allowed,
		Remaining:     remaining,
		CurrentTokens: currentTokens,
	}

	if !allowed {
		// Calculate retry after time
		tokensNeeded := 1.0 - currentTokens
		tokensPerSecond := float64(tbl.refillRate) / tbl.refillInterval.Seconds()
		secondsToWait := tokensNeeded / tokensPerSecond
		rateLimitResult.RetryAfter = time.Duration(secondsToWait * float64(time.Second))
		rateLimitResult.ResetAt = now.Add(rateLimitResult.RetryAfter)
	}

	return rateLimitResult, nil
}

// allowInMemory implements rate limiting using in-memory storage
func (tbl *TokenBucketLimiter) allowInMemory(clientKey string) (*RateLimitResult, error) {
	bucket := tbl.inMemoryStore.GetOrCreate(clientKey, tbl.maxTokens, tbl.refillRate, tbl.refillInterval)

	bucket.mu.Lock()
	defer bucket.mu.Unlock()

	now := time.Now()

	// Calculate tokens to add based on elapsed time
	elapsed := now.Sub(bucket.lastRefill)
	tokensPerSecond := bucket.refillRate
	tokensToAdd := elapsed.Seconds() * tokensPerSecond

	// Refill tokens (capped at max)
	bucket.tokens = min(float64(bucket.maxTokens), bucket.tokens+tokensToAdd)
	bucket.lastRefill = now

	// Check if request can be allowed
	allowed := bucket.tokens >= 1.0

	result := &RateLimitResult{
		Allowed:       allowed,
		Remaining:     int(bucket.tokens),
		CurrentTokens: bucket.tokens,
	}

	if allowed {
		bucket.tokens -= 1.0
		result.Remaining = int(bucket.tokens)
	} else {
		// Calculate retry after time
		tokensNeeded := 1.0 - bucket.tokens
		secondsToWait := tokensNeeded / tokensPerSecond
		result.RetryAfter = time.Duration(secondsToWait * float64(time.Second))
		result.ResetAt = now.Add(result.RetryAfter)
	}

	return result, nil
}

// GetStats returns current rate limit statistics for a client
func (tbl *TokenBucketLimiter) GetStats(ctx context.Context, clientKey string) (float64, error) {
	if tbl.useRedis {
		key := tbl.keyPrefix + clientKey
		tokens, err := tbl.redisClient.HGet(ctx, key, "tokens").Float64()
		if err != nil {
			return float64(tbl.maxTokens), nil // Return max if key doesn't exist
		}
		return tokens, nil
	}

	bucket := tbl.inMemoryStore.Get(clientKey)
	if bucket == nil {
		return float64(tbl.maxTokens), nil
	}

	bucket.mu.RLock()
	defer bucket.mu.RUnlock()
	return bucket.tokens, nil
}

// NewInMemoryStore creates a new in-memory token bucket store
func NewInMemoryStore() *InMemoryStore {
	store := &InMemoryStore{
		buckets: make(map[string]*Bucket),
		cleanup: time.NewTicker(5 * time.Minute),
	}

	// Start cleanup routine
	go store.cleanupRoutine()

	return store
}

// GetOrCreate gets or creates a bucket for a client
func (ims *InMemoryStore) GetOrCreate(clientKey string, maxTokens int, refillRate int, refillInterval time.Duration) *BucketWithLock {
	ims.mu.Lock()
	defer ims.mu.Unlock()

	bucket, exists := ims.buckets[clientKey]
	if !exists {
		tokensPerSecond := float64(refillRate) / refillInterval.Seconds()
		bucket = &Bucket{
			tokens:     float64(maxTokens),
			lastRefill: time.Now(),
			maxTokens:  maxTokens,
			refillRate: tokensPerSecond,
		}
		ims.buckets[clientKey] = bucket
	}

	return &BucketWithLock{Bucket: bucket}
}

// Get retrieves a bucket for a client (returns nil if not found)
func (ims *InMemoryStore) Get(clientKey string) *BucketWithLock {
	ims.mu.RLock()
	defer ims.mu.RUnlock()

	bucket, exists := ims.buckets[clientKey]
	if !exists {
		return nil
	}

	return &BucketWithLock{Bucket: bucket}
}

// cleanupRoutine removes inactive buckets
func (ims *InMemoryStore) cleanupRoutine() {
	for range ims.cleanup.C {
		ims.mu.Lock()
		now := time.Now()
		for key, bucket := range ims.buckets {
			if now.Sub(bucket.lastRefill) > 1*time.Hour {
				delete(ims.buckets, key)
			}
		}
		ims.mu.Unlock()
	}
}

// BucketWithLock wraps Bucket with a mutex for safe concurrent access
type BucketWithLock struct {
	*Bucket
	mu sync.RWMutex
}

func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
