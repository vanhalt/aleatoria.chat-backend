package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"os"
	"sort"
	"strconv"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/redis/go-redis/v9"
)

// RedisStore handles all Redis operations with graceful degradation
type RedisStore struct {
	client     *redis.Client
	ctx        context.Context
	messageTTL time.Duration
}

// NewRedisStore creates a new Redis store with configuration from environment variables
func NewRedisStore() *RedisStore {
	// Get configuration from environment variables with defaults
	valkeyHost := os.Getenv("VALKEY_HOST")
	if valkeyHost == "" {
		valkeyHost = "localhost"
	}

	valkeyPort := os.Getenv("VALKEY_PORT")
	if valkeyPort == "" {
		valkeyPort = "6379"
	}

	valkeyAddr := fmt.Sprintf("%s:%s", valkeyHost, valkeyPort)
	valkeyPassword := os.Getenv("VALKEY_PASSWORD")

	valkeyUsername := os.Getenv("VALKEY_USERNAME")
	if valkeyUsername == "" {
		valkeyUsername = "default"
	}

	// TLS configuration - enabled by default for Digital Ocean compatibility
	useTLS := true
	if tlsEnv := os.Getenv("VALKEY_USE_TLS"); tlsEnv != "" {
		useTLS = tlsEnv != "false" && tlsEnv != "0"
	}

	redisDB := 0
	if dbStr := os.Getenv("REDIS_DB"); dbStr != "" {
		if db, err := strconv.Atoi(dbStr); err == nil {
			redisDB = db
		}
	}

	messageTTLHours := 24
	if ttlStr := os.Getenv("MESSAGE_TTL_HOURS"); ttlStr != "" {
		if ttl, err := strconv.Atoi(ttlStr); err == nil {
			messageTTLHours = ttl
		}
	}

	// Configure Redis client options
	options := &redis.Options{
		Addr:         valkeyAddr,
		Username:     valkeyUsername,
		Password:     valkeyPassword,
		DB:           redisDB,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
		PoolSize:     10,
		MinIdleConns: 2,
	}

	// Enable TLS if configured
	if useTLS {
		options.TLSConfig = &tls.Config{
			MinVersion: tls.VersionTLS12,
		}
	}

	client := redis.NewClient(options)

	store := &RedisStore{
		client:     client,
		ctx:        context.Background(),
		messageTTL: time.Duration(messageTTLHours) * time.Hour,
	}

	// Test connection (non-blocking)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := client.Ping(ctx).Err(); err != nil {
			log.Printf("WARNING: Valkey/Redis connection failed: %v. Will operate with degraded functionality.", err)

			// Capture Redis connection failure
			sentry.WithScope(func(scope *sentry.Scope) {
				scope.SetTag("operation", "redis_connect")
				scope.SetTag("redisAddr", valkeyAddr)
				scope.SetLevel(sentry.LevelWarning)
				scope.SetContext("redis_config", map[string]interface{}{
					"address": valkeyAddr,
					"db":      redisDB,
				})
				sentry.CaptureException(err)
			})
		} else {
			tlsStatus := "without TLS"
			if useTLS {
				tlsStatus = "with TLS"
			}
			log.Printf("Valkey/Redis connected successfully at %s (%s)", valkeyAddr, tlsStatus)

			// Add breadcrumb for successful connection
			sentry.AddBreadcrumb(&sentry.Breadcrumb{
				Category:  "redis",
				Message:   "Redis connected successfully",
				Level:     sentry.LevelInfo,
				Timestamp: time.Now(),
				Data: map[string]interface{}{
					"address": valkeyAddr,
					"tls":     useTLS,
				},
			})
		}
	}()

	return store
}

// Close closes the Redis connection
func (rs *RedisStore) Close() error {
	if rs.client != nil {
		return rs.client.Close()
	}
	return nil
}

// Room Operations

// AddUserToRoom adds a user to a room in Redis
func (rs *RedisStore) AddUserToRoom(roomID, userID string) error {
	ctx, cancel := context.WithTimeout(rs.ctx, 2*time.Second)
	defer cancel()

	pipe := rs.client.Pipeline()
	// Add user to room members set
	pipe.SAdd(ctx, fmt.Sprintf("room:members:%s", roomID), userID)
	// Add room to user's rooms set
	pipe.SAdd(ctx, fmt.Sprintf("user:rooms:%s", userID), roomID)

	_, err := pipe.Exec(ctx)
	if err != nil {
		log.Printf("Redis error adding user %s to room %s: %v", userID, roomID, err)

		// Capture Redis operation error
		sentry.WithScope(func(scope *sentry.Scope) {
			scope.SetTag("operation", "redis_add_user_to_room")
			scope.SetTag("userId", userID)
			scope.SetTag("room", roomID)
			scope.SetLevel(sentry.LevelError)
			sentry.CaptureException(err)
		})
		return err
	}
	return nil
}

