package main

import (
	"encoding/json"
	"log"
	"math/rand" // Added for random peer selection
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

func init() {
	// Seed the random number generator. Important for true randomness.
	rand.Seed(time.Now().UnixNano())
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     ValidateOrigin,
}

// Client represents a single chatting user.
type Client struct {
	conn *websocket.Conn
	send chan []byte // Outbound messages are JSON byte slices
	room string
	id   string      // Unique client identifier (e.g. from query param, not heavily used yet)
}

// Hub maintains the set of active clients and broadcasts messages to the
// clients in the same room.
type Hub struct {
	clients     map[*Client]bool
	clientsByID map[string]*Client // Added for direct messaging by client ID
	broadcast   chan Message       // Structs go into broadcast
	register    chan *Client
	unregister  chan *Client
	rooms       map[string]map[*Client]bool
	mu          sync.Mutex
}

// Message defines the generic structure for all WebSocket messages.
type Message struct {
	Type       string      `json:"type"`                 // e.g., "chat_message", "webrtc_offer", "webrtc_answer", "webrtc_candidate", "assign_role"
	FromUser   string      `json:"fromUser,omitempty"`   // Sender's client ID
	ToUser     string      `json:"toUser,omitempty"`     // Recipient's client ID (for direct messages)
	Room       string      `json:"room,omitempty"`       // Chat room ID or a call-specific context
	Payload    interface{} `json:"payload,omitempty"`    // Flexible payload based on Type
	MessageID  string      `json:"messageId,omitempty"`  // Unique message ID, typically client-generated
	CreateTime string      `json:"createTime,omitempty"` // Timestamp, can be client or server generated/overridden
	Incoming   bool        `json:"incoming,omitempty"` // incoming message from remote user or not
}

// Specific payload structures
type ChatPayload struct {
	Content string `json:"content"`
}

type WebRTCSessionDescriptionPayload struct { // Used for offer and answer
	Type string `json:"type"` // "offer" or "answer" (redundant but good for clarity on client)
	Sdp  string `json:"sdp"`
}

type WebRTCIceCandidatePayload struct {
	Candidate        interface{} `json:"candidate"` // This will be the RTCIceCandidateInit dictionary/object
	SdpMid           *string     `json:"sdpMid,omitempty"`
	SdpMLineIndex    *uint16     `json:"sdpMLineIndex,omitempty"`
	UsernameFragment *string     `json:"usernameFragment,omitempty"`
}

type AssignRolePayload struct {
	Role   string `json:"role"`   // "polite" or "impolite"
	PeerId string `json:"peerId"` // The ID of the peer they are negotiating with
}

// For user status updates, e.g., when a user connects/disconnects or becomes available/unavailable for calls
type UserStatusPayload struct {
	UserID    string `json:"userId"`
	Status    string `json:"status"` // "online", "offline", "in-call", "available_for_call"
	Timestamp string `json:"timestamp"`
}

type RequestRandomPeerPayload struct {
	// Could include user preferences in the future
	CurrentPeerID string `json:"currentPeerId,omitempty"` // If user is skipping someone
}


func newHub() *Hub {
	return &Hub{
		broadcast:          make(chan Message),
		register:           make(chan *Client),
		unregister:         make(chan *Client),
		clients:            make(map[*Client]bool),
		clientsByID:        make(map[string]*Client),
		rooms:              make(map[string]map[*Client]bool),
		// availableForCall: make(map[string]bool), // Client IDs available for calls
	}
}

// Helper to manage available users. For simplicity, a slice. Could be a map for faster removal.
var availableForCall []string
var availableForCallMu sync.Mutex

func removeUserFromAvailable(userID string) {
	availableForCallMu.Lock()
	defer availableForCallMu.Unlock()
	for i, id := range availableForCall {
		if id == userID {
			availableForCall = append(availableForCall[:i], availableForCall[i+1:]...)
			return
		}
	}
}

func addUserToAvailable(userID string) {
	availableForCallMu.Lock()
	defer availableForCallMu.Unlock()
	// Avoid duplicates
	for _, id := range availableForCall {
		if id == userID {
			return
		}
	}
	availableForCall = append(availableForCall, userID)
	log.Printf("User %s added to available list. Total available: %d", userID, len(availableForCall))
}


// sendToClient sends a message to a specific client by their ID.
// IMPORTANT: This function must be called with h.mu locked if it modifies shared client state,
// or if called from a context that already holds the lock.
// For sending messages, it's generally safer if it doesn't try to unregister clients directly
// to avoid deadlocks, as unregister also takes the same lock.
func (h *Hub) sendToClientUnsafe(clientID string, message Message) {
	if targetClient, ok := h.clientsByID[clientID]; ok {
		jsonMessage, err := json.Marshal(message)
		if err != nil {
			log.Printf("Error marshalling direct message for %s: %v. Message: %+v", clientID, err, message)
			return
		}
		select {
		case targetClient.send <- jsonMessage:
		default:
			// This part is tricky. If send channel is full, client is stuck or slow.
			// Closing their channel or trying to unregister them from here can lead to deadlocks
			// if Hub.run() is also trying to operate on this client.
			// Best to log and potentially have a separate cleanup mechanism for stuck clients.
			log.Printf("Could not send direct message to client %s, channel full.", clientID)
		}
	} else {
		log.Printf("Client %s not found for direct message.", clientID)
	}
}


func (h *Hub) run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			// General client registration
			h.clients[client] = true
			if client.id != "" {
				h.clientsByID[client.id] = client
				// By default, new users are not added to availableForCall list for WebRTC.
				// They need to explicitly indicate availability.
				// Or, if chat room implies WebRTC context, can add them here.
				// For now, let's assume chat clients join a chat room:
				if client.room != "" { // If room is specified (e.g. for chat)
					if _, ok := h.rooms[client.room]; !ok {
						h.rooms[client.room] = make(map[*Client]bool)
					}
					h.rooms[client.room][client] = true
				}
			}
			log.Printf("Client %s (ID: %s) registered. Room: %s. Total clients: %d. Total clientsByID: %d",
				client.conn.RemoteAddr(), client.id, client.room, len(h.clients), len(h.clientsByID))
			h.mu.Unlock()

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				if client.id != "" {
					delete(h.clientsByID, client.id)
					removeUserFromAvailable(client.id) // Remove from WebRTC available list
					log.Printf("User %s removed from available list during unregister.", client.id)
				}
				close(client.send)
				if client.room != "" && h.rooms[client.room] != nil { // If was in a chat room
					delete(h.rooms[client.room], client)
					if len(h.rooms[client.room]) == 0 {
						delete(h.rooms, client.room)
						log.Printf("Chat room %s closed", client.room)
					}
				}
				log.Printf("Client %s (ID: %s) unregistered. Room: %s", client.conn.RemoteAddr(), client.id, client.room)
			}
			h.mu.Unlock()

		case message := <-h.broadcast: // message is a Message struct from a client
			h.mu.Lock() // Lock for duration of processing this message

			log.Printf("received web socket message %+v", message)

			// Ensure CreateTime is set if not already
			if message.CreateTime == "" {
				message.CreateTime = time.Now().Format(time.RFC3339Nano)
			}

			switch message.Type {
			case "chat_message":
				if roomClients, ok := h.rooms[message.Room]; ok {
					jsonMessage, err := json.Marshal(message)
					if err != nil {
						log.Printf("Error marshalling chat_message: %v. Message: %+v", err, message)
						break // Break switch, will release lock in defer
					}
					for cl := range roomClients {
						if cl.id == message.FromUser && message.Room == cl.room { // Don't send to self if it's a self-echo from same room
							// This depends on whether client expects echo or not. Usually not for chat.
							// continue
						}
						select {
						case cl.send <- jsonMessage:
						default:
							log.Printf("Chat: Client %s (ID: %s) in room %s disconnected due to full send channel.", cl.conn.RemoteAddr(), cl.id, cl.room)
							// Potentially unregister this client - complex due to lock
						}
					}
				}
			case "webrtc_offer", "webrtc_answer", "webrtc_candidate":
				if message.ToUser != "" {
					log.Printf("Routing WebRTC message type %s from %s to %s", message.Type, message.FromUser, message.ToUser)
					h.sendToClientUnsafe(message.ToUser, message)
				} else {
					log.Printf("WebRTC message type %s from %s without ToUser field. Discarding.", message.Type, message.FromUser)
				}
			case "assign_role": // Server originates this, but if client could send it, handle defensively
				if message.ToUser != "" {
					log.Printf("Routing assign_role message from %s to %s", message.FromUser, message.ToUser)
					h.sendToClientUnsafe(message.ToUser, message)
				}
			case "user_available_webrtc":
				log.Printf("User %s marked as available for WebRTC.", message.FromUser)
				addUserToAvailable(message.FromUser)
				// Optionally, confirm to user:
				// h.sendToClientUnsafe(message.FromUser, Message{Type:"status_update", Payload: UserStatusPayload{UserID: message.FromUser, Status: "available_for_call", Timestamp: time.Now().Format(time.RFC3339Nano)}})

			case "user_unavailable_webrtc":
				log.Printf("User %s marked as unavailable for WebRTC.", message.FromUser)
				removeUserFromAvailable(message.FromUser)

			case "request_random_peer":
				log.Printf("User %s requests a random peer.", message.FromUser)
				var currentPeerID string
				if payload, ok := message.Payload.(map[string]interface{}); ok {
					if cpid, ok := payload["currentPeerId"].(string); ok {
						currentPeerID = cpid
					}
				}

				availableForCallMu.Lock()
				var selectedPeerID string
				if len(availableForCall) > 0 {
					// Simple random selection for now, avoid self and currentPeerID
					possiblePeers := []string{}
					for _, potentialPeer := range availableForCall {
						if potentialPeer != message.FromUser && potentialPeer != currentPeerID {
							possiblePeers = append(possiblePeers, potentialPeer)
						}
					}

					if len(possiblePeers) > 0 {
						// Select a random peer from the possiblePeers list
						randomIndex := rand.Intn(len(possiblePeers))
						selectedPeerID = possiblePeers[randomIndex]

						// Remove both from available list as they are about to be paired
						// This needs to be careful with availableForCallMu if called from within loop
						// It's better to collect IDs to remove and do it after loop or use map for availableForCall

						// For now, simple removal (potential issues if many requests concurrently)
						// This is a critical section that needs careful thought for concurrency.
						// Let's just mark them conceptually for now and actual removal happens on call accept/start.
						log.Printf("Found potential peer %s for %s", selectedPeerID, message.FromUser)

					}
				}
				availableForCallMu.Unlock() // Unlock before sending messages

				log.Printf("Mutext was unlocked. selectedPeerID is: %s", selectedPeerID)

				if selectedPeerID != "" {
					// Assign roles: Requester is impolite, selected is polite
					// Server sends 'assign_role' to both.
					roleForRequester := AssignRolePayload{Role: "impolite", PeerId: selectedPeerID}
					msgToRequester := Message{Type: "assign_role", ToUser: message.FromUser, FromUser: "system", Payload: roleForRequester, CreateTime: time.Now().Format(time.RFC3339Nano)}
					h.sendToClientUnsafe(message.FromUser, msgToRequester)

					roleForSelected := AssignRolePayload{Role: "polite", PeerId: message.FromUser}
					msgToSelected := Message{Type: "assign_role", ToUser: selectedPeerID, FromUser: "system", Payload: roleForSelected, CreateTime: time.Now().Format(time.RFC3339Nano)}
					h.sendToClientUnsafe(selectedPeerID, msgToSelected)

					// Crucially, after successful pairing for negotiation, remove them from general availability
                    // This prevents them from being immediately picked by another request.
                    // They should re-declare availability after their call ends.
                    removeUserFromAvailable(message.FromUser)
                    removeUserFromAvailable(selectedPeerID)
                    log.Printf("Users %s and %s paired for WebRTC negotiation, removed from available list.", message.FromUser, selectedPeerID)

				} else {
					// No peer available
					noPeerPayload := map[string]string{"message": "No peer available at the moment. Please try again later."}
					msgToRequester := Message{Type: "no_peer_available", ToUser: message.FromUser, FromUser: "system", Payload: noPeerPayload, CreateTime: time.Now().Format(time.RFC3339Nano)}
					h.sendToClientUnsafe(message.FromUser, msgToRequester)
					log.Printf("No suitable peer found for %s.", message.FromUser)
				}

			default:
				log.Printf("Unknown message type received: %s from client %s. Discarding.", message.Type, message.FromUser)
			}
			h.mu.Unlock() // Release lock after processing
		}
	}
}

