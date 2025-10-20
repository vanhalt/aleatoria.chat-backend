# Sentry Integration Summary

## Overview
Comprehensive Sentry integration has been successfully implemented across the Aleatoria WebSocket backend with error tracking, panic recovery, breadcrumbs, and rich contextual information.

## What Was Implemented

### 1. Sentry SDK Installation ✅
- Installed `github.com/getsentry/sentry-go v0.36.0`
- Updated `go.mod` and `go.sum`

### 2. Initialization (main.go) ✅
- Sentry initialized in `main()` function with DSN
- Environment set to "production" (configurable)
- Traces sample rate: 100%
- `defer sentry.Flush(2 * time.Second)` ensures events are sent before shutdown
- Startup verification message: "Aleatoria backend started successfully"
- Server errors captured before fatal exit

### 3. Helper Functions (main.go) ✅
Created three utility functions for consistent Sentry usage:

#### `captureErrorWithContext(err, message, tags, extra)`
- Centralized error capture with tags and extra context
- Used throughout codebase for consistent error reporting

#### `addBreadcrumb(category, message, level, data)`
- Wrapper for adding breadcrumbs to trace application flow
- Categories: websocket, room, message, webrtc, redis

#### `recoverWithSentry(componentName, additionalContext)`
- Panic recovery helper for goroutines
- Converts panics to errors and reports to Sentry with context
- Used in: `hub.run()`, `readPump()`, `writePump()`

### 4. Panic Recovery ✅
Added panic recovery with Sentry reporting to all goroutines:
- **hub.run()**: Main hub loop with client/room counts
- **readPump()**: Per-client read loop with userId and remoteAddr
- **writePump()**: Per-client write loop with userId and remoteAddr

### 5. Error Capture in main.go ✅
Instrumented all critical error points:

#### WebSocket Operations
- Upgrade errors in `serveWs()` with userId and room context
- Unexpected close errors in `readPump()`
- Message unmarshalling errors with raw message data
- Write errors in `writePump()`

#### Message Processing
- Message marshalling errors in `sendToClientUnsafe()` and broadcast handlers
- Full send channel errors (stuck/slow clients)
- Unknown message types for monitoring

#### Room Operations
- Failed room join/leave persistence to Redis
- Unauthorized room access attempts

#### WebRTC Operations
- Failed availability list updates
- Peer pairing errors with exclude list context
- No peers available situations

#### Message Retrieval
- Room message retrieval errors
- Direct message retrieval errors

### 6. Breadcrumbs ✅
Added breadcrumbs at key flow points:

#### Connection Events
- Client connection (userId, remoteAddr, initialRooms, totalClients)
- Client disconnection (userId, rooms, totalClients)
- New WebSocket connection attempts

#### Room Events
- Room join (userId, room, roomClients)
- Room leave (userId, room)

#### Message Events
- Direct message sent (fromUser, toUser, type)
- Room message broadcast (fromUser, room, roomClients, type)

#### WebRTC Events
- User marked available/unavailable (userId)
- WebRTC signaling messages (fromUser, toUser, type)
- Random peer requested (userId, currentPeerId)
- Peer pairing successful (requester, selectedPeer)
- No peer available (userId)

### 7. Contextual Tags ✅
All Sentry events include relevant tags:

- **userId**: User ID involved in the operation
- **room**: Room name (when applicable)
- **messageType**: Type of WebSocket message
- **operation**: Specific operation that failed
- **component**: Component/goroutine where panic occurred

Extra data includes:
- Remote addresses
- Message IDs
- Available peer counts
- Room client counts
- Raw message data (for parsing errors)

### 8. Redis Store Instrumentation (redis_store.go) ✅
Added comprehensive Sentry instrumentation:

#### Connection
- Redis connection failure/success with breadcrumb
- Configuration context (address, db)

#### Room Operations
- `AddUserToRoom()`: Error capture with userId and room tags
- `RemoveUserFromRoom()`: Error capture with userId and room tags

#### Message Persistence
- `SaveRoomMessage()`: Error capture for marshalling, saving, and TTL operations
- `SaveDirectMessage()`: Error capture for marshalling, saving, and TTL operations

#### WebRTC Available Pool
- `AddUserToAvailable()`: Error capture + breadcrumb for successful addition
- `RemoveUserFromAvailable()`: Error capture
- `GetRandomAvailablePeer()`: 
  - Error capture for Redis failures
  - Breadcrumb when no peers available
  - Breadcrumb for successful peer selection with metrics

## Testing

### Compilation
✅ Code compiles successfully without errors

### Verification Steps
To verify Sentry is working:

1. **Start the server**: Messages should appear in Sentry dashboard
   - "Aleatoria backend started successfully" message

2. **Test error capture**: Trigger an error (e.g., malformed JSON message)
   - Should appear in Sentry with full context

