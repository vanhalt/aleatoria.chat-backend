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

// TestHealthCheckHandler tests the /health endpoint.
func TestHealthCheckHandler(t *testing.T) {
	req, err := http.NewRequest("GET", "/health", nil)
	if err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(healthCheckHandler)

	handler.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusOK)
	}

	expected := `{"status":"ok"}`
	// Using strings.TrimSpace to remove any trailing newline characters from the response body
	if strings.TrimSpace(rr.Body.String()) != expected {
		t.Errorf("handler returned unexpected body: got %v want %v",
			rr.Body.String(), expected)
	}
}

// Helper function to create a test server and a websocket client connected to it.
func newTestServerAndClient(t *testing.T, hub *Hub, room, userID string) (*httptest.Server, *websocket.Conn) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serveWs(hub, w, r)
	}))

	// The dialer needs to have an Origin header set that is allowed by the server's CheckOrigin function.
	// Based on the passing TestValidateOrigin tests, "https://www.aleatoria.chat" is an allowed origin.
	dialer := websocket.Dialer{}
	header := http.Header{}
	header.Add("Origin", "https://www.aleatoria.chat")

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws?room=" + room + "&userId=" + userID
	ws, _, err := dialer.Dial(wsURL, header)
	if err != nil {
		t.Fatalf("Failed to connect to websocket: %v", err)
	}

	return server, ws
}

// TestServeWsConnection tests if a client can successfully connect via WebSocket.
func TestServeWsConnection(t *testing.T) {
	hub := newHub()
	go hub.run()

	server, ws := newTestServerAndClient(t, hub, "test_room", "user1")
	defer server.Close()
	defer ws.Close()

	if ws == nil {
		t.Fatal("WebSocket connection should not be nil")
	}

	// Give some time for the client to register
	time.Sleep(100 * time.Millisecond)

	// Simple check to see if client was registered
	hub.mu.Lock()
	if len(hub.clients) != 1 {
		t.Errorf("Expected 1 client to be registered, got %d", len(hub.clients))
	}
	if _, ok := hub.clientsByID["user1"]; !ok {
		t.Error("Client 'user1' not found in clientsByID map")
	}
	hub.mu.Unlock()
}

// Helper function to read the next message, skipping user list updates
func readNextMessage(t *testing.T, ws *websocket.Conn, timeout time.Duration) Message {
	ws.SetReadDeadline(time.Now().Add(timeout))
	defer ws.SetReadDeadline(time.Time{}) // Reset deadline
	var msg Message
	for {
		if err := ws.ReadJSON(&msg); err != nil {
			return Message{} // Return empty on timeout or error
		}
		if msg.Type != "update_user_list" {
			return msg
		}
	}
}

// TestWebSocketChatMessage tests the sending and receiving of chat messages.
func TestWebSocketChatMessage(t *testing.T) {
	hub := newHub()
	go hub.run()

	// Client 1
	server1, ws1 := newTestServerAndClient(t, hub, "chat_room", "user1")
	defer server1.Close()
	defer ws1.Close()

	// Client 2
	server2, ws2 := newTestServerAndClient(t, hub, "chat_room", "user2")
	defer server2.Close()
	defer ws2.Close()

	// Clear initial user list updates
	readUserListUpdate(t, ws1, 200*time.Millisecond) // user1 connects
	readUserListUpdate(t, ws1, 200*time.Millisecond) // user2 connects
	readUserListUpdate(t, ws2, 200*time.Millisecond) // user2 connects

	// User 1 sends a message
	chatMsg := Message{
		Type: "chat_message",
		Room: "chat_room",
		Payload: ChatPayload{
			Content: "Hello, world!",
		},
	}
	if err := ws1.WriteJSON(chatMsg); err != nil {
		t.Fatalf("Failed to write message: %v", err)
	}

	// User 2 should receive the message
	receivedMsg := readNextMessage(t, ws2, 200*time.Millisecond)

	if receivedMsg.Type != "chat_message" {
		t.Errorf("Expected message type 'chat_message', got '%s'", receivedMsg.Type)
	}
	if receivedMsg.FromUser != "user1" {
		t.Errorf("Expected message from 'user1', got '%s'", receivedMsg.FromUser)
	}

	payload, ok := receivedMsg.Payload.(map[string]interface{})
	if !ok {
		t.Fatalf("Could not assert payload type")
	}
	if payload["content"] != "Hello, world!" {
		t.Errorf("Unexpected chat content: got '%s', want 'Hello, world!'", payload["content"])
	}
}

