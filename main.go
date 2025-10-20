package main

import (
	"encoding/json"
	"fmt"
	"log"
	"math/rand" // Added for random peer selection
	"net/http"
	"sync"
	"time"

	"github.com/getsentry/sentry-go"
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
	// Add timeouts to prevent zombie connections
	HandshakeTimeout: 10 * time.Second,
}

// Client represents a single chatting user.
type Client struct {
	conn  *websocket.Conn
	send  chan []byte     // Outbound messages are JSON byte slices
	rooms map[string]bool // Set of rooms the client is in
	id    string          // Unique client identifier (e.g. from query param, not heavily used yet)
	hub   *Hub            // Reference to hub for room management
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
	redisStore  *RedisStore // Redis persistence layer
}

// Message defines the generic structure for all WebSocket messages.
type Message struct {
	Type       string      `json:"type"`                 // e.g., "chat_message", "webrtc_offer", "webrtc_answer", "webrtc_candidate", "assign_role", "join_room", "leave_room"
	FromUser   string      `json:"fromUser,omitempty"`   // Sender's client ID
	ToUser     string      `json:"toUser,omitempty"`     // Recipient's client ID (for direct messages)
	Room       string      `json:"room,omitempty"`       // Chat room ID or a call-specific context
	Payload    interface{} `json:"payload,omitempty"`    // Flexible payload based on Type
	MessageID  string      `json:"messageId,omitempty"`  // Unique message ID, typically client-generated
	CreateTime string      `json:"createTime,omitempty"` // Timestamp, can be client or server generated/overridden
	Incoming   bool        `json:"incoming,omitempty"`   // incoming message from remote user or not
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

// Payload for room management
type RoomActionPayload struct {
	Room   string `json:"room"`   // Room to join or leave
	Action string `json:"action"` // "join" or "leave"
}

// Sentry Helper Functions

// captureErrorWithContext sends an error to Sentry with additional context
func captureErrorWithContext(err error, message string, tags map[string]string, extra map[string]interface{}) {
	sentry.WithScope(func(scope *sentry.Scope) {
		// Add tags
		for key, value := range tags {
			scope.SetTag(key, value)
		}
		// Add extra context
		for key, value := range extra {
			scope.SetExtra(key, value)
		}
		// Set level based on error severity
		scope.SetLevel(sentry.LevelError)
		// Capture the error with message
		if err != nil {
			scope.SetContext("error_details", map[string]interface{}{
				"message": message,
				"error":   err.Error(),
			})
			sentry.CaptureException(err)
		} else {
			sentry.CaptureMessage(message)
		}
	})
}

// addBreadcrumb adds a breadcrumb to Sentry for tracing application flow
func addBreadcrumb(category string, message string, level sentry.Level, data map[string]interface{}) {
	sentry.AddBreadcrumb(&sentry.Breadcrumb{
		Category:  category,
		Message:   message,
		Level:     level,
		Timestamp: time.Now(),
		Data:      data,
	})
}

// recoverWithSentry recovers from panics in goroutines and reports them to Sentry
func recoverWithSentry(componentName string, additionalContext map[string]interface{}) {
	if r := recover(); r != nil {
		sentry.WithScope(func(scope *sentry.Scope) {
			scope.SetTag("component", componentName)
			scope.SetLevel(sentry.LevelFatal)
			for key, value := range additionalContext {
				scope.SetExtra(key, value)
			}
			scope.SetContext("panic_info", map[string]interface{}{
				"recovered_value": r,
				"component":       componentName,
			})

			// Convert panic to error
			var err error
			if e, ok := r.(error); ok {
				err = e
			} else {
				// Convert non-error panic to error using fmt.Sprintf
				err = fmt.Errorf("panic in %s: %v", componentName, r)
			}

			sentry.CaptureException(err)
			sentry.Flush(2 * time.Second)
		})
		log.Printf("PANIC in %s: %v. Reported to Sentry.", componentName, r)
	}
}

func newHub() *Hub {
	return &Hub{
		broadcast:   make(chan Message),
		register:    make(chan *Client),
		unregister:  make(chan *Client),
		clients:     make(map[*Client]bool),
		clientsByID: make(map[string]*Client),
		rooms:       make(map[string]map[*Client]bool),
		redisStore:  NewRedisStore(),
	}
}

// Helper function to get list of rooms from map
func getRoomList(rooms map[string]bool) []string {
	roomList := make([]string, 0, len(rooms))
	for room := range rooms {
		roomList = append(roomList, room)
	}
	return roomList
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

			// Capture marshalling errors
			captureErrorWithContext(err, "Failed to marshal message for client",
				map[string]string{"userId": clientID, "messageType": message.Type, "operation": "sendToClient_marshal"},
				map[string]interface{}{"message": message})
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
	defer recoverWithSentry("hub.run", map[string]interface{}{
		"total_clients": len(h.clients),
		"total_rooms":   len(h.rooms),
	})

	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			// General client registration
			h.clients[client] = true
			if client.id != "" {
				h.clientsByID[client.id] = client

				// Add breadcrumb for client connection
				addBreadcrumb("websocket", "Client connected", sentry.LevelInfo, map[string]interface{}{
					"userId":       client.id,
					"remoteAddr":   client.conn.RemoteAddr().String(),
					"initialRooms": getRoomList(client.rooms),
					"totalClients": len(h.clients) + 1,
				})

				// Join initial rooms if any
				for room := range client.rooms {
					if _, ok := h.rooms[room]; !ok {
						h.rooms[room] = make(map[*Client]bool)
					}
					h.rooms[room][client] = true
					// Persist to Redis
					if err := h.redisStore.AddUserToRoom(room, client.id); err != nil {
						log.Printf("Warning: Failed to persist room join to Redis: %v", err)
						captureErrorWithContext(err, "Failed to persist room join to Redis",
							map[string]string{"userId": client.id, "room": room, "operation": "register_room_join"},
							map[string]interface{}{"roomId": room})
					}

					// Add breadcrumb for room join
					addBreadcrumb("room", "User joined room", sentry.LevelInfo, map[string]interface{}{
						"userId": client.id,
						"room":   room,
					})
					log.Printf("Client %s joined room %s", client.id, room)
				}
			}
			log.Printf("Client %s (ID: %s) registered. Rooms: %v. Total clients: %d. Total clientsByID: %d",
				client.conn.RemoteAddr(), client.id, getRoomList(client.rooms), len(h.clients), len(h.clientsByID))
			h.mu.Unlock()

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				if client.id != "" {
					delete(h.clientsByID, client.id)

					// Add breadcrumb for client disconnection
					addBreadcrumb("websocket", "Client disconnected", sentry.LevelInfo, map[string]interface{}{
						"userId":       client.id,
						"remoteAddr":   client.conn.RemoteAddr().String(),
						"rooms":        getRoomList(client.rooms),
						"totalClients": len(h.clients) - 1,
					})

					// Remove from WebRTC available list in Redis
					if err := h.redisStore.RemoveUserFromAvailable(client.id); err != nil {
						log.Printf("Warning: Failed to remove user from available list in Redis: %v", err)
						captureErrorWithContext(err, "Failed to remove user from available list",
							map[string]string{"userId": client.id, "operation": "unregister_remove_available"},
							map[string]interface{}{})
					}
					log.Printf("User %s removed from available list during unregister.", client.id)
				}
				close(client.send)
				// Remove from all rooms
				for room := range client.rooms {
					if roomClients, ok := h.rooms[room]; ok {
						delete(roomClients, client)
						if len(roomClients) == 0 {
							delete(h.rooms, room)
							log.Printf("Chat room %s closed", room)
						}
					}
					// Remove from Redis
					if client.id != "" {
						if err := h.redisStore.RemoveUserFromRoom(room, client.id); err != nil {
							log.Printf("Warning: Failed to remove user from room in Redis: %v", err)
							captureErrorWithContext(err, "Failed to remove user from room",
								map[string]string{"userId": client.id, "room": room, "operation": "unregister_room_leave"},
								map[string]interface{}{"roomId": room})
						}

						// Add breadcrumb for room leave
						addBreadcrumb("room", "User left room on disconnect", sentry.LevelInfo, map[string]interface{}{
							"userId": client.id,
							"room":   room,
						})
					}
				}
				log.Printf("Client %s (ID: %s) unregistered. Was in rooms: %v", client.conn.RemoteAddr(), client.id, getRoomList(client.rooms))
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
				// Check if it's a direct message first
				if message.ToUser != "" {
					// Direct message - send only to the specific user
					log.Printf("Routing direct message from %s to %s", message.FromUser, message.ToUser)

					// Add breadcrumb for direct message
					addBreadcrumb("message", "Direct message sent", sentry.LevelInfo, map[string]interface{}{
						"fromUser": message.FromUser,
						"toUser":   message.ToUser,
						"type":     "chat_message",
					})

					h.sendToClientUnsafe(message.ToUser, message)

					// Also send a copy back to the sender for their UI
					// Mark it as outgoing for the sender
					senderCopy := message
					senderCopy.Incoming = false
					h.sendToClientUnsafe(message.FromUser, senderCopy)

					// Persist direct message to Redis
					if err := h.redisStore.SaveDirectMessage(message.FromUser, message.ToUser, message); err != nil {
						log.Printf("Warning: Failed to persist direct message to Redis: %v", err)
						captureErrorWithContext(err, "Failed to persist direct message",
							map[string]string{"fromUser": message.FromUser, "toUser": message.ToUser, "operation": "save_direct_message"},
							map[string]interface{}{"messageId": message.MessageID})
					}
				} else if message.Room != "" {
					// Room broadcast - verify sender is in the room
					senderClient, senderExists := h.clientsByID[message.FromUser]
					if senderExists && senderClient.rooms != nil && senderClient.rooms[message.Room] {
						// Sender is in the room, broadcast to all room members
						if roomClients, ok := h.rooms[message.Room]; ok {
							// Add breadcrumb for room message
							addBreadcrumb("message", "Room message broadcast", sentry.LevelInfo, map[string]interface{}{
								"fromUser":    message.FromUser,
								"room":        message.Room,
								"roomClients": len(roomClients),
								"type":        "chat_message",
							})

							jsonMessage, err := json.Marshal(message)
							if err != nil {
								log.Printf("Error marshalling chat_message: %v. Message: %+v", err, message)
								captureErrorWithContext(err, "Failed to marshal room chat message",
									map[string]string{"fromUser": message.FromUser, "room": message.Room, "operation": "marshal_room_message"},
									map[string]interface{}{"message": message})
								break
							}
							for cl := range roomClients {
								select {
								case cl.send <- jsonMessage:
								default:
									log.Printf("Chat: Client %s (ID: %s) disconnected due to full send channel.", cl.conn.RemoteAddr(), cl.id)
									captureErrorWithContext(nil, "Client send channel full",
										map[string]string{"userId": cl.id, "room": message.Room, "operation": "room_broadcast"},
										map[string]interface{}{"remoteAddr": cl.conn.RemoteAddr().String()})
								}
							}
							// Persist room message to Redis
							if err := h.redisStore.SaveRoomMessage(message.Room, message); err != nil {
								log.Printf("Warning: Failed to persist room message to Redis: %v", err)
								captureErrorWithContext(err, "Failed to persist room message",
									map[string]string{"fromUser": message.FromUser, "room": message.Room, "operation": "save_room_message"},
									map[string]interface{}{"messageId": message.MessageID})
							}
						}
					} else {
						log.Printf("Client %s tried to send to room %s but is not a member", message.FromUser, message.Room)

						// Capture unauthorized room access attempt
						captureErrorWithContext(nil, "User attempted to send to room without membership",
							map[string]string{"userId": message.FromUser, "room": message.Room, "operation": "unauthorized_room_send"},
							map[string]interface{}{"isMember": false})

						// Send error back to sender
						errorMsg := Message{
							Type:       "error",
							FromUser:   "system",
							ToUser:     message.FromUser,
							Payload:    map[string]string{"error": "You are not in that room. Join the room first."},
							CreateTime: time.Now().Format(time.RFC3339Nano),
						}
						h.sendToClientUnsafe(message.FromUser, errorMsg)
					}
				} else {
					log.Printf("Chat message from %s has neither ToUser nor Room. Discarding.", message.FromUser)
				}
			case "webrtc_offer", "webrtc_answer", "webrtc_candidate":
				if message.ToUser != "" {
					log.Printf("Routing WebRTC message type %s from %s to %s", message.Type, message.FromUser, message.ToUser)

					// Add breadcrumb for WebRTC signaling
					addBreadcrumb("webrtc", "WebRTC signaling message", sentry.LevelInfo, map[string]interface{}{
						"fromUser": message.FromUser,
						"toUser":   message.ToUser,
						"type":     message.Type,
					})

					h.sendToClientUnsafe(message.ToUser, message)
				} else {
					log.Printf("WebRTC message type %s from %s without ToUser field. Discarding.", message.Type, message.FromUser)
					captureErrorWithContext(nil, "WebRTC message missing ToUser field",
						map[string]string{"fromUser": message.FromUser, "messageType": message.Type, "operation": "webrtc_routing"},
						map[string]interface{}{})
				}
			case "assign_role": // Server originates this, but if client could send it, handle defensively
				if message.ToUser != "" {
					log.Printf("Routing assign_role message from %s to %s", message.FromUser, message.ToUser)
					h.sendToClientUnsafe(message.ToUser, message)
				}
			case "user_available_webrtc":
				log.Printf("User %s marked as available for WebRTC.", message.FromUser)

				// Add breadcrumb for availability change
				addBreadcrumb("webrtc", "User marked as available", sentry.LevelInfo, map[string]interface{}{
					"userId": message.FromUser,
				})

				if err := h.redisStore.AddUserToAvailable(message.FromUser); err != nil {
					log.Printf("Warning: Failed to add user to available list in Redis: %v", err)
					captureErrorWithContext(err, "Failed to add user to available list",
						map[string]string{"userId": message.FromUser, "operation": "add_to_available"},
						map[string]interface{}{})
				} else {
					// Log available count
					if count, err := h.redisStore.GetAvailableCount(); err == nil {
						log.Printf("User %s added to available list. Total available: %d", message.FromUser, count)
					}
				}
				// Optionally, confirm to user:
				// h.sendToClientUnsafe(message.FromUser, Message{Type:"status_update", Payload: UserStatusPayload{UserID: message.FromUser, Status: "available_for_call", Timestamp: time.Now().Format(time.RFC3339Nano)}})

			case "user_unavailable_webrtc":
				log.Printf("User %s marked as unavailable for WebRTC.", message.FromUser)

				// Add breadcrumb for availability change
				addBreadcrumb("webrtc", "User marked as unavailable", sentry.LevelInfo, map[string]interface{}{
					"userId": message.FromUser,
				})

				if err := h.redisStore.RemoveUserFromAvailable(message.FromUser); err != nil {
					log.Printf("Warning: Failed to remove user from available list in Redis: %v", err)
					captureErrorWithContext(err, "Failed to remove user from available list",
						map[string]string{"userId": message.FromUser, "operation": "remove_from_available"},
						map[string]interface{}{})
				}

			case "join_room":
				// Handle room join request
				if message.Room != "" && message.FromUser != "" {
					if client, ok := h.clientsByID[message.FromUser]; ok {
						// Add client to the room
						if client.rooms == nil {
							client.rooms = make(map[string]bool)
						}
						client.rooms[message.Room] = true

						// Add to hub's room map
						if _, ok := h.rooms[message.Room]; !ok {
							h.rooms[message.Room] = make(map[*Client]bool)
						}
						h.rooms[message.Room][client] = true

						// Add breadcrumb for room join
						addBreadcrumb("room", "User joined room via message", sentry.LevelInfo, map[string]interface{}{
							"userId":      message.FromUser,
							"room":        message.Room,
							"roomClients": len(h.rooms[message.Room]),
						})

						// Persist to Redis
						if err := h.redisStore.AddUserToRoom(message.Room, message.FromUser); err != nil {
							log.Printf("Warning: Failed to persist room join to Redis: %v", err)
							captureErrorWithContext(err, "Failed to persist room join",
								map[string]string{"userId": message.FromUser, "room": message.Room, "operation": "join_room"},
								map[string]interface{}{})
						}

						log.Printf("Client %s joined room %s", message.FromUser, message.Room)

						// Send confirmation back to client
						confirmMsg := Message{
							Type:       "room_joined",
							Room:       message.Room,
							FromUser:   "system",
							ToUser:     message.FromUser,
							Payload:    RoomActionPayload{Room: message.Room, Action: "joined"},
							CreateTime: time.Now().Format(time.RFC3339Nano),
						}
						h.sendToClientUnsafe(message.FromUser, confirmMsg)
					}
				}

			case "leave_room":
				// Handle room leave request
				if message.Room != "" && message.FromUser != "" {
					if client, ok := h.clientsByID[message.FromUser]; ok {
						// Remove client from the room
						if client.rooms != nil {
							delete(client.rooms, message.Room)
						}

						// Remove from hub's room map
						if roomClients, ok := h.rooms[message.Room]; ok {
							delete(roomClients, client)
							if len(roomClients) == 0 {
								delete(h.rooms, message.Room)
								log.Printf("Room %s closed (no clients)", message.Room)
							}
						}

						// Add breadcrumb for room leave
						addBreadcrumb("room", "User left room via message", sentry.LevelInfo, map[string]interface{}{
							"userId": message.FromUser,
							"room":   message.Room,
						})

						// Remove from Redis
						if err := h.redisStore.RemoveUserFromRoom(message.Room, message.FromUser); err != nil {
							log.Printf("Warning: Failed to remove user from room in Redis: %v", err)
							captureErrorWithContext(err, "Failed to remove user from room",
								map[string]string{"userId": message.FromUser, "room": message.Room, "operation": "leave_room"},
								map[string]interface{}{})
						}

						log.Printf("Client %s left room %s", message.FromUser, message.Room)

						// Send confirmation back to client
						confirmMsg := Message{
							Type:       "room_left",
							Room:       message.Room,
							FromUser:   "system",
							ToUser:     message.FromUser,
							Payload:    RoomActionPayload{Room: message.Room, Action: "left"},
							CreateTime: time.Now().Format(time.RFC3339Nano),
						}
						h.sendToClientUnsafe(message.FromUser, confirmMsg)
					}
				}

			case "get_rooms":
				// Return list of rooms the client is in
				if message.FromUser != "" {
					if client, ok := h.clientsByID[message.FromUser]; ok {
						roomList := getRoomList(client.rooms)
						roomsMsg := Message{
							Type:       "rooms_list",
							FromUser:   "system",
							ToUser:     message.FromUser,
							Payload:    map[string]interface{}{"rooms": roomList},
							CreateTime: time.Now().Format(time.RFC3339Nano),
						}
						h.sendToClientUnsafe(message.FromUser, roomsMsg)
					}
				}

			case "request_random_peer":
				log.Printf("User %s requests a random peer.", message.FromUser)
				var currentPeerID string
				if payload, ok := message.Payload.(map[string]interface{}); ok {
					if cpid, ok := payload["currentPeerId"].(string); ok {
						currentPeerID = cpid
					}
				}

				// Add breadcrumb for peer request
				addBreadcrumb("webrtc", "Random peer requested", sentry.LevelInfo, map[string]interface{}{
					"userId":        message.FromUser,
					"currentPeerId": currentPeerID,
				})

				// Get random available peer from Redis, excluding self and current peer
				excludeUsers := []string{message.FromUser}
				if currentPeerID != "" {
					excludeUsers = append(excludeUsers, currentPeerID)
				}

				selectedPeerID, err := h.redisStore.GetRandomAvailablePeer(excludeUsers...)
				if err != nil {
					log.Printf("Error getting random peer: %v", err)
					captureErrorWithContext(err, "Failed to get random peer",
						map[string]string{"userId": message.FromUser, "operation": "get_random_peer"},
						map[string]interface{}{"excludeUsers": excludeUsers})
					selectedPeerID = ""
				}

				log.Printf("Selected peer: %s for requester: %s", selectedPeerID, message.FromUser)

				if selectedPeerID != "" {
					// Add breadcrumb for successful pairing
					addBreadcrumb("webrtc", "Peer pairing successful", sentry.LevelInfo, map[string]interface{}{
						"requester":    message.FromUser,
						"selectedPeer": selectedPeerID,
					})

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
					if err := h.redisStore.RemoveUserFromAvailable(message.FromUser); err != nil {
						log.Printf("Warning: Failed to remove requester from available list: %v", err)
						captureErrorWithContext(err, "Failed to remove requester from available list",
							map[string]string{"userId": message.FromUser, "operation": "remove_after_pairing"},
							map[string]interface{}{"peerId": selectedPeerID})
					}
					if err := h.redisStore.RemoveUserFromAvailable(selectedPeerID); err != nil {
						log.Printf("Warning: Failed to remove selected peer from available list: %v", err)
						captureErrorWithContext(err, "Failed to remove selected peer from available list",
							map[string]string{"userId": selectedPeerID, "operation": "remove_after_pairing"},
							map[string]interface{}{"requesterId": message.FromUser})
					}
					log.Printf("Users %s and %s paired for WebRTC negotiation, removed from available list.", message.FromUser, selectedPeerID)

				} else {
					// No peer available
					addBreadcrumb("webrtc", "No peer available", sentry.LevelInfo, map[string]interface{}{
						"userId": message.FromUser,
					})

					noPeerPayload := map[string]string{"message": "No peer available at the moment. Please try again later."}
					msgToRequester := Message{Type: "no_peer_available", ToUser: message.FromUser, FromUser: "system", Payload: noPeerPayload, CreateTime: time.Now().Format(time.RFC3339Nano)}
					h.sendToClientUnsafe(message.FromUser, msgToRequester)
					log.Printf("No suitable peer found for %s.", message.FromUser)
				}

			case "hangup":
				// Handle hangup message - forward to the peer
				if message.ToUser != "" {
					log.Printf("Routing hangup message from %s to %s", message.FromUser, message.ToUser)
					h.sendToClientUnsafe(message.ToUser, message)

					// Both users should re-declare availability after hangup
					// Note: They should do this themselves, but we can add a delay to prevent immediate re-pairing
				}

			case "get_recent_messages":
				// Retrieve recent messages from a room
				if message.FromUser != "" && message.Room != "" {
					// Parse payload for limit and before timestamp
					limit := int64(50) // Default limit
					var beforeTime *time.Time

					if payload, ok := message.Payload.(map[string]interface{}); ok {
						if limitVal, ok := payload["limit"].(float64); ok {
							limit = int64(limitVal)
						}
						if beforeStr, ok := payload["before"].(string); ok {
							if t, err := time.Parse(time.RFC3339Nano, beforeStr); err == nil {
								beforeTime = &t
							}
						}
					}

					messages, err := h.redisStore.GetRoomMessages(message.Room, limit, beforeTime)
					if err != nil {
						log.Printf("Error retrieving room messages: %v", err)
						captureErrorWithContext(err, "Failed to retrieve room messages",
							map[string]string{"userId": message.FromUser, "room": message.Room, "operation": "get_room_messages"},
							map[string]interface{}{"limit": limit})
						messages = []Message{} // Return empty array on error
					}

					responseMsg := Message{
						Type:       "recent_messages",
						FromUser:   "system",
						ToUser:     message.FromUser,
						Room:       message.Room,
						Payload:    map[string]interface{}{"messages": messages, "room": message.Room},
						CreateTime: time.Now().Format(time.RFC3339Nano),
					}
					h.sendToClientUnsafe(message.FromUser, responseMsg)
					log.Printf("Sent %d recent messages from room %s to user %s", len(messages), message.Room, message.FromUser)
				}

			case "get_direct_messages":
				// Retrieve direct messages between two users
				if message.FromUser != "" {
					withUser := ""
					limit := int64(50) // Default limit

					if payload, ok := message.Payload.(map[string]interface{}); ok {
						if user, ok := payload["withUser"].(string); ok {
							withUser = user
						}
						if limitVal, ok := payload["limit"].(float64); ok {
							limit = int64(limitVal)
						}
					}

					if withUser != "" {
						messages, err := h.redisStore.GetDirectMessages(message.FromUser, withUser, limit)
						if err != nil {
							log.Printf("Error retrieving direct messages: %v", err)
							captureErrorWithContext(err, "Failed to retrieve direct messages",
								map[string]string{"userId": message.FromUser, "withUser": withUser, "operation": "get_direct_messages"},
								map[string]interface{}{"limit": limit})
							messages = []Message{} // Return empty array on error
						}

						responseMsg := Message{
							Type:       "direct_messages",
							FromUser:   "system",
							ToUser:     message.FromUser,
							Payload:    map[string]interface{}{"messages": messages, "withUser": withUser},
							CreateTime: time.Now().Format(time.RFC3339Nano),
						}
						h.sendToClientUnsafe(message.FromUser, responseMsg)
						log.Printf("Sent %d direct messages between %s and %s", len(messages), message.FromUser, withUser)
					}
				}

			default:
				log.Printf("Unknown message type received: %s from client %s. Discarding.", message.Type, message.FromUser)

				// Capture unknown message type for monitoring
				captureErrorWithContext(nil, "Unknown message type received",
					map[string]string{"userId": message.FromUser, "messageType": message.Type, "operation": "message_routing"},
					map[string]interface{}{"message": message})
			}
			h.mu.Unlock() // Release lock after processing
		}
	}
}