3. **Test breadcrumbs**: Perform normal operations
   - Breadcrumbs should provide full trace of user actions

4. **Test panic recovery**: If any goroutine panics
   - Panic captured and reported to Sentry
   - Server continues running (for readPump/writePump)

## Configuration

### Required Environment Variables

#### Redis/Valkey Configuration (Required for persistence)
The application uses these environment variables for Redis/Valkey connectivity:

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `VALKEY_HOST` | No | `localhost` | Redis/Valkey server hostname or IP address |
| `VALKEY_PORT` | No | `6379` | Redis/Valkey server port |
| `VALKEY_PASSWORD` | No | `""` (empty) | Redis/Valkey authentication password |
| `REDIS_DB` | No | `0` | Redis database number (0-15) |
| `MESSAGE_TTL_HOURS` | No | `24` | Time-to-live for stored messages in hours |

**Example:**
```bash
export VALKEY_HOST="redis.example.com"
export VALKEY_PORT="6379"
export VALKEY_PASSWORD="your-secure-password"
export REDIS_DB="0"
export MESSAGE_TTL_HOURS="48"
```

#### Sentry Configuration (Currently Hardcoded)

**Current Implementation:**
- Sentry DSN is hardcoded in `main.go`
- Environment is set to "production"
- Debug mode is `false`

**Recommended: Use Environment Variables**

To make Sentry configuration more flexible, you can modify `main.go` to use these environment variables:

| Variable | Recommended | Default | Description |
|----------|-------------|---------|-------------|
| `SENTRY_DSN` | Yes | Hardcoded | Sentry Data Source Name for error reporting |
| `SENTRY_ENV` | Yes | `production` | Environment name (production, staging, development) |
| `SENTRY_DEBUG` | No | `false` | Enable verbose Sentry debugging |
| `SENTRY_SAMPLE_RATE` | No | `1.0` | Trace sample rate (0.0 to 1.0) |

**To implement environment variable support, update main.go:**
```go
err := sentry.Init(sentry.ClientOptions{
    Dsn:              getEnv("SENTRY_DSN", "https://6245ca95ad3f27465a22b2da1eefecad@o4510162358894592.ingest.us.sentry.io/4510162360795136"),
    Environment:      getEnv("SENTRY_ENV", "production"),
    TracesSampleRate: getEnvFloat("SENTRY_SAMPLE_RATE", 1.0),
    Debug:            getEnvBool("SENTRY_DEBUG", false),
})

// Helper function
func getEnv(key, defaultValue string) string {
    if value := os.Getenv(key); value != "" {
        return value
    }
    return defaultValue
}
```

**Example:**
```bash
export SENTRY_DSN="https://your-dsn@sentry.io/project-id"
export SENTRY_ENV="production"
export SENTRY_DEBUG="false"
export SENTRY_SAMPLE_RATE="1.0"
```

### Complete Environment Setup

For a full production deployment, set all variables:

```bash
# Redis/Valkey
export VALKEY_HOST="your-redis-host.com"
export VALKEY_PORT="6379"
export VALKEY_PASSWORD="your-redis-password"
export REDIS_DB="0"
export MESSAGE_TTL_HOURS="24"

# Sentry (if using environment variables)
export SENTRY_DSN="https://your-dsn@sentry.io/project-id"
export SENTRY_ENV="production"
export SENTRY_DEBUG="false"
export SENTRY_SAMPLE_RATE="1.0"
```

### Docker/Container Environment

If deploying with Docker, add to your `docker-compose.yml`:

```yaml
services:
  aleatoria-backend:
    image: aleatoria-backend:latest
    environment:
      # Redis/Valkey
      - VALKEY_HOST=redis
      - VALKEY_PORT=6379
      - VALKEY_PASSWORD=${VALKEY_PASSWORD}
      - REDIS_DB=0
      - MESSAGE_TTL_HOURS=24
      
      # Sentry
      - SENTRY_DSN=${SENTRY_DSN}
      - SENTRY_ENV=production
      - SENTRY_DEBUG=false
      - SENTRY_SAMPLE_RATE=1.0
    ports:
      - "8085:8085"
```

### Kubernetes Deployment

For Kubernetes, create a ConfigMap and Secret:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: aleatoria-config
data:
  VALKEY_HOST: "redis-service"
  VALKEY_PORT: "6379"
  REDIS_DB: "0"
  MESSAGE_TTL_HOURS: "24"
  SENTRY_ENV: "production"
  SENTRY_DEBUG: "false"
  SENTRY_SAMPLE_RATE: "1.0"
---
apiVersion: v1
kind: Secret
metadata:
  name: aleatoria-secrets
type: Opaque
stringData:
  VALKEY_PASSWORD: "your-redis-password"
  SENTRY_DSN: "https://your-dsn@sentry.io/project-id"
