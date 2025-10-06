# Aleatoria Chat Server

## Overview

**Aleatoria** is a real-time WebSocket communication server written in Go, designed to power modern chat applications with integrated WebRTC video/audio calling capabilities. It provides a robust signaling infrastructure for peer-to-peer connections and multi-user chat rooms, making it ideal for building Omegle-like random chat applications or any real-time communication platform.

### Key Features

- **🔌 WebSocket-based Real-time Communication**: Persistent connections for instant message delivery
- **💬 Multi-room Chat System**: Support for unlimited chat rooms with dynamic join/leave functionality
- **📹 WebRTC Signaling Server**: Full signaling support for peer-to-peer video/audio calls
- **🎲 Random Peer Matching**: Intelligent algorithm to match users for WebRTC calls
- **📧 Direct Messaging**: One-to-one private messaging between users
- **🔒 Origin Validation**: Security layer to prevent unauthorized connections
- **❤️ Connection Health Monitoring**: Automatic ping/pong heartbeat mechanism
- **⚡ Concurrent Connection Handling**: Thread-safe operations using Go's concurrency primitives
- **🛡️ Robust Error Handling**: Comprehensive error messages and graceful degradation
- **✅ Production-Ready**: Includes health check endpoint for load balancers and monitoring

### Technology Stack

