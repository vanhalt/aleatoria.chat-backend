package main

import (
	"os"
	"testing"
	"time"
)

// TestRedisStoreCreation tests that RedisStore can be created
func TestRedisStoreCreation(t *testing.T) {
	// Set environment variables for testing
	os.Setenv("VALKEY_HOST", "localhost")
	os.Setenv("VALKEY_PORT", "6379")
	os.Setenv("MESSAGE_TTL_HOURS", "24")

	store := NewRedisStore()
	if store == nil {
		t.Fatal("RedisStore should not be nil")
	}

	if store.messageTTL != 24*time.Hour {
		t.Errorf("Expected messageTTL to be 24 hours, got %v", store.messageTTL)
	}

	// Clean up
	store.Close()
}

// TestRedisRoomOperations tests room membership operations
// Note: This test will fail if Redis is not running, but should gracefully handle it
func TestRedisRoomOperations(t *testing.T) {
	store := NewRedisStore()
	defer store.Close()

	// Test adding user to room
	err := store.AddUserToRoom("test_room", "user123")
	if err != nil {
		t.Logf("Warning: Redis may not be available: %v", err)
		t.Skip("Skipping test as Redis is not available")
	}

	// Test getting room members
	members, err := store.GetRoomMembers("test_room")
	if err != nil {
		t.Logf("Warning: Redis may not be available: %v", err)
		t.Skip("Skipping test as Redis is not available")
	}

	found := false
	for _, member := range members {
		if member == "user123" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected user123 to be in room members")
	}

	// Test getting user rooms
	rooms, err := store.GetUserRooms("user123")
	if err != nil {
		t.Logf("Warning: Redis may not be available: %v", err)
		t.Skip("Skipping test as Redis is not available")
	}

	found = false
	for _, room := range rooms {
		if room == "test_room" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected test_room to be in user's rooms")
	}

	// Test removing user from room
	err = store.RemoveUserFromRoom("test_room", "user123")
	if err != nil {
		t.Errorf("Failed to remove user from room: %v", err)
	}

	// Verify removal
	members, _ = store.GetRoomMembers("test_room")
	for _, member := range members {
		if member == "user123" {
			t.Error("user123 should have been removed from room")
		}
	}
}

// TestRedisMessagePersistence tests message storage and retrieval
func TestRedisMessagePersistence(t *testing.T) {
	store := NewRedisStore()
	defer store.Close()

	testMsg := Message{
		Type:       "chat_message",
		FromUser:   "user1",
		Room:       "test_room",
		Payload:    ChatPayload{Content: "Hello Redis!"},
		MessageID:  "msg123",
		CreateTime: time.Now().Format(time.RFC3339Nano),
	}

	// Test saving room message
	err := store.SaveRoomMessage("test_room", testMsg)
	if err != nil {
		t.Logf("Warning: Redis may not be available: %v", err)
		t.Skip("Skipping test as Redis is not available")
	}

	// Test retrieving room messages
	messages, err := store.GetRoomMessages("test_room", 10, nil)
	if err != nil {
		t.Errorf("Failed to retrieve room messages: %v", err)
	}

	if len(messages) == 0 {
		t.Error("Expected at least one message in room")
	}

	// Verify message content
	found := false
	for _, msg := range messages {
		if msg.MessageID == "msg123" {
			found = true
			if payload, ok := msg.Payload.(map[string]interface{}); ok {
				if content, ok := payload["content"].(string); ok {
					if content != "Hello Redis!" {
						t.Errorf("Expected content 'Hello Redis!', got '%s'", content)
					}
				}
			}
			break
		}
	}
	if !found {
		t.Error("Expected to find message with ID msg123")
	}
}

// TestRedisDirectMessages tests direct message storage
func TestRedisDirectMessages(t *testing.T) {
	store := NewRedisStore()
	defer store.Close()

	testMsg := Message{
		Type:       "chat_message",
		FromUser:   "user1",
		ToUser:     "user2",
		Payload:    ChatPayload{Content: "Private message"},
		MessageID:  "dm123",
		CreateTime: time.Now().Format(time.RFC3339Nano),
	}

	// Test saving direct message
	err := store.SaveDirectMessage("user1", "user2", testMsg)
	if err != nil {
		t.Logf("Warning: Redis may not be available: %v", err)
		t.Skip("Skipping test as Redis is not available")
	}

	// Test retrieving direct messages
	messages, err := store.GetDirectMessages("user1", "user2", 10)
	if err != nil {
		t.Errorf("Failed to retrieve direct messages: %v", err)
	}

	if len(messages) == 0 {
		t.Error("Expected at least one direct message")
	}

	// Verify the order is consistent (should work from either user's perspective)
	messages2, err := store.GetDirectMessages("user2", "user1", 10)
	if err != nil {
		t.Errorf("Failed to retrieve direct messages from reverse perspective: %v", err)
	}

	if len(messages) != len(messages2) {
		t.Error("Direct messages should be the same regardless of query order")
	}
}

