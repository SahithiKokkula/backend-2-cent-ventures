package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/go-redis/redis/v8"
)

type RedisCache struct {
	client *redis.Client
	ctx    context.Context
}

type RedisCacheConfig struct {
	Host     string
	Port     int
	Password string
	DB       int
	PoolSize int
}

func DefaultRedisCacheConfig() *RedisCacheConfig {
	return &RedisCacheConfig{
		Host:     "localhost",
		Port:     6379,
		Password: "",
		DB:       0,
		PoolSize: 10,
	}
}

func NewRedisCache(config *RedisCacheConfig) (*RedisCache, error) {
	if config == nil {
		config = DefaultRedisCacheConfig()
	}

	client := redis.NewClient(&redis.Options{
		Addr:         fmt.Sprintf("%s:%d", config.Host, config.Port),
		Password:     config.Password,
		DB:           config.DB,
		PoolSize:     config.PoolSize,
		MinIdleConns: 2,
		MaxRetries:   3,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
		PoolTimeout:  4 * time.Second,
	})

	ctx := context.Background()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	return &RedisCache{
		client: client,
		ctx:    ctx,
	}, nil
}

func (rc *RedisCache) Get(key string) (string, error) {
	val, err := rc.client.Get(rc.ctx, key).Result()
	if err == redis.Nil {
		return "", fmt.Errorf("key not found: %s", key)
	}
	if err != nil {
		return "", fmt.Errorf("failed to get key %s: %w", key, err)
	}
	return val, nil
}

func (rc *RedisCache) Set(key string, value interface{}, ttl time.Duration) error {
	var data string
	switch v := value.(type) {
	case string:
		data = v
	case []byte:
		data = string(v)
	default:
		jsonData, err := json.Marshal(value)
		if err != nil {
			return fmt.Errorf("failed to marshal value: %w", err)
		}
		data = string(jsonData)
	}

	err := rc.client.Set(rc.ctx, key, data, ttl).Err()
	if err != nil {
		return fmt.Errorf("failed to set key %s: %w", key, err)
	}
	return nil
}

// GetJSON retrieves and unmarshals JSON data from cache
func (rc *RedisCache) GetJSON(key string, dest interface{}) error {
	val, err := rc.Get(key)
	if err != nil {
		return err
	}

	err = json.Unmarshal([]byte(val), dest)
	if err != nil {
		return fmt.Errorf("failed to unmarshal JSON for key %s: %w", key, err)
	}
	return nil
}

// SetJSON marshals and stores JSON data in cache with TTL
func (rc *RedisCache) SetJSON(key string, value interface{}, ttl time.Duration) error {
	jsonData, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}

	return rc.Set(key, jsonData, ttl)
}

// Delete removes a key from cache
func (rc *RedisCache) Delete(key string) error {
	err := rc.client.Del(rc.ctx, key).Err()
	if err != nil {
		return fmt.Errorf("failed to delete key %s: %w", key, err)
	}
	return nil
}

// DeletePattern removes all keys matching a pattern
// Example: DeletePattern("orderbook:*") removes all orderbook caches
func (rc *RedisCache) DeletePattern(pattern string) error {
	var cursor uint64
	var keys []string

	// Scan for keys matching pattern
	for {
		var scanKeys []string
		var err error
		scanKeys, cursor, err = rc.client.Scan(rc.ctx, cursor, pattern, 100).Result()
		if err != nil {
			return fmt.Errorf("failed to scan keys with pattern %s: %w", pattern, err)
		}

		keys = append(keys, scanKeys...)

		if cursor == 0 {
			break
		}
	}

	// Delete all matching keys
	if len(keys) > 0 {
		err := rc.client.Del(rc.ctx, keys...).Err()
		if err != nil {
			return fmt.Errorf("failed to delete keys: %w", err)
		}
	}

	return nil
}

// Exists checks if a key exists in cache
func (rc *RedisCache) Exists(key string) (bool, error) {
	count, err := rc.client.Exists(rc.ctx, key).Result()
	if err != nil {
		return false, fmt.Errorf("failed to check key existence: %w", err)
	}
	return count > 0, nil
}

// GetTTL returns the remaining TTL for a key
func (rc *RedisCache) GetTTL(key string) (time.Duration, error) {
	ttl, err := rc.client.TTL(rc.ctx, key).Result()
	if err != nil {
		return 0, fmt.Errorf("failed to get TTL for key %s: %w", key, err)
	}
	return ttl, nil
}

// Expire sets a new TTL for an existing key
func (rc *RedisCache) Expire(key string, ttl time.Duration) error {
	err := rc.client.Expire(rc.ctx, key, ttl).Err()
	if err != nil {
		return fmt.Errorf("failed to set expiry for key %s: %w", key, err)
	}
	return nil
}

// Increment atomically increments a key's value
func (rc *RedisCache) Increment(key string) (int64, error) {
	val, err := rc.client.Incr(rc.ctx, key).Result()
	if err != nil {
		return 0, fmt.Errorf("failed to increment key %s: %w", key, err)
	}
	return val, nil
}

// IncrementBy atomically increments a key's value by delta
func (rc *RedisCache) IncrementBy(key string, delta int64) (int64, error) {
	val, err := rc.client.IncrBy(rc.ctx, key, delta).Result()
	if err != nil {
		return 0, fmt.Errorf("failed to increment key %s by %d: %w", key, delta, err)
	}
	return val, nil
}