- **Language**: Go 1.24.3
- **WebSocket Library**: [Gorilla WebSocket](https://github.com/gorilla/websocket) v1.5.1
- **Server Port**: 8085 (configurable)
- **Protocol**: WebSocket over HTTP/HTTPS with JSON message format

### Use Cases

- Random video chat applications (Omegle-style)
- Multi-room chat platforms
- Real-time collaboration tools
- WebRTC signaling infrastructure
- Live customer support systems
- Social networking platforms with video features

This documentation is intended for developers and AI agents who need to interact with the server's API or maintain the codebase.

## Table of Contents

1. [Getting Started](#getting-started)
2. [Configuration](#configuration)
3. [Architecture](#architecture)
4. [API Endpoints](#api-endpoints)
5. [WebSocket Message Structure](#websocket-message-structure)
6. [Message Types and Payloads](#message-types-and-payloads)
7. [WebRTC Signaling Flow](#webrtc-signaling-flow)
8. [Security Features](#security-features)
9. [Testing](#testing)
10. [Go Data Structures](#go-data-structures)
11. [Deployment](#deployment)

## Getting Started

### Prerequisites

- Go 1.24.3 or higher
- Git

### Installation

1. Clone the repository:
   ```bash
   git clone <repository-url>
   cd backend
   ```

2. Install dependencies:
   ```bash
   go mod download
   ```

3. Run the server:
   ```bash
   go run main.go validation.go
   ```

4. The server will start on port 8085:
   ```
   HTTP server started on :8085
   ```

### Quick Test

Test the health endpoint:
```bash
curl http://localhost:8085/health
```

Expected response:
```json
{"status":"ok"}
```

### Running Tests

Run all tests:
```bash
go test -v
```

Run specific test:
```bash
go test -v -run TestWebRTC_PeerToPeerFlow
```

## Configuration

### Environment Variables

The server can be configured through the following methods:

**Allowed Origins**: Modify the `allowedOrigins` variable in `validation.go`:
```go
var allowedOrigins = []string{"https://www.aleatoria.chat"}
```

For production, consider loading this from environment variables or a configuration file.

**Server Port**: The default port is `8085`. To change it, modify the `main.go` file:
```go
http.ListenAndServe(":8085", nil)
```

### Connection Parameters

The server has built-in timeouts and buffer sizes optimized for production:

- **Read Buffer**: 1024 bytes
- **Write Buffer**: 1024 bytes
- **Handshake Timeout**: 10 seconds
- **Read Deadline**: 60 seconds (reset on pong)
- **Write Deadline**: 10 seconds
- **Ping Interval**: 54 seconds
- **Send Channel Buffer**: 256 messages

## Architecture

### System Design

The server follows a hub-and-spoke architecture pattern:

```
┌─────────────────────────────────────────────────┐
│                     Hub                         │
│  ┌───────────────────────────────────────────┐ │
│  │  • Client Registry (by ID & connection)   │ │
│  │  • Room Management                        │ │
│  │  • Message Broadcasting                   │ │
│  │  • WebRTC Peer Matching                   │ │
│  │  • Availability Tracking                  │ │
│  └───────────────────────────────────────────┘ │
└────┬────────────┬────────────┬─────────────────┘
     │            │            │
┌────▼────┐  ┌───▼─────┐  ┌──▼──────┐
│ Client1 │  │ Client2 │  │ Client3 │
│ (user1) │  │ (user2) │  │ (user3) │
└─────────┘  └─────────┘  └─────────┘
```

### Core Components

#### 1. Hub
Central coordinator managing all connections and message routing. It runs in a dedicated goroutine processing registration, unregistration, and message broadcasting through channels.

#### 2. Client
Represents a connected user with:
- WebSocket connection
- Send channel for outbound messages
- Room membership set
- Unique user ID

#### 3. Message Router
Handles different message types (chat, WebRTC signaling, room management) and routes them appropriately.

#### 4. Availability Manager
Tracks users available for WebRTC calls and handles peer matching logic.

### Concurrency Model

- **Hub goroutine**: Single goroutine handling all hub operations (registration, broadcast)
- **Per-client goroutines**: Each client has two goroutines:
  - `readPump`: Reads messages from the WebSocket
  - `writePump`: Writes messages to the WebSocket and handles ping/pong
- **Thread-safe operations**: Mutex protection for shared state access
- **Channel-based communication**: Non-blocking message passing between components

## API Endpoints

The server exposes two HTTP endpoints.

### Health Check

-   **Endpoint**: `GET /health`
-   **Description**: A simple health check endpoint to verify that the server is running.
-   **Success Response**:
    -   **Code**: `200 OK`
    -   **Content**: `{"status": "ok"}`

### WebSocket Upgrade

-   **Endpoint**: `GET /ws`
-   **Description**: This is the primary endpoint for establishing a WebSocket connection with the server. The connection must be upgraded from an HTTP GET request.
-   **Query Parameters**:
    -   `userId` (required): A unique identifier for the client. This ID is used for direct messaging and peer identification.
    -   `room` (optional): The name of the chat room the client wishes to join. If not provided, the client defaults to a room named "Lobby".
-   **Example Connection URL**:
    ```
    ws://<server-address>:8085/ws?userId=user123&room=my-chat-room
    ```

## WebSocket Message Structure

All communication over the WebSocket connection is done via JSON-formatted messages. Each message adheres to a generic structure, defined by the `Message` object.

```json
{
  "type": "message_type_here",
  "fromUser": "sender_client_id",
  "toUser": "recipient_client_id",
  "room": "chat_room_id",
  "payload": {},
  "messageId": "client_generated_uuid",
  "createTime": "2023-10-27T10:00:00Z",
  "incoming": true
}
```

### Fields

-   `type` (string, required): Defines the purpose of the message. The server uses this field to route the message to the correct handler. See the "Message Types" section for a full list of possible values.
-   `fromUser` (string, optional): The `userId` of the client sending the message. While optional in the client-sent message, the server will populate this field with the sender's connection ID before broadcasting or routing the message.
-   `toUser` (string, optional): The `userId` of the intended recipient. This is used for direct messaging, such as sending a WebRTC offer to a specific peer.
-   `room` (string, optional): The ID of the chat room. This is used for chat messages that should be broadcast to all members of a room.
-   `payload` (object, optional): A flexible JSON object containing the data specific to the message `type`. The structure of the payload varies depending on the message type.
-   `messageId` (string, optional): A unique identifier for the message, typically generated by the client (e.g., a UUID). This can be used for tracking messages.
-   `createTime` (string, optional): An ISO 8601 formatted timestamp indicating when the message was created. If not provided by the client, the server will add a timestamp.
-   `incoming` (boolean, optional): A client-side flag to indicate if a message is from a remote user. The server does not use this field.

## Message Types and Payloads

The `type` field in the message determines its purpose and the expected structure of its `payload`.

### Chat Messages

#### `chat_message`

-   **Direction**: Client -> Server -> Client(s)
-   **Description**: Sends a text message to a chat room or directly to another user. The server routes the message appropriately.
-   **Message Fields**:
    -   `room` (optional): The target chat room for broadcast messages.
    -   `toUser` (optional): The target user ID for direct messages.
    -   **Note**: Must specify either `room` or `toUser`, but not both.
-   **Payload (`ChatPayload`)**:
    ```json
    {
      "content": "Hello, world!"
    }
    ```
-   **Validation**: 
    -   Sender must be a member of the room to send room messages
    -   Server sends error response if user tries to send to a room they haven't joined
-   **Echo Behavior**: 
    -   Room messages: Broadcast to all room members (including sender)
    -   Direct messages: Copy sent to both sender (with `incoming: false`) and recipient (with `incoming: true`)

### Room Management

#### `join_room`

-   **Direction**: Client -> Server
-   **Description**: Requests to join a specific chat room. Creates the room if it doesn't exist.
-   **Message Fields**:
    -   `room`: The room name to join.
    -   `fromUser`: Automatically populated by server.
-   **Payload**: None required.
-   **Server Response**: Sends `room_joined` confirmation message.

#### `leave_room`

-   **Direction**: Client -> Server
-   **Description**: Leaves a specific chat room. If the room becomes empty, it is automatically deleted.
-   **Message Fields**:
    -   `room`: The room name to leave.
-   **Payload**: None required.
-   **Server Response**: Sends `room_left` confirmation message.

#### `get_rooms`

-   **Direction**: Client -> Server
-   **Description**: Requests a list of all rooms the client is currently in.
-   **Payload**: None required.
-   **Server Response**: `rooms_list` message with array of room names.

#### `room_joined`

-   **Direction**: Server -> Client
-   **Description**: Confirmation that the client successfully joined a room.
-   **Payload (`RoomActionPayload`)**:
    ```json
    {
      "room": "room_name",
      "action": "joined"
    }
    ```

#### `room_left`

-   **Direction**: Server -> Client
-   **Description**: Confirmation that the client successfully left a room.
-   **Payload (`RoomActionPayload`)**:
    ```json
    {
      "room": "room_name",
      "action": "left"
    }
    ```

#### `rooms_list`

-   **Direction**: Server -> Client
-   **Description**: Response to `get_rooms` request containing all rooms the client is in.
-   **Payload**:
    ```json
    {
      "rooms": ["room1", "room2", "room3"]
    }
    ```

### User Availability

#### `user_available_webrtc`

-   **Direction**: Client -> Server
-   **Description**: Notifies the server that the client is available to receive WebRTC call requests.
-   **Payload**: None.

#### `user_unavailable_webrtc`

-   **Direction**: Client -> Server
-   **Description**: Notifies the server that the client is no longer available for WebRTC calls (e.g., the user navigated away or closed the call interface).
-   **Payload**: None.

### Peer Matching

#### `request_random_peer`

-   **Direction**: Client -> Server
-   **Description**: The client requests to be matched with a random, available peer for a WebRTC call.
-   **Payload (`RequestRandomPeerPayload`)**:
    ```json
    {
      "currentPeerId": "previously_connected_peer_id" // Optional
    }
    ```
    - `currentPeerId`: If the user is skipping a peer they were just connected to, their ID can be included here to avoid immediate reconnection.

#### `assign_role`

-   **Direction**: Server -> Client
-   **Description**: Sent by the server to two matched peers. It assigns a role for the WebRTC negotiation process to avoid glare (simultaneous offer collision). One peer is "polite" (the callee) and the other is "impolite" (the caller).
-   **Message Fields**:
    -   `toUser`: The ID of the client receiving their role.
-   **Payload (`AssignRolePayload`)**:
    ```json
    {
      "role": "polite", // or "impolite"
      "peerId": "the_other_peer_id"
    }
    ```

#### `no_peer_available`

-   **Direction**: Server -> Client
-   **Description**: Sent to a client who requested a random peer when no other users are available.
-   **Payload**:
    ```json
    {
      "message": "No peer available at the moment. Please try again later."
    }
    ```

### WebRTC Signaling

These messages are relayed by the server between two specific peers (`fromUser` and `toUser`).

#### `webrtc_offer`

-   **Direction**: Client -> Server -> Client
-   **Description**: Sends a WebRTC session description offer to a peer.
-   **Message Fields**:
    -   `toUser`: The ID of the peer to receive the offer.
-   **Payload (`WebRTCSessionDescriptionPayload`)**:
    ```json
    {
      "type": "offer",
      "sdp": "session_description_protocol_string"
    }
    ```

#### `webrtc_answer`

-   **Direction**: Client -> Server -> Client
-   **Description**: Sends a WebRTC session description answer to a peer.
-   **Message Fields**:
    -   `toUser`: The ID of the peer to receive the answer.
-   **Payload (`WebRTCSessionDescriptionPayload`)**:
    ```json
    {
      "type": "answer",
      "sdp": "session_description_protocol_string"
    }
    ```

#### `webrtc_candidate`

-   **Direction**: Client -> Server -> Client
-   **Description**: Sends an ICE (Interactive Connectivity Establishment) candidate to a peer.
-   **Message Fields**:
    -   `toUser`: The ID of the peer to receive the candidate.
-   **Payload (`WebRTCIceCandidatePayload`)**:
    ```json
    {
      "candidate": { ... }, // The RTCIceCandidateInit object
      "sdpMid": "sdp_media_id", // Optional
      "sdpMLineIndex": 0, // Optional
      "usernameFragment": "fragment" // Optional
    }
    ```

#### `hangup`

-   **Direction**: Client -> Server -> Client
-   **Description**: Notifies a peer that the call has ended. Used to gracefully terminate WebRTC connections.
-   **Message Fields**:
    -   `toUser`: The ID of the peer to notify.
-   **Payload**: Optional, can include reason or metadata.
-   **Best Practice**: After sending/receiving hangup, clients should:
    1. Close the RTCPeerConnection
    2. Clean up local media streams
    3. Send `user_available_webrtc` to become available for new calls

### Error Handling

#### `error_response`

-   **Direction**: Server -> Client
-   **Description**: Sent by the server if it receives a malformed or invalid message from a client. This includes JSON parsing errors or invalid message structures.
-   **Message Fields**:
    -   `toUser`: Set to the client's ID.
    -   `messageId`: Generated by server with "err-" prefix.
-   **Payload**:
    ```json
    {
      "error": "Invalid message structure",
      "details": "json: cannot unmarshal string into Go value of type main.Message"
    }
    ```
-   **Common Triggers**:
    -   Invalid JSON syntax
    -   Missing required fields
    -   Type mismatches in payload

#### `error`

-   **Direction**: Server -> Client
-   **Description**: General error message for various validation failures or business logic errors.
-   **Message Fields**:
    -   `fromUser`: "system"
    -   `toUser`: The client receiving the error.
-   **Payload Examples**:
    ```json
    {
      "error": "You are not in that room. Join the room first."
    }
    ```
-   **Common Scenarios**:
    -   Attempting to send messages to rooms not joined
    -   Permission violations
    -   Resource not found

## WebRTC Signaling Flow

The following sequence describes how two clients (Peer A and Peer B) establish a WebRTC connection through the server.

1.  **Connection and Availability**
    -   Peer A connects to the WebSocket: `GET /ws?userId=peerA`
    -   Peer B connects to the WebSocket: `GET /ws?userId=peerB`
    -   Peer A sends a `user_available_webrtc` message to signal it's ready for calls.
    -   Peer B also sends a `user_available_webrtc` message.

2.  **Peer Matching**
    -   Peer A decides to start a call and sends a `request_random_peer` message.
    -   The server finds that Peer B is available. It removes both Peer A and Peer B from the available pool to prevent them from being matched with others.
    -   The server sends an `assign_role` message to Peer A with `{"role": "impolite", "peerId": "peerB"}`. The "impolite" peer is responsible for creating the WebRTC offer.
    -   The server sends an `assign_role` message to Peer B with `{"role": "polite", "peerId": "peerA"}`.

3.  **SDP Offer/Answer Exchange**
    -   Peer A (the "impolite" peer) creates an SDP offer.
    -   Peer A sends a `webrtc_offer` message with `toUser: "peerB"` and the SDP offer in the payload.
    -   The server relays this message to Peer B.
    -   Peer B receives the offer, sets its remote description, and creates an SDP answer.
    -   Peer B sends a `webrtc_answer` message with `toUser: "peerA"` and the SDP answer in the payload.
    -   The server relays this message to Peer A.
    -   Peer A receives the answer and sets its remote description. The basic connection is now established.

4.  **ICE Candidate Exchange**
    -   As Peer A and Peer B's ICE agents gather candidates (potential network paths), they send them to the other peer.
    -   Peer A's client generates an ICE candidate and sends a `webrtc_candidate` message with `toUser: "peerB"`.
    -   The server relays the candidate to Peer B, who adds it to its peer connection.
    -   Peer B's client generates an ICE candidate and sends a `webrtc_candidate` message with `toUser: "peerA"`.
    -   The server relays the candidate to Peer A, who adds it to its peer connection.
    -   This process repeats until both peers have exchanged enough candidates to find a suitable path for media to flow directly between them.

5.  **Call Termination**
    -   When one peer wants to end the call, they send a `hangup` message with `toUser` set to the other peer's ID.
    -   The server relays the hangup message to the other peer.
    -   Both clients should:
        -   Close their local `RTCPeerConnection`
        -   Stop and clean up local media streams
        -   Update their UI to reflect the call has ended
    -   To become available for new calls, each client must send a `user_available_webrtc` message to the server again.
    -   **Important**: Users are automatically removed from the available pool when matched, preventing duplicate connections.

## Security Features

The server implements multiple security layers to protect against common vulnerabilities and ensure safe operation.

### Origin Validation

**Implementation**: `ValidateOrigin()` function in `validation.go`

The server enforces strict origin checking for WebSocket upgrade requests:

```go
var allowedOrigins = []string{"https://www.aleatoria.chat"}
```

**Features**:
- ✅ Validates Origin header is present
- ✅ Parses and validates URL structure
- ✅ Compares scheme (http/https)
- ✅ Compares hostname
- ✅ Normalizes and compares ports (defaults: 80 for http, 443 for https)
- ✅ Rejects connections from unauthorized origins
- ✅ Comprehensive logging of all connection attempts

**Example Validation**:
```
✅ Allowed: https://www.aleatoria.chat
✅ Allowed: https://www.aleatoria.chat:443 (normalized)
❌ Rejected: http://www.aleatoria.chat (wrong scheme)
❌ Rejected: https://evil.com (unauthorized origin)
❌ Rejected: (no Origin header)
```

### Connection Security

**Timeouts and Limits**:
- **Handshake Timeout**: 10 seconds (prevents slow handshake attacks)
- **Read Deadline**: 60 seconds with automatic reset on pong (prevents zombie connections)
- **Write Deadline**: 10 seconds (prevents blocking writes)
- **Ping Interval**: 54 seconds (keeps connections alive and detects disconnects)

**Buffer Limits**:
- **Read/Write Buffers**: 1024 bytes (prevents memory exhaustion)
- **Send Channel Buffer**: 256 messages per client (prevents unbounded memory growth)

### Message Validation

**Server-Authoritative Fields**:
The server overrides client-provided values for security-critical fields:

```go
msg.FromUser = c.id  // Always set by server based on connection
msg.CreateTime = time.Now().Format(time.RFC3339Nano)  // Server timestamp
```

This prevents:
- ❌ Spoofing messages from other users
- ❌ Timestamp manipulation
- ❌ Identity impersonation

**Input Validation**:
- Validates JSON structure before processing
- Type-checks message payloads
- Validates room membership before allowing chat
- Checks user existence before routing direct messages

### Concurrency Safety

**Thread-Safe Operations**:
- Mutex-protected access to shared maps (`clients`, `clientsByID`, `rooms`)
- Separate mutex for availability tracking (`availableForCallMu`)
- Channel-based communication prevents race conditions
- Non-blocking sends with default cases to prevent deadlocks

### Error Handling

**Graceful Degradation**:
- Invalid messages don't crash the server
- Malformed JSON triggers `error_response` to client
- Unexpected WebSocket close errors are logged and handled
- Full send channels skip the message rather than blocking

**Information Disclosure Prevention**:
- Errors sent to clients don't expose internal server state
- Structured error messages with safe details only
- Comprehensive server-side logging for debugging

### Best Practices for Production

1. **Use TLS/HTTPS**: Deploy behind a reverse proxy (nginx, Caddy) with TLS termination
2. **Environment-based Origins**: Load `allowedOrigins` from environment variables
3. **Rate Limiting**: Implement rate limiting at the reverse proxy level
4. **Monitoring**: Use the `/health` endpoint for health checks
5. **Authentication**: Add authentication layer before WebSocket upgrade
6. **IP Filtering**: Consider IP allowlisting at firewall level
7. **DDoS Protection**: Use CDN with DDoS protection (Cloudflare, etc.)
8. **Resource Limits**: Set maximum concurrent connections per IP

## Testing

The project includes comprehensive test coverage for all major features.

### Test Suite Overview

**Files**:
- `main_test.go`: Core functionality tests
- `validation_test.go`: Origin validation tests

**Coverage**:
- ✅ Health endpoint
- ✅ WebSocket connection establishment
- ✅ Chat message routing (room and direct)
- ✅ Multi-room support
- ✅ WebRTC peer matching and role assignment
- ✅ WebRTC signaling (offer/answer/candidate)
- ✅ Origin validation with multiple scenarios

### Running Tests

**Run all tests**:
```bash
go test -v
```

**Run specific test**:
```bash
go test -v -run TestWebRTC_PeerToPeerFlow
```

**Run with coverage**:
```bash
go test -v -cover
go test -coverprofile=coverage.out
go tool cover -html=coverage.out
```

**Test with race detector**:
```bash
go test -race -v
```

### Key Test Cases

#### 1. Health Check Test
Verifies the `/health` endpoint returns proper JSON response.

#### 2. WebSocket Connection Test
Tests client registration, ID mapping, and initial room assignment.

#### 3. Chat Message Test
Validates message broadcasting within rooms with proper `fromUser` field population.

#### 4. WebRTC Flow Test
Complete end-to-end test including:
- User availability declaration
- Random peer matching
- Role assignment (polite/impolite)
- Offer/answer exchange
- ICE candidate exchange

#### 5. Origin Validation Test
Comprehensive validation scenarios:
- Allowed origins
- Subdomain handling
- Scheme validation
- Port normalization
- Missing/malformed origins

### Manual Testing

**Using wscat**:
```bash
# Install wscat
npm install -g wscat

# Connect to server
wscat -c "ws://localhost:8085/ws?userId=testuser&room=lobby" --origin https://www.aleatoria.chat

# Send test message
{"type":"chat_message","room":"lobby","payload":{"content":"Hello"}}
```

**Using JavaScript**:
```javascript
const ws = new WebSocket('ws://localhost:8085/ws?userId=user123&room=lobby');

ws.onopen = () => {
  console.log('Connected');
  ws.send(JSON.stringify({
    type: 'chat_message',
    room: 'lobby',
    payload: { content: 'Hello, server!' }
  }));
};

ws.onmessage = (event) => {
  console.log('Received:', JSON.parse(event.data));
};
```

## Deployment

### Production Deployment Checklist

#### 1. Configuration
- [ ] Set `allowedOrigins` from environment variables
- [ ] Configure appropriate server port
- [ ] Set up proper logging (use structured logging library)
- [ ] Configure resource limits

#### 2. Infrastructure
- [ ] Deploy behind reverse proxy (nginx/Caddy) with TLS
- [ ] Set up load balancer if scaling horizontally
- [ ] Configure health check monitoring
- [ ] Set up log aggregation (ELK, Splunk, etc.)
- [ ] Configure metrics collection (Prometheus, Grafana)

#### 3. Security
- [ ] Enable HTTPS only
- [ ] Configure CORS properly
- [ ] Implement rate limiting
- [ ] Set up DDoS protection
- [ ] Configure firewall rules
- [ ] Add authentication layer

#### 4. Monitoring
- [ ] Set up uptime monitoring
- [ ] Configure alerts for connection failures
- [ ] Monitor WebSocket connection count
- [ ] Track message throughput
- [ ] Monitor memory and CPU usage

### Docker Deployment

**Dockerfile**:
```dockerfile
FROM golang:1.24.3-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o aleatoria-server .

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/

COPY --from=builder /app/aleatoria-server .

EXPOSE 8085
CMD ["./aleatoria-server"]
```

**Build and run**:
```bash
docker build -t aleatoria-server .
docker run -p 8085:8085 aleatoria-server
```

### Nginx Reverse Proxy Configuration

```nginx
upstream aleatoria_backend {
    server localhost:8085;
}

server {
    listen 443 ssl http2;
    server_name www.aleatoria.chat;

    ssl_certificate /path/to/cert.pem;
    ssl_certificate_key /path/to/key.pem;

    location /ws {
        proxy_pass http://aleatoria_backend;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        
        # Timeouts
        proxy_connect_timeout 7d;
        proxy_send_timeout 7d;
        proxy_read_timeout 7d;
    }

    location /health {
        proxy_pass http://aleatoria_backend;
    }
}
```

### Systemd Service

**`/etc/systemd/system/aleatoria-server.service`**:
```ini
[Unit]
Description=Aleatoria WebSocket Chat Server
After=network.target

[Service]
Type=simple
User=aleatoria
WorkingDirectory=/opt/aleatoria
ExecStart=/opt/aleatoria/aleatoria-server
Restart=always
RestartSec=10

# Security hardening
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/var/log/aleatoria

[Install]
WantedBy=multi-user.target
```

**Commands**:
```bash
sudo systemctl daemon-reload
sudo systemctl enable aleatoria-server
sudo systemctl start aleatoria-server
sudo systemctl status aleatoria-server
```

### Environment-Based Configuration Example

**Update `validation.go`**:
```go
import (
    "os"
    "strings"
)

var allowedOrigins = getOrigins()

func getOrigins() []string {
    origins := os.Getenv("ALLOWED_ORIGINS")
    if origins == "" {
        return []string{"https://www.aleatoria.chat"}
    }
    return strings.Split(origins, ",")
}
```

**Set environment variable**:
```bash
export ALLOWED_ORIGINS="https://www.aleatoria.chat,https://app.aleatoria.chat"
./aleatoria-server
```

### Scaling Considerations

**Horizontal Scaling Challenges**:
The current implementation uses in-memory state, which makes horizontal scaling challenging. For multi-instance deployment:

1. **Redis for Shared State**: Use Redis to store:
   - Active connections mapping
   - Room memberships
   - Available users for WebRTC

2. **Message Bus**: Use Redis Pub/Sub or RabbitMQ for cross-server messaging

3. **Sticky Sessions**: Configure load balancer for WebSocket sticky sessions

4. **Service Mesh**: Consider using a service mesh for advanced routing

**Vertical Scaling**:
Single instance can handle thousands of concurrent connections. Monitor:
- File descriptor limits (`ulimit -n`)
- Memory usage (each connection ~10-50KB)
- CPU usage for JSON serialization/deserialization

## Go Data Structures

This section provides documentation for the core data structures used in the Go server code, presented in a style similar to official Go documentation.

### type `Client`

```go
type Client struct {
    conn  *websocket.Conn
    send  chan []byte
    rooms map[string]bool
    id    string
    hub   *Hub
}
```

`Client` represents a single connected user. It holds the WebSocket connection, a buffered channel for outbound messages, identifiers, and room memberships.

-   `conn`: The underlying WebSocket connection.
-   `send`: A buffered channel (capacity: 256) where JSON-formatted byte slices are placed to be sent to the client.
-   `rooms`: A map representing the set of chat rooms the client is currently in. The map key is the room ID.
-   `id`: The unique `userId` for this client, provided via query parameter during connection.
-   `hub`: Reference to the hub for room management operations.

### type `Hub`

```go
type Hub struct {
    clients     map[*Client]bool
    clientsByID map[string]*Client
    broadcast   chan Message
    register    chan *Client
    unregister  chan *Client
    rooms       map[string]map[*Client]bool
    mu          sync.Mutex
}
```

`Hub` is the central component that manages all clients and message broadcasting.

-   `clients`: A set of all currently connected clients.
-   `clientsByID`: Maps `userId` strings to their corresponding `Client` pointers for direct access.
-   `broadcast`: A channel that receives messages from clients. The hub processes these messages.
-   `register`: A channel for new clients waiting to be registered.
-   `unregister`: A channel for clients that have disconnected and need to be cleaned up.
-   `rooms`: A map where keys are room IDs and values are sets of clients in that room.
-   `mu`: A mutex to synchronize access to shared resources like the client and room maps.

### type `Message`

```go
type Message struct {
    Type       string      `json:"type"`
    FromUser   string      `json:"fromUser,omitempty"`
    ToUser     string      `json:"toUser,omitempty"`
    Room       string      `json:"room,omitempty"`
    Payload    interface{} `json:"payload,omitempty"`
    MessageID  string      `json:"messageId,omitempty"`
    CreateTime string      `json:"createTime,omitempty"`
    Incoming   bool        `json:"incoming,omitempty"`
}
```

`Message` is the generic structure for all WebSocket messages exchanged between clients and the server. See the "WebSocket Message Structure" section for field details.

### Payload Structs

These structs define the `payload` for specific message types.

```go
// For "chat_message"
type ChatPayload struct {
    Content string `json:"content"`
}

// For "webrtc_offer" and "webrtc_answer"
type WebRTCSessionDescriptionPayload struct {
    Type string `json:"type"` // "offer" or "answer"
    Sdp  string `json:"sdp"`
}

// For "webrtc_candidate"
type WebRTCIceCandidatePayload struct {
    Candidate        interface{} `json:"candidate"`
    SdpMid           *string     `json:"sdpMid,omitempty"`
    SdpMLineIndex    *uint16     `json:"sdpMLineIndex,omitempty"`
    UsernameFragment *string     `json:"usernameFragment,omitempty"`
}

// For "assign_role"
type AssignRolePayload struct {
    Role   string `json:"role"`
    PeerId string `json:"peerId"`
}

// For "request_random_peer"
type RequestRandomPeerPayload struct {
    CurrentPeerID string `json:"currentPeerId,omitempty"`
}

// For user status updates
type UserStatusPayload struct {
    UserID    string `json:"userId"`
    Status    string `json:"status"`
    Timestamp string `json:"timestamp"`
}

// For room management (join_room, leave_room responses)
type RoomActionPayload struct {
    Room   string `json:"room"`
    Action string `json:"action"`
}
```

## Additional Resources

### Performance Characteristics

**Connection Limits**:
- Theoretical: Limited by file descriptors and memory
- Practical: ~10,000 concurrent connections per instance (varies by hardware)
- Memory per connection: ~10-50KB (depending on activity)

**Latency**:
- Message routing: < 1ms (in-memory operations)
- WebSocket overhead: ~1-5ms (network dependent)
- JSON serialization: ~100-500μs per message

**Throughput**:
- Messages per second: 10,000+ (single instance)
- Bottleneck: JSON marshaling/unmarshaling
- Optimization: Consider MessagePack for binary protocol

### Common Issues and Troubleshooting

**Issue: Connections rejected with "Rejecting connection from origin"**
- **Cause**: Origin validation failing
- **Solution**: Add the frontend origin to `allowedOrigins` in `validation.go`

**Issue: "Client disconnected due to full send channel"**
- **Cause**: Client not reading messages fast enough
- **Solution**: Client should handle messages asynchronously, increase buffer if needed

**Issue: WebSocket connection closes after 60 seconds**
- **Cause**: Client not responding to ping messages
- **Solution**: Implement pong handler on client side

**Issue: "No peer available" when peers exist**
- **Cause**: Users not sending `user_available_webrtc` message
- **Solution**: Ensure clients declare availability before requesting peers

### Future Enhancements

Potential improvements for the project:

1. **Redis Integration**: For horizontal scaling and persistence
2. **Authentication**: JWT-based authentication before WebSocket upgrade
3. **Rate Limiting**: Per-user message rate limits
4. **Presence System**: Advanced online/offline/typing indicators
5. **Message History**: Store and retrieve chat history
6. **File Sharing**: Support for file transfer over WebRTC data channels
7. **Admin Dashboard**: Web UI for monitoring connections and rooms
8. **Metrics Export**: Prometheus metrics endpoint
9. **Structured Logging**: Use zerolog or zap for better log management
10. **GraphQL Subscriptions**: Alternative to WebSocket for some clients

### Contributing

When contributing to this project:

1. **Code Style**: Follow standard Go conventions (`gofmt`, `golint`)
2. **Testing**: Add tests for new features
3. **Documentation**: Update this README for API changes
4. **Commit Messages**: Use descriptive commit messages
5. **Backward Compatibility**: Avoid breaking changes to the message protocol

### License

[Add your license information here]

### Support

For questions, issues, or contributions:
- Open an issue on the repository
- Check existing issues before creating new ones
- Provide detailed information for bug reports

---

**Project**: Aleatoria WebSocket Chat Server  
**Version**: 1.0  
**Last Updated**: 2025  
**Maintainer**: [Your Name/Team]