// TestRedisAvailableUsers tests the available-for-call pool
func TestRedisAvailableUsers(t *testing.T) {
	store := NewRedisStore()
	defer store.Close()

	// Test adding user to available pool
	err := store.AddUserToAvailable("user1")
	if err != nil {
		t.Logf("Warning: Redis may not be available: %v", err)
		t.Skip("Skipping test as Redis is not available")
	}

	err = store.AddUserToAvailable("user2")
	if err != nil {
		t.Errorf("Failed to add user2 to available pool: %v", err)
	}

	// Test getting available count
	count, err := store.GetAvailableCount()
	if err != nil {
		t.Errorf("Failed to get available count: %v", err)
	}

	if count < 2 {
		t.Errorf("Expected at least 2 available users, got %d", count)
	}

	// Test getting random available peer
	peer, err := store.GetRandomAvailablePeer("user1") // Exclude user1
	if err != nil {
		t.Errorf("Failed to get random peer: %v", err)
	}

	if peer == "user1" {
		t.Error("Random peer should not be user1 (was excluded)")
	}

	if peer != "user2" {
		t.Logf("Got peer: %s (may be from previous test runs)", peer)
	}

	// Test removing user from available pool
	err = store.RemoveUserFromAvailable("user1")
	if err != nil {
		t.Errorf("Failed to remove user from available pool: %v", err)
	}

	err = store.RemoveUserFromAvailable("user2")
	if err != nil {
		t.Errorf("Failed to remove user2 from available pool: %v", err)
	}

	// Verify removal
	count, _ = store.GetAvailableCount()
	t.Logf("Available count after removal: %d", count)
}

// TestRedisGracefulDegradation tests that the system continues working when Redis is unavailable
func TestRedisGracefulDegradation(t *testing.T) {
	// Create store with invalid Redis configuration
	os.Setenv("VALKEY_HOST", "invalid-host")
	os.Setenv("VALKEY_PORT", "9999")
	store := NewRedisStore()
	defer store.Close()

	// Operations should fail gracefully without crashing
	err := store.AddUserToRoom("test_room", "user1")
	if err != nil {
		t.Logf("Expected error when Redis is unavailable: %v", err)
	}

	// Getting data should return empty results
	messages, err := store.GetRoomMessages("test_room", 10, nil)
	if err != nil {
		t.Logf("Expected error when Redis is unavailable: %v", err)
	}
	if len(messages) != 0 {
		t.Error("Expected empty messages when Redis is unavailable")
	}

	// Reset to valid configuration for other tests
	os.Setenv("VALKEY_HOST", "localhost")
	os.Setenv("VALKEY_PORT", "6379")
}

// TestHubWithRedis tests that Hub properly integrates with RedisStore
func TestHubWithRedis(t *testing.T) {
	hub := newHub()
	if hub.redisStore == nil {
		t.Fatal("Hub should have a RedisStore initialized")
	}

	// Test that hub operations don't panic when Redis is used
	go hub.run()

	server1, ws1 := newTestServerAndClient(t, hub, "redis_test_room", "redis_user1")
	defer server1.Close()
	defer ws1.Close()

	time.Sleep(100 * time.Millisecond)

	// Send a message
	chatMsg := Message{
		Type: "chat_message",
		Room: "redis_test_room",
		Payload: ChatPayload{
			Content: "Testing Redis integration",
		},
	}
	if err := ws1.WriteJSON(chatMsg); err != nil {
		t.Fatalf("Failed to write message: %v", err)
	}

	// Read the message back
	var receivedMsg Message
	err := ws1.ReadJSON(&receivedMsg)
	if err != nil {
		t.Fatalf("Failed to read message: %v", err)
	}

	// Give Redis time to persist
	time.Sleep(100 * time.Millisecond)

	// Try to retrieve messages from Redis
	// Note: This will fail gracefully if Redis is not available
	messages, err := hub.redisStore.GetRoomMessages("redis_test_room", 10, nil)
	if err != nil {
		t.Logf("Warning: Could not retrieve messages from Redis: %v", err)
	} else {
		t.Logf("Successfully retrieved %d messages from Redis", len(messages))
	}
}