// RemoveUserFromRoom removes a user from a room in Redis
func (rs *RedisStore) RemoveUserFromRoom(roomID, userID string) error {
	ctx, cancel := context.WithTimeout(rs.ctx, 2*time.Second)
	defer cancel()

	pipe := rs.client.Pipeline()
	// Remove user from room members set
	pipe.SRem(ctx, fmt.Sprintf("room:members:%s", roomID), userID)
	// Remove room from user's rooms set
	pipe.SRem(ctx, fmt.Sprintf("user:rooms:%s", userID), roomID)

	_, err := pipe.Exec(ctx)
	if err != nil {
		log.Printf("Redis error removing user %s from room %s: %v", userID, roomID, err)

		// Capture Redis operation error
		sentry.WithScope(func(scope *sentry.Scope) {
			scope.SetTag("operation", "redis_remove_user_from_room")
			scope.SetTag("userId", userID)
			scope.SetTag("room", roomID)
			scope.SetLevel(sentry.LevelError)
			sentry.CaptureException(err)
		})
		return err
	}
	return nil
}

// GetRoomMembers retrieves all members of a room
func (rs *RedisStore) GetRoomMembers(roomID string) ([]string, error) {
	ctx, cancel := context.WithTimeout(rs.ctx, 2*time.Second)
	defer cancel()

	members, err := rs.client.SMembers(ctx, fmt.Sprintf("room:members:%s", roomID)).Result()
	if err != nil {
		log.Printf("Redis error getting members for room %s: %v", roomID, err)
		return []string{}, err
	}
	return members, nil
}

// GetUserRooms retrieves all rooms a user is in
func (rs *RedisStore) GetUserRooms(userID string) ([]string, error) {
	ctx, cancel := context.WithTimeout(rs.ctx, 2*time.Second)
	defer cancel()

	rooms, err := rs.client.SMembers(ctx, fmt.Sprintf("user:rooms:%s", userID)).Result()
	if err != nil {
		log.Printf("Redis error getting rooms for user %s: %v", userID, err)
		return []string{}, err
	}
	return rooms, nil
}

// Message Persistence

// SaveRoomMessage saves a message to a room's message history with TTL
func (rs *RedisStore) SaveRoomMessage(roomID string, message Message) error {
	ctx, cancel := context.WithTimeout(rs.ctx, 2*time.Second)
	defer cancel()

	// Marshal message to JSON
	messageJSON, err := json.Marshal(message)
	if err != nil {
		log.Printf("Error marshalling message for Redis: %v", err)

		// Capture marshalling error
		sentry.WithScope(func(scope *sentry.Scope) {
			scope.SetTag("operation", "redis_save_room_message_marshal")
			scope.SetTag("room", roomID)
			scope.SetLevel(sentry.LevelError)
			sentry.CaptureException(err)
		})
		return err
	}

	// Parse timestamp for score
	timestamp, err := time.Parse(time.RFC3339Nano, message.CreateTime)
	if err != nil {
		timestamp = time.Now()
	}
	score := float64(timestamp.UnixNano())

	key := fmt.Sprintf("room:messages:%s", roomID)

	// Add to sorted set with timestamp as score
	if err := rs.client.ZAdd(ctx, key, redis.Z{
		Score:  score,
		Member: string(messageJSON),
	}).Err(); err != nil {
		log.Printf("Redis error saving room message to %s: %v", roomID, err)

		// Capture Redis operation error
		sentry.WithScope(func(scope *sentry.Scope) {
			scope.SetTag("operation", "redis_save_room_message")
			scope.SetTag("room", roomID)
			scope.SetLevel(sentry.LevelError)
			sentry.CaptureException(err)
		})
		return err
	}

	// Set TTL on the key
	if err := rs.client.Expire(ctx, key, rs.messageTTL).Err(); err != nil {
		log.Printf("Redis error setting TTL for room %s: %v", roomID, err)

		// Capture TTL error
		sentry.WithScope(func(scope *sentry.Scope) {
			scope.SetTag("operation", "redis_set_ttl_room")
			scope.SetTag("room", roomID)
			scope.SetLevel(sentry.LevelWarning)
			sentry.CaptureException(err)
		})
		return err
	}

	return nil
}