// TestWebRTC_PeerToPeerFlow tests the full peer-to-peer negotiation flow.
func TestWebRTC_PeerToPeerFlow(t *testing.T) {
	hub := newHub()
	go hub.run()

	// Client 1 (user1)
	server1, ws1 := newTestServerAndClient(t, hub, "webrtc_room", "user1")
	defer server1.Close()
	defer ws1.Close()

	// Client 2 (user2)
	server2, ws2 := newTestServerAndClient(t, hub, "webrtc_room", "user2")
	defer server2.Close()
	defer ws2.Close()

	// Clear initial user list updates
	readUserListUpdate(t, ws1, 200*time.Millisecond) // user1 connects
	readUserListUpdate(t, ws1, 200*time.Millisecond) // user2 connects
	readUserListUpdate(t, ws2, 200*time.Millisecond) // user2 connects

	// Step 1: Both users mark themselves as available for WebRTC
	availableMsg := Message{Type: "user_available_webrtc"}
	ws1.WriteJSON(availableMsg)
	ws2.WriteJSON(availableMsg)
	time.Sleep(100 * time.Millisecond) // Allow processing

	// Step 2: User1 requests a random peer
	requestMsg := Message{Type: "request_random_peer"}
	ws1.WriteJSON(requestMsg)

	// Step 3: Verify both clients receive 'assign_role' messages
	msg1 := readNextMessage(t, ws1, 200*time.Millisecond)
	if msg1.Type != "assign_role" {
		t.Errorf("User1 expected 'assign_role', got '%s'", msg1.Type)
	}
	payload1, _ := msg1.Payload.(map[string]interface{})
	if payload1["peerId"] != "user2" {
		t.Errorf("User1 expected peerId 'user2', got '%s'", payload1["peerId"])
	}

	msg2 := readNextMessage(t, ws2, 200*time.Millisecond)
	if msg2.Type != "assign_role" {
		t.Errorf("User2 expected 'assign_role', got '%s'", msg2.Type)
	}
	payload2, _ := msg2.Payload.(map[string]interface{})
	if payload2["peerId"] != "user1" {
		t.Errorf("User2 expected peerId 'user1', got '%s'", payload2["peerId"])
	}

	// Step 4: Test WebRTC offer/answer/candidate exchange
	// User1 (impolite, so should send offer) sends an offer
	offerPayload := WebRTCSessionDescriptionPayload{Type: "offer", Sdp: "test_sdp_offer"}
	offerMsg := Message{Type: "webrtc_offer", ToUser: "user2", Payload: offerPayload}
	ws1.WriteJSON(offerMsg)

	// User2 should receive the offer
	receivedOffer := readNextMessage(t, ws2, 200*time.Millisecond)
	if receivedOffer.Type != "webrtc_offer" {
		t.Errorf("User2 expected 'webrtc_offer', got '%s'", receivedOffer.Type)
	}

	// User2 sends an answer
	answerPayload := WebRTCSessionDescriptionPayload{Type: "answer", Sdp: "test_sdp_answer"}
	answerMsg := Message{Type: "webrtc_answer", ToUser: "user1", Payload: answerPayload}
	ws2.WriteJSON(answerMsg)

	// User1 should receive the answer
	receivedAnswer := readNextMessage(t, ws1, 200*time.Millisecond)
	if receivedAnswer.Type != "webrtc_answer" {
		t.Errorf("User1 expected 'webrtc_answer', got '%s'", receivedAnswer.Type)
	}

	// User1 sends an ICE candidate
	candidatePayload := WebRTCIceCandidatePayload{Candidate: "test_candidate"}
	candidateMsg := Message{Type: "webrtc_candidate", ToUser: "user2", Payload: candidatePayload}
	ws1.WriteJSON(candidateMsg)

	// User2 should receive the ICE candidate
	receivedCandidate := readNextMessage(t, ws2, 200*time.Millisecond)
	if receivedCandidate.Type != "webrtc_candidate" {
		t.Errorf("User2 expected 'webrtc_candidate', got '%s'", receivedCandidate.Type)
	}
}