func (c *Client) readPump(hub *Hub) {
	defer func() {
		hub.unregister <- c
		c.conn.Close()
	}()
	for {
		_, messageBytes, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("error reading message: %v, client: %s (ID: %s)", err, c.conn.RemoteAddr(), c.id)
			}
			break
		}

		var msg Message
		if err := json.Unmarshal(messageBytes, &msg); err != nil {
			log.Printf("error unmarshalling outer message: %v from client %s (ID: %s), raw: %s", err, c.conn.RemoteAddr(), c.id, string(messageBytes))
			// Consider sending a structured error message back to the client
			errorPayload := map[string]string{"error": "Invalid message structure", "details": err.Error()}
			errorMsg := Message{
				Type:       "error_response",
				ToUser:     c.id, // Send back to the originating user
				Payload:    errorPayload,
				MessageID:  "err-" + time.Now().Format(time.RFC3339Nano),
				CreateTime: time.Now().Format(time.RFC3339Nano),
			}
			jsonErrorMsg, _ := json.Marshal(errorMsg)
			// This direct send might block if client.send is full.
			// For errors, it might be okay or use a select with a default.
			select {
			case c.send <- jsonErrorMsg:
			default:
				log.Printf("Could not send error_response to client %s, channel full or closed.", c.id)
			}
			continue
		}

		// Populate server-authoritative fields
		msg.FromUser = c.id
		if msg.Type == "chat_message" { // For chat messages, ensure they are associated with the client's current room
			msg.Room = c.room
		}
		// If CreateTime is not set by client, or to enforce server time:
		if msg.CreateTime == "" {
			msg.CreateTime = time.Now().Format(time.RFC3339Nano)
		}
		// MessageID should ideally be client-generated for client-side tracking.

		// Further validation based on msg.Type could happen here.
		// For example, ensuring payload is of expected type.
		// If msg.Type == "chat_message", msg.Payload should be unmarshallable to ChatPayload.
		// For now, we'll pass it as is, assuming client sends correct payload structure.

		hub.broadcast <- msg
	}
}