// GetRoomMessages retrieves recent messages from a room
func (rs *RedisStore) GetRoomMessages(roomID string, limit int64, beforeTimestamp *time.Time) ([]Message, error) {
	ctx, cancel := context.WithTimeout(rs.ctx, 2*time.Second)
	defer cancel()

	key := fmt.Sprintf("room:messages:%s", roomID)

	// Determine the range
	maxScore := "+inf"
	if beforeTimestamp != nil {
		maxScore = fmt.Sprintf("%d", beforeTimestamp.UnixNano())
	}

	// Get messages in reverse order (newest first)
	results, err := rs.client.ZRevRangeByScore(ctx, key, &redis.ZRangeBy{
		Min:   "-inf",
		Max:   maxScore,
		Count: limit,
	}).Result()

	if err != nil {
		log.Printf("Redis error getting messages for room %s: %v", roomID, err)
		return []Message{}, err
	}

	messages := make([]Message, 0, len(results))
	for _, result := range results {
		var msg Message
		if err := json.Unmarshal([]byte(result), &msg); err != nil {
			log.Printf("Error unmarshalling message from Redis: %v", err)
			continue
		}
		messages = append(messages, msg)
	}

	return messages, nil
}

// SaveDirectMessage saves a direct message between two users with TTL
func (rs *RedisStore) SaveDirectMessage(fromUser, toUser string, message Message) error {
	ctx, cancel := context.WithTimeout(rs.ctx, 2*time.Second)
	defer cancel()

	// Create a consistent key by sorting user IDs
	users := []string{fromUser, toUser}
	sort.Strings(users)
	key := fmt.Sprintf("dm:%s:%s", users[0], users[1])

	// Marshal message to JSON
	messageJSON, err := json.Marshal(message)
	if err != nil {
		log.Printf("Error marshalling direct message for Redis: %v", err)

		// Capture marshalling error
		sentry.WithScope(func(scope *sentry.Scope) {
			scope.SetTag("operation", "redis_save_dm_marshal")
			scope.SetTag("fromUser", fromUser)
			scope.SetTag("toUser", toUser)
			scope.SetLevel(sentry.LevelError)
			sentry.CaptureException(err)
		})
		return err
	}

	// Add to list (right push for chronological order)
	if err := rs.client.RPush(ctx, key, string(messageJSON)).Err(); err != nil {
		log.Printf("Redis error saving direct message: %v", err)

		// Capture Redis operation error
		sentry.WithScope(func(scope *sentry.Scope) {
			scope.SetTag("operation", "redis_save_dm")
			scope.SetTag("fromUser", fromUser)
			scope.SetTag("toUser", toUser)
			scope.SetLevel(sentry.LevelError)
			sentry.CaptureException(err)
		})
		return err
	}

	// Set TTL on the key
	if err := rs.client.Expire(ctx, key, rs.messageTTL).Err(); err != nil {
		log.Printf("Redis error setting TTL for DM key %s: %v", key, err)

		// Capture TTL error
		sentry.WithScope(func(scope *sentry.Scope) {
			scope.SetTag("operation", "redis_set_ttl_dm")
			scope.SetTag("fromUser", fromUser)
			scope.SetTag("toUser", toUser)
			scope.SetLevel(sentry.LevelWarning)
			sentry.CaptureException(err)
		})
		return err
	}

	return nil
}

// GetDirectMessages retrieves direct messages between two users
func (rs *RedisStore) GetDirectMessages(user1, user2 string, limit int64) ([]Message, error) {
	ctx, cancel := context.WithTimeout(rs.ctx, 2*time.Second)
	defer cancel()

	// Create consistent key
	users := []string{user1, user2}
	sort.Strings(users)
	key := fmt.Sprintf("dm:%s:%s", users[0], users[1])

	// Get messages (most recent first)
	start := int64(-1) * limit
	if start < -1 {
		// Get last N messages
		results, err := rs.client.LRange(ctx, key, start, -1).Result()
		if err != nil {
			log.Printf("Redis error getting direct messages: %v", err)
			return []Message{}, err
		}

		messages := make([]Message, 0, len(results))
		for _, result := range results {
			var msg Message
			if err := json.Unmarshal([]byte(result), &msg); err != nil {
				log.Printf("Error unmarshalling direct message from Redis: %v", err)
				continue
			}
			messages = append(messages, msg)
		}

		return messages, nil
	}

	// Get all messages if limit is not specified or too small
	results, err := rs.client.LRange(ctx, key, 0, -1).Result()
	if err != nil {
		log.Printf("Redis error getting direct messages: %v", err)
		return []Message{}, err
	}

	messages := make([]Message, 0, len(results))
	for _, result := range results {
		var msg Message
		if err := json.Unmarshal([]byte(result), &msg); err != nil {
			log.Printf("Error unmarshalling direct message from Redis: %v", err)
			continue
		}
		messages = append(messages, msg)
	}

	// Return only the last N messages if needed
	if limit > 0 && int64(len(messages)) > limit {
		return messages[len(messages)-int(limit):], nil
	}

	return messages, nil
}

