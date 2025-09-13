package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestDirectMessageFlow(t *testing.T) {
	hub := newHub()
	go hub.run()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serveWs(hub, w, r)
	}))
	defer server.Close()

	// Convert http:// to ws://
	u := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"

	// Connect user1
	dialer1 := websocket.DefaultDialer
	header1 := http.Header{}
	header1.Set("Origin", "https://www.aleatoria.chat")
	ws1, _, err := dialer1.Dial(u+"?userId=user1&room=test_room", header1)
	if err != nil {
		t.Fatalf("user1 dial error: %v", err)
	}
	defer ws1.Close()

	// Connect user2
	dialer2 := websocket.DefaultDialer
	header2 := http.Header{}
	header2.Set("Origin", "https://www.aleatoria.chat")
	ws2, _, err := dialer2.Dial(u+"?userId=user2&room=test_room", header2)
	if err != nil {
		t.Fatalf("user2 dial error: %v", err)
	}
	defer ws2.Close()

	// Give connections time to register
	time.Sleep(100 * time.Millisecond)

	// User1 sends a DM to User2
	dmMessage := Message{
		Type:     "chat_message",
		ToUser:   "user2", // This makes it a DM
		Payload:  ChatPayload{Content: "Hello User2, this is a private message!"},
		Incoming: false,
	}

	if err := ws1.WriteJSON(dmMessage); err != nil {
		t.Fatalf("user1 write error: %v", err)
	}

	// User2 should receive the DM
	var receivedMsg Message
	ws2.SetReadDeadline(time.Now().Add(2 * time.Second))
	if err := ws2.ReadJSON(&receivedMsg); err != nil {
		t.Fatalf("user2 read error: %v", err)
	}

	// Verify the received message
	if receivedMsg.Type != "chat_message" {
		t.Errorf("Expected message type 'chat_message', got '%s'", receivedMsg.Type)
	}
	if receivedMsg.FromUser != "user1" {
		t.Errorf("Expected fromUser 'user1', got '%s'", receivedMsg.FromUser)
	}
	if receivedMsg.ToUser != "user2" {
		t.Errorf("Expected toUser 'user2', got '%s'", receivedMsg.ToUser)
	}
	if receivedMsg.Room != "" {
		t.Errorf("Expected no room for DM, got '%s'", receivedMsg.Room)
	}

	// Check payload
	payloadBytes, _ := json.Marshal(receivedMsg.Payload)
	var payload ChatPayload
	json.Unmarshal(payloadBytes, &payload)
	if payload.Content != "Hello User2, this is a private message!" {
		t.Errorf("Expected DM content, got '%s'", payload.Content)
	}

	// User1 should also receive a copy of their sent message
	var senderCopy Message
	ws1.SetReadDeadline(time.Now().Add(2 * time.Second))
	if err := ws1.ReadJSON(&senderCopy); err != nil {
		t.Fatalf("user1 read error for sender copy: %v", err)
	}

	// Verify sender copy
	if senderCopy.Incoming != false {
		t.Errorf("Expected sender copy to have incoming=false")
	}
	if senderCopy.ToUser != "user2" {
		t.Errorf("Expected sender copy to have toUser 'user2', got '%s'", senderCopy.ToUser)
	}

	t.Log("Direct message test passed!")
}
