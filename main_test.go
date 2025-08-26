package main

import (
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

	// Give some time for clients to register
	time.Sleep(100 * time.Millisecond)

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
	var receivedMsg Message
	err := ws2.ReadJSON(&receivedMsg)
	if err != nil {
		t.Fatalf("Failed to read message: %v", err)
	}

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

	time.Sleep(100 * time.Millisecond) // Allow registration

	// Step 1: Both users mark themselves as available for WebRTC
	availableMsg := Message{Type: "user_available_webrtc"}
	ws1.WriteJSON(availableMsg)
	ws2.WriteJSON(availableMsg)
	time.Sleep(100 * time.Millisecond) // Allow processing

	// Step 2: User1 requests a random peer
	requestMsg := Message{Type: "request_random_peer"}
	ws1.WriteJSON(requestMsg)

	// Step 3: Verify both clients receive 'assign_role' messages
	var msg1, msg2 Message

	// User 1 receives role assignment
	if err := ws1.ReadJSON(&msg1); err != nil {
		t.Fatalf("User1 failed to read assign_role message: %v", err)
	}
	if msg1.Type != "assign_role" {
		t.Errorf("User1 expected 'assign_role', got '%s'", msg1.Type)
	}
	payload1, _ := msg1.Payload.(map[string]interface{})
	if payload1["peerId"] != "user2" {
		t.Errorf("User1 expected peerId 'user2', got '%s'", payload1["peerId"])
	}

	// User 2 receives role assignment
	if err := ws2.ReadJSON(&msg2); err != nil {
		t.Fatalf("User2 failed to read assign_role message: %v", err)
	}
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
	var receivedOffer Message
	if err := ws2.ReadJSON(&receivedOffer); err != nil {
		t.Fatalf("User2 failed to read offer: %v", err)
	}
	if receivedOffer.Type != "webrtc_offer" {
		t.Errorf("User2 expected 'webrtc_offer', got '%s'", receivedOffer.Type)
	}

	// User2 sends an answer
	answerPayload := WebRTCSessionDescriptionPayload{Type: "answer", Sdp: "test_sdp_answer"}
	answerMsg := Message{Type: "webrtc_answer", ToUser: "user1", Payload: answerPayload}
	ws2.WriteJSON(answerMsg)

	// User1 should receive the answer
	var receivedAnswer Message
	if err := ws1.ReadJSON(&receivedAnswer); err != nil {
		t.Fatalf("User1 failed to read answer: %v", err)
	}
	if receivedAnswer.Type != "webrtc_answer" {
		t.Errorf("User1 expected 'webrtc_answer', got '%s'", receivedAnswer.Type)
	}

	// User1 sends an ICE candidate
	candidatePayload := WebRTCIceCandidatePayload{Candidate: "test_candidate"}
	candidateMsg := Message{Type: "webrtc_candidate", ToUser: "user2", Payload: candidatePayload}
	ws1.WriteJSON(candidateMsg)

	// User2 should receive the ICE candidate
	var receivedCandidate Message
	if err := ws2.ReadJSON(&receivedCandidate); err != nil {
		t.Fatalf("User2 failed to read candidate: %v", err)
	}
	if receivedCandidate.Type != "webrtc_candidate" {
		t.Errorf("User2 expected 'webrtc_candidate', got '%s'", receivedCandidate.Type)
	}
}