func (c *Client) writePump() {
	defer func() {
		c.conn.Close()
	}()
	for {
		select {
		case messageBytes, ok := <-c.send: // Expecting JSON bytes
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				log.Printf("Hub closed channel for client %s (ID: %s)", c.conn.RemoteAddr(), c.id)
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, messageBytes); err != nil {
				log.Printf("error writing message: %v, client: %s (ID: %s)", err, c.conn.RemoteAddr(), c.id)
				// Consider unregistering the client on write error
				return
			}
		}
	}
}

func serveWs(hub *Hub, w http.ResponseWriter, r *http.Request) {
	roomName := r.URL.Query().Get("room")
	if roomName == "" {
		roomName = "Lobby" // Default room
	}
	userId := r.URL.Query().Get("userId") // Get userId from query param

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("Upgrade error:", err)
		return
	}

	client := &Client{
		conn: conn,
		send: make(chan []byte, 256),
		room: roomName,
		id:   userId, // Store the userId
	}
	hub.register <- client

	go client.writePump()
	go client.readPump(hub)

	log.Printf("Client %s (ID: %s) connected to room: %s", conn.RemoteAddr(), userId, roomName)
}

func healthCheckHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	// A simple response to indicate the server is up and running
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile) // Optional: for more detailed logging

	hub := newHub()
	go hub.run()

	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		serveWs(hub, w, r)
	})

	http.HandleFunc("/health", healthCheckHandler)

	log.Println("HTTP server started on :8085")
	err := http.ListenAndServe(":8085", nil)
	if err != nil {
		log.Fatal("ListenAndServe: ", err)
	}
}