```

### Environment-Specific Configurations

#### Development
```bash
export VALKEY_HOST="localhost"
export VALKEY_PORT="6379"
export SENTRY_ENV="development"
export SENTRY_DEBUG="true"
export SENTRY_SAMPLE_RATE="1.0"  # 100% sampling for testing
```

#### Staging
```bash
export VALKEY_HOST="staging-redis.internal"
export VALKEY_PORT="6379"
export VALKEY_PASSWORD="staging-password"
export SENTRY_ENV="staging"
export SENTRY_DEBUG="false"
export SENTRY_SAMPLE_RATE="0.5"  # 50% sampling
```

#### Production
```bash
export VALKEY_HOST="production-redis.internal"
export VALKEY_PORT="6379"
export VALKEY_PASSWORD="production-password"
export REDIS_DB="0"
export MESSAGE_TTL_HOURS="48"
export SENTRY_ENV="production"
export SENTRY_DEBUG="false"
export SENTRY_SAMPLE_RATE="0.1"  # 10% sampling to reduce load
```

## Sentry Dashboard
Events will appear at: https://sentry.io/organizations/[org]/projects/

### Event Categories
- **Errors**: All captured errors with tags and context
- **Performance**: Transaction tracing (100% sample rate)
- **Breadcrumbs**: Available on each event for flow tracing

### Useful Filters
- `userId:[id]` - Filter by user
- `room:[name]` - Filter by room
- `operation:[type]` - Filter by operation
- `component:[name]` - Filter by component

## Performance Impact
- Minimal overhead for breadcrumbs and error capture
- Asynchronous event sending doesn't block operations
- 2-second flush timeout on shutdown ensures events are sent

## Key Benefits
1. **Full visibility** into production errors and panics
2. **Rich context** for debugging with tags and breadcrumbs
3. **User-centric tracing** with userId tracking
4. **Performance monitoring** with transaction tracing
5. **Proactive monitoring** of edge cases (no peers, full channels, etc.)

## Next Steps (Optional Enhancements)
1. Add user context with `sentry.SetUser()` for better tracking
2. Add release tracking with Git commit SHA
3. Add custom metrics for key operations
4. Set up Sentry alerts for critical errors
5. Add environment variable configuration for DSN and environment

---

## Quick Reference

### Minimum Required Setup (Development)

The application will work with **zero configuration** using these defaults:
- Redis/Valkey: `localhost:6379` (no password)
- Sentry: Hardcoded DSN (production environment)

**Just run:**
```bash
go run main.go redis_store.go validation.go
```

### Minimum Required Setup (Production)

For production, you should at minimum set:

```bash
# Required for Redis/Valkey if not on localhost
export VALKEY_HOST="your-redis-host"
export VALKEY_PASSWORD="your-redis-password"

# Run the server
./aleatoria-backend
```

### All Environment Variables Reference

| Variable | Default | Type | Description |
|----------|---------|------|-------------|
| **Redis/Valkey** | | | |
| `VALKEY_HOST` | `localhost` | String | Redis server hostname |
| `VALKEY_PORT` | `6379` | String | Redis server port |
| `VALKEY_PASSWORD` | `""` | String | Redis password (optional) |
| `REDIS_DB` | `0` | Integer | Redis database number |
| `MESSAGE_TTL_HOURS` | `24` | Integer | Message retention hours |
| **Sentry** | | | |
| `SENTRY_DSN` | Hardcoded | String | Sentry project DSN (modify code to use) |
| `SENTRY_ENV` | `production` | String | Environment name (modify code to use) |
| `SENTRY_DEBUG` | `false` | Boolean | Sentry debug mode (modify code to use) |
| `SENTRY_SAMPLE_RATE` | `1.0` | Float | Trace sampling rate 0.0-1.0 (modify code to use) |

### Troubleshooting

**Redis Connection Issues:**
- Verify `VALKEY_HOST` and `VALKEY_PORT` are correct
- Check if Redis requires password (set `VALKEY_PASSWORD`)
- Server will log: "WARNING: Valkey/Redis connection failed"
- Server continues to run with degraded functionality (no persistence)

**Sentry Not Reporting:**
- Check DSN is correct in `main.go` (or `SENTRY_DSN` if using env vars)
- Look for "Sentry initialized successfully" in logs
- Verify firewall allows HTTPS to `sentry.io`
- Check Sentry dashboard for events

**Missing Events in Sentry:**
- Events are sent asynchronously
- Wait 2-5 seconds after event occurs
- Check `SENTRY_SAMPLE_RATE` isn't too low
- Verify environment filter in Sentry dashboard

### Health Check

The server provides a health endpoint:
```bash
curl http://localhost:8085/health
# Response: {"status":"ok"}
```