func TestPrivateMessage(t *testing.T) {
	hub := newHub()
	go hub.run()

	// Client 1
	server1, ws1 := newTestServerAndClient(t, hub, "private_chat", "user1")
	defer server1.Close()
	defer ws1.Close()

	// Client 2
	server2, ws2 := newTestServerAndClient(t, hub, "private_chat", "user2")
	defer server2.Close()
	defer ws2.Close()

	// Client 3
	server3, ws3 := newTestServerAndClient(t, hub, "private_chat", "user3")
	defer server3.Close()
	defer ws3.Close()

	// Clear initial user list updates for all clients
	readUserListUpdate(t, ws1, 200*time.Millisecond) // user1 connects
	readUserListUpdate(t, ws1, 200*time.Millisecond) // user2 connects
	readUserListUpdate(t, ws2, 200*time.Millisecond) // user2 connects
	readUserListUpdate(t, ws1, 200*time.Millisecond) // user3 connects
	readUserListUpdate(t, ws2, 200*time.Millisecond) // user3 connects
	readUserListUpdate(t, ws3, 200*time.Millisecond) // user3 connects

	// User 1 sends a private message to User 2
	privateMsg := Message{
		Type:   "private_message",
		ToUser: "user2",
		Payload: ChatPayload{
			Content: "This is a private message.",
		},
	}
	if err := ws1.WriteJSON(privateMsg); err != nil {
		t.Fatalf("Failed to write private message: %v", err)
	}

	// User 2 should receive the message
	receivedMsg := readNextMessage(t, ws2, 200*time.Millisecond)
	if receivedMsg.Type != "private_message" {
		t.Errorf("Expected message type 'private_message', got '%s'", receivedMsg.Type)
	}
	if receivedMsg.FromUser != "user1" {
		t.Errorf("Expected message from 'user1', got '%s'", receivedMsg.FromUser)
	}
	payload, ok := receivedMsg.Payload.(map[string]interface{})
	if !ok {
		t.Fatalf("Could not assert payload type")
	}
	if payload["content"] != "This is a private message." {
		t.Errorf("Unexpected private message content: got '%s'", payload["content"])
	}

	// User 3 should not receive the private message.
	// We'll read with a timeout and ensure no 'private_message' type comes through.
	ws3.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	for {
		discardedMsg := readNextMessage(t, ws3, 100*time.Millisecond)
		if discardedMsg.Type == "" { // This indicates a read error, likely a timeout
			break // Correctly timed out
		}
		if discardedMsg.Type == "private_message" {
			t.Errorf("User 3 unexpectedly received a private message: %+v", discardedMsg)
			break
		}
		// It's okay if they receive other messages, like user list updates.
	}
}

func TestUserListBroadcast(t *testing.T) {
	hub := newHub()
	go hub.run()
	timeout := 200 * time.Millisecond

	// Client 1
	server1, ws1 := newTestServerAndClient(t, hub, "user_list_test", "user1")
	defer server1.Close()
	defer ws1.Close()

	// Client 1 should receive a user list with just itself
	msg1 := readUserListUpdate(t, ws1, timeout)
	if len(msg1.Payload.(UserListPayload).Users) != 1 {
		t.Errorf("Expected 1 user, got %d", len(msg1.Payload.(UserListPayload).Users))
	}

	// Client 2
	server2, ws2 := newTestServerAndClient(t, hub, "user_list_test", "user2")
	defer server2.Close()
	defer ws2.Close()

	// Both clients should receive an updated user list
	msg1_update := readUserListUpdate(t, ws1, timeout)
	if len(msg1_update.Payload.(UserListPayload).Users) != 2 {
		t.Errorf("Expected 2 users, got %d", len(msg1_update.Payload.(UserListPayload).Users))
	}
	msg2_update := readUserListUpdate(t, ws2, timeout)
	if len(msg2_update.Payload.(UserListPayload).Users) != 2 {
		t.Errorf("Expected 2 users, got %d", len(msg2_update.Payload.(UserListPayload).Users))
	}

	// Disconnect User 2
	ws2.Close()

	// User 1 should receive another update
	msg1_final := readUserListUpdate(t, ws1, timeout)
	if len(msg1_final.Payload.(UserListPayload).Users) != 1 {
		t.Errorf("Expected 1 user, got %d", len(msg1_final.Payload.(UserListPayload).Users))
	}
}

// Helper function to specifically read a user list update message
func readUserListUpdate(t *testing.T, ws *websocket.Conn, timeout time.Duration) Message {
	ws.SetReadDeadline(time.Now().Add(timeout))
	defer ws.SetReadDeadline(time.Time{}) // Reset deadline
	var msg Message
	for {
		if err := ws.ReadJSON(&msg); err != nil {
			t.Fatalf("Failed to read message: %v", err)
		}
		if msg.Type == "update_user_list" {
			// Unmarshal payload to ensure it's the correct type
			var payload UserListPayload
			payloadBytes, _ := json.Marshal(msg.Payload)
			if err := json.Unmarshal(payloadBytes, &payload); err != nil {
				t.Fatalf("Failed to unmarshal UserListPayload: %v", err)
			}
			msg.Payload = payload // Replace the generic map with the struct
			return msg
		}
	}
}