// WebRTC Available Users Pool

// AddUserToAvailable adds a user to the available-for-call pool
func (rs *RedisStore) AddUserToAvailable(userID string) error {
	ctx, cancel := context.WithTimeout(rs.ctx, 2*time.Second)
	defer cancel()

	if err := rs.client.SAdd(ctx, "available:webrtc", userID).Err(); err != nil {
		log.Printf("Redis error adding user %s to available pool: %v", userID, err)

		// Capture Redis operation error
		sentry.WithScope(func(scope *sentry.Scope) {
			scope.SetTag("operation", "redis_add_to_available")
			scope.SetTag("userId", userID)
			scope.SetLevel(sentry.LevelError)
			sentry.CaptureException(err)
		})
		return err
	}

	// Add breadcrumb for availability change
	sentry.AddBreadcrumb(&sentry.Breadcrumb{
		Category:  "redis",
		Message:   "User added to available pool",
		Level:     sentry.LevelInfo,
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"userId": userID,
		},
	})
	return nil
}

// RemoveUserFromAvailable removes a user from the available-for-call pool
func (rs *RedisStore) RemoveUserFromAvailable(userID string) error {
	ctx, cancel := context.WithTimeout(rs.ctx, 2*time.Second)
	defer cancel()

	if err := rs.client.SRem(ctx, "available:webrtc", userID).Err(); err != nil {
		log.Printf("Redis error removing user %s from available pool: %v", userID, err)

		// Capture Redis operation error
		sentry.WithScope(func(scope *sentry.Scope) {
			scope.SetTag("operation", "redis_remove_from_available")
			scope.SetTag("userId", userID)
			scope.SetLevel(sentry.LevelError)
			sentry.CaptureException(err)
		})
		return err
	}
	return nil
}

// GetRandomAvailablePeer gets a random peer from the available pool, excluding specified users
func (rs *RedisStore) GetRandomAvailablePeer(excludeUsers ...string) (string, error) {
	ctx, cancel := context.WithTimeout(rs.ctx, 2*time.Second)
	defer cancel()

	// Get all available users
	available, err := rs.client.SMembers(ctx, "available:webrtc").Result()
	if err != nil {
		log.Printf("Redis error getting available users: %v", err)

		// Capture Redis operation error
		sentry.WithScope(func(scope *sentry.Scope) {
			scope.SetTag("operation", "redis_get_available_peers")
			scope.SetLevel(sentry.LevelError)
			sentry.CaptureException(err)
		})
		return "", err
	}

	// Filter out excluded users
	excludeMap := make(map[string]bool)
	for _, user := range excludeUsers {
		excludeMap[user] = true
	}

	possiblePeers := []string{}
	for _, user := range available {
		if !excludeMap[user] {
			possiblePeers = append(possiblePeers, user)
		}
	}

	if len(possiblePeers) == 0 {
		// Add breadcrumb for no peers available
		sentry.AddBreadcrumb(&sentry.Breadcrumb{
			Category:  "redis",
			Message:   "No available peers found",
			Level:     sentry.LevelInfo,
			Timestamp: time.Now(),
			Data: map[string]interface{}{
				"totalAvailable": len(available),
				"excludedCount":  len(excludeUsers),
			},
		})
		return "", fmt.Errorf("no available peers")
	}

	// Return random peer (using the same rand that was seeded in init())
	selectedPeer := possiblePeers[rand.Intn(len(possiblePeers))]

	// Add breadcrumb for peer selection
	sentry.AddBreadcrumb(&sentry.Breadcrumb{
		Category:  "redis",
		Message:   "Random peer selected",
		Level:     sentry.LevelInfo,
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"selectedPeer":   selectedPeer,
			"possiblePeers":  len(possiblePeers),
			"totalAvailable": len(available),
		},
	})

	return selectedPeer, nil
}

// GetAvailableCount returns the count of users available for calls
func (rs *RedisStore) GetAvailableCount() (int64, error) {
	ctx, cancel := context.WithTimeout(rs.ctx, 2*time.Second)
	defer cancel()

	count, err := rs.client.SCard(ctx, "available:webrtc").Result()
	if err != nil {
		log.Printf("Redis error getting available count: %v", err)
		return 0, err
	}
	return count, nil
}