// SetNX sets a key only if it doesn't exist (SET if Not eXists)
// Useful for distributed locks
func (rc *RedisCache) SetNX(key string, value interface{}, ttl time.Duration) (bool, error) {
	var data string
	switch v := value.(type) {
	case string:
		data = v
	default:
		jsonData, err := json.Marshal(value)
		if err != nil {
			return false, fmt.Errorf("failed to marshal value: %w", err)
		}
		data = string(jsonData)
	}

	ok, err := rc.client.SetNX(rc.ctx, key, data, ttl).Result()
	if err != nil {
		return false, fmt.Errorf("failed to setnx key %s: %w", key, err)
	}
	return ok, nil
}

// MGet gets multiple keys at once
func (rc *RedisCache) MGet(keys ...string) ([]interface{}, error) {
	vals, err := rc.client.MGet(rc.ctx, keys...).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to mget keys: %w", err)
	}
	return vals, nil
}

// MSet sets multiple keys at once
func (rc *RedisCache) MSet(pairs map[string]interface{}) error {
	// Convert map to slice of key-value pairs
	kvPairs := make([]interface{}, 0, len(pairs)*2)
	for k, v := range pairs {
		kvPairs = append(kvPairs, k)

		// Serialize value
		switch val := v.(type) {
		case string:
			kvPairs = append(kvPairs, val)
		default:
			jsonData, err := json.Marshal(val)
			if err != nil {
				return fmt.Errorf("failed to marshal value for key %s: %w", k, err)
			}
			kvPairs = append(kvPairs, string(jsonData))
		}
	}

	err := rc.client.MSet(rc.ctx, kvPairs...).Err()
	if err != nil {
		return fmt.Errorf("failed to mset keys: %w", err)
	}
	return nil
}

// FlushDB clears all keys in the current database (use with caution!)
func (rc *RedisCache) FlushDB() error {
	err := rc.client.FlushDB(rc.ctx).Err()
	if err != nil {
		return fmt.Errorf("failed to flush database: %w", err)
	}
	return nil
}

// GetStats returns Redis statistics
func (rc *RedisCache) GetStats() map[string]interface{} {
	poolStats := rc.client.PoolStats()

	return map[string]interface{}{
		"hits":        poolStats.Hits,
		"misses":      poolStats.Misses,
		"timeouts":    poolStats.Timeouts,
		"total_conns": poolStats.TotalConns,
		"idle_conns":  poolStats.IdleConns,
		"stale_conns": poolStats.StaleConns,
	}
}

// Ping checks if Redis connection is alive
func (rc *RedisCache) Ping() error {
	return rc.client.Ping(rc.ctx).Err()
}

// Close closes the Redis connection
func (rc *RedisCache) Close() error {
	return rc.client.Close()
}

// GetClient returns the underlying Redis client for advanced operations
func (rc *RedisCache) GetClient() *redis.Client {
	return rc.client
}

// KeyBuilder provides structured key naming conventions
type KeyBuilder struct {
	prefix string
}

// NewKeyBuilder creates a new key builder with optional prefix
func NewKeyBuilder(prefix string) *KeyBuilder {
	return &KeyBuilder{prefix: prefix}
}

// OrderbookKey returns key for full orderbook cache
// Format: orderbook:{symbol}
func (kb *KeyBuilder) OrderbookKey(symbol string) string {
	if kb.prefix != "" {
		return fmt.Sprintf("%s:orderbook:%s", kb.prefix, symbol)
	}
	return fmt.Sprintf("orderbook:%s", symbol)
}

// OrderbookTopKey returns key for top-of-book cache
// Format: orderbook:top:{symbol}
func (kb *KeyBuilder) OrderbookTopKey(symbol string) string {
	if kb.prefix != "" {
		return fmt.Sprintf("%s:orderbook:top:%s", kb.prefix, symbol)
	}
	return fmt.Sprintf("orderbook:top:%s", symbol)
}

// TradesKey returns key for recent trades cache
// Format: trades:{symbol}:{limit}
func (kb *KeyBuilder) TradesKey(symbol string, limit int) string {
	if kb.prefix != "" {
		return fmt.Sprintf("%s:trades:%s:%d", kb.prefix, symbol, limit)
	}
	return fmt.Sprintf("trades:%s:%d", symbol, limit)
}

// TradesLatestKey returns key for latest trades (without limit)
// Format: trades:{symbol}:latest
func (kb *KeyBuilder) TradesLatestKey(symbol string) string {
	if kb.prefix != "" {
		return fmt.Sprintf("%s:trades:%s:latest", kb.prefix, symbol)
	}
	return fmt.Sprintf("trades:%s:latest", symbol)
}

// OrderKey returns key for individual order cache
// Format: order:{orderId}
func (kb *KeyBuilder) OrderKey(orderID string) string {
	if kb.prefix != "" {
		return fmt.Sprintf("%s:order:%s", kb.prefix, orderID)
	}
	return fmt.Sprintf("order:%s", orderID)
}

// StatsKey returns key for statistics cache
// Format: stats:{symbol}:{metric}
func (kb *KeyBuilder) StatsKey(symbol, metric string) string {
	if kb.prefix != "" {
		return fmt.Sprintf("%s:stats:%s:%s", kb.prefix, symbol, metric)
	}
	return fmt.Sprintf("stats:%s:%s", symbol, metric)
}

// LockKey returns key for distributed locks
// Format: lock:{resource}
func (kb *KeyBuilder) LockKey(resource string) string {
	if kb.prefix != "" {
		return fmt.Sprintf("%s:lock:%s", kb.prefix, resource)
	}
	return fmt.Sprintf("lock:%s", resource)
}

// PubSubChannel returns channel name for pub/sub
// Format: channel:{topic}
func (kb *KeyBuilder) PubSubChannel(topic string) string {
	if kb.prefix != "" {
		return fmt.Sprintf("%s:channel:%s", kb.prefix, topic)
	}
	return fmt.Sprintf("channel:%s", topic)
}