func (c *Client) readPump(hub *Hub) {
	defer func() {
		recoverWithSentry("readPump", map[string]interface{}{
			"userId":     c.id,
			"remoteAddr": c.conn.RemoteAddr().String(),
		})
		hub.unregister <- c
		c.conn.Close()
	}()

	// Set read deadline to detect disconnected clients
	c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		_, messageBytes, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure, websocket.CloseNoStatusReceived) {
				log.Printf("error reading message: %v, client: %s (ID: %s)", err, c.conn.RemoteAddr(), c.id)

				// Capture unexpected close errors
				captureErrorWithContext(err, "Unexpected WebSocket close error",
					map[string]string{"userId": c.id, "operation": "readPump_read"},
					map[string]interface{}{"remoteAddr": c.conn.RemoteAddr().String()})
			}
			break
		}

		var msg Message
		if err := json.Unmarshal(messageBytes, &msg); err != nil {
			log.Printf("error unmarshalling outer message: %v from client %s (ID: %s), raw: %s", err, c.conn.RemoteAddr(), c.id, string(messageBytes))

			// Capture unmarshalling errors
			captureErrorWithContext(err, "Failed to unmarshal message from client",
				map[string]string{"userId": c.id, "operation": "readPump_unmarshal"},
				map[string]interface{}{"remoteAddr": c.conn.RemoteAddr().String(), "rawMessage": string(messageBytes)})

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
		// Clients now specify the room they want to send to
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
	ticker := time.NewTicker(54 * time.Second)
	defer func() {
		recoverWithSentry("writePump", map[string]interface{}{
			"userId":     c.id,
			"remoteAddr": c.conn.RemoteAddr().String(),
		})
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case messageBytes, ok := <-c.send: // Expecting JSON bytes
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				log.Printf("Hub closed channel for client %s (ID: %s)", c.conn.RemoteAddr(), c.id)
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, messageBytes); err != nil {
				log.Printf("error writing message: %v, client: %s (ID: %s)", err, c.conn.RemoteAddr(), c.id)

				// Capture write errors
				captureErrorWithContext(err, "Failed to write message to client",
					map[string]string{"userId": c.id, "operation": "writePump_write"},
					map[string]interface{}{"remoteAddr": c.conn.RemoteAddr().String()})

				// Consider unregistering the client on write error
				return
			}
		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func serveWs(hub *Hub, w http.ResponseWriter, r *http.Request) {
	roomName := r.URL.Query().Get("room")
	userId := r.URL.Query().Get("userId") // Get userId from query param

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("Upgrade error:", err)

		// Capture WebSocket upgrade errors
		captureErrorWithContext(err, "WebSocket upgrade failed",
			map[string]string{"userId": userId, "operation": "websocket_upgrade"},
			map[string]interface{}{"remoteAddr": r.RemoteAddr, "room": roomName})
		return
	}

	// Add breadcrumb for new connection attempt
	addBreadcrumb("websocket", "New WebSocket connection", sentry.LevelInfo, map[string]interface{}{
		"userId":     userId,
		"room":       roomName,
		"remoteAddr": r.RemoteAddr,
	})

	// Initialize client with support for multiple rooms
	client := &Client{
		conn:  conn,
		send:  make(chan []byte, 256),
		rooms: make(map[string]bool),
		id:    userId,
		hub:   hub,
	}

	// If a room was specified in the URL, join it initially
	if roomName != "" {
		client.rooms[roomName] = true
	} else {
		// Default to Lobby if no room specified
		client.rooms["Lobby"] = true
	}

	hub.register <- client

	go client.writePump()
	go client.readPump(hub)

	log.Printf("Client %s (ID: %s) connected with initial rooms: %v", conn.RemoteAddr(), userId, getRoomList(client.rooms))
}

func healthCheckHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	// A simple response to indicate the server is up and running
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile) // Optional: for more detailed logging

	// Initialize Sentry
	err := sentry.Init(sentry.ClientOptions{
		Dsn:              "https://6245ca95ad3f27465a22b2da1eefecad@o4510162358894592.ingest.us.sentry.io/4510162360795136",
		Environment:      "production", // Change to "development" or read from env var if needed
		TracesSampleRate: 1.0,          // Capture 100% of transactions for performance monitoring
		Debug:            false,        // Set to true for verbose debugging
	})
	if err != nil {
		log.Fatalf("sentry.Init: %s", err)
	}
	// Flush buffered events before the program terminates
	defer sentry.Flush(2 * time.Second)

	// Send startup verification message
	sentry.CaptureMessage("Aleatoria backend started successfully")
	log.Println("Sentry initialized successfully")

	hub := newHub()
	go hub.run()

	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		serveWs(hub, w, r)
	})

	http.HandleFunc("/health", healthCheckHandler)

	log.Println("HTTP server started on :8085")
	serverErr := http.ListenAndServe(":8085", nil)
	if serverErr != nil {
		// Capture fatal server error
		sentry.CaptureException(serverErr)
		sentry.Flush(2 * time.Second)
		log.Fatal("ListenAndServe: ", serverErr)
	}
}
