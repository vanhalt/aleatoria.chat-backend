# Redis/Valkey Integration

This document describes the Redis/Valkey integration for message persistence and state management in the Aleatoria WebSocket server.

## Overview

The server uses Redis (or Valkey, a Redis fork) to provide:
- **Message persistence**: Room messages and direct messages with 24-hour retention
- **Room membership tracking**: Persistent room membership across reconnections
- **WebRTC peer matching**: Available user pool for random video chat pairing
- **Graceful degradation**: If Redis/Valkey is unavailable, the server continues operating with in-memory-only state

## Environment Variables

Configure the connection using these environment variables:

### Required (with defaults)
- `VALKEY_HOST` - Valkey/Redis host (default: `localhost`)
- `VALKEY_PORT` - Valkey/Redis port (default: `6379`)
- `VALKEY_PASSWORD` - Valkey/Redis password (default: empty, no authentication)

### Optional
- `REDIS_DB` - Database number (default: `0`)
- `MESSAGE_TTL_HOURS` - Message retention time in hours (default: `24`)

### Example Configuration

```bash
export VALKEY_HOST=valkey.example.com
export VALKEY_PORT=6379
export VALKEY_PASSWORD=your_secure_password
export MESSAGE_TTL_HOURS=24
```

Or in a `.env` file:
```
VALKEY_HOST=valkey.example.com
VALKEY_PORT=6379
VALKEY_PASSWORD=your_secure_password
MESSAGE_TTL_HOURS=24
```

## Redis Data Structures

The server uses the following Redis keys:

### 1. Room Messages
- **Key**: `room:messages:{roomID}`
- **Type**: Sorted Set
- **Score**: Unix nanosecond timestamp
- **Value**: JSON-encoded message
- **TTL**: 24 hours (configurable)

### 2. Room Members
- **Key**: `room:members:{roomID}`
- **Type**: Set
- **Values**: User IDs

### 3. User Rooms
- **Key**: `user:rooms:{userID}`
- **Type**: Set
- **Values**: Room IDs

### 4. Direct Messages
- **Key**: `dm:{user1}:{user2}` (user IDs lexicographically sorted)
- **Type**: List
- **Values**: JSON-encoded messages
- **TTL**: 24 hours (configurable)

### 5. Available for Call
- **Key**: `available:webrtc`
- **Type**: Set
- **Values**: User IDs of users available for video calls

## New Message Types

### Get Recent Room Messages

Request recent messages from a room:

```json
{
  "type": "get_recent_messages",
  "room": "lobby",
  "payload": {
    "limit": 50,
    "before": "2025-10-10T12:00:00.000Z"
  }
}
```

Response:
```json
{
  "type": "recent_messages",
  "fromUser": "system",
  "room": "lobby",
  "payload": {
    "room": "lobby",
    "messages": [...]
  }
}
```

### Get Direct Messages

Request direct messages with another user:

```json
{
  "type": "get_direct_messages",
  "payload": {
    "withUser": "user123",
    "limit": 50
  }
}
```

Response:
```json
{
  "type": "direct_messages",
  "fromUser": "system",
  "payload": {
    "withUser": "user123",
    "messages": [...]
  }
}
```

## Graceful Degradation

If Redis/Valkey is unavailable:

1. **Writes**: Operations log warnings but don't fail
2. **Reads**: Return empty results
3. **Connections**: Continue working with in-memory state only
4. **No message persistence**: Messages are broadcast but not stored

The server will log:
```
WARNING: Valkey/Redis connection failed: <error>. Will operate with degraded functionality.
```

## Testing

Run Redis/Valkey integration tests:

```bash
# Make sure Valkey/Redis is running locally
docker run -d -p 6379:6379 valkey/valkey

# Run tests
go test -v -run TestRedis
```

Tests will gracefully skip if Valkey/Redis is not available.

## Performance Considerations

### Connection Pooling
- Pool size: 10 connections
- Min idle connections: 2
- Dial timeout: 5 seconds
- Read/Write timeout: 3 seconds

### Message Retention
- Default TTL: 24 hours
- Automatic expiration via Redis TTL
- No manual cleanup required

### Atomic Operations
- User availability uses Redis Sets for atomic add/remove
- Random peer selection is thread-safe
- Room membership updates are atomic

## Monitoring

Key metrics to monitor:

1. **Redis connection status**: Check logs for connection failures
2. **Available user count**: Track `SCARD available:webrtc`
3. **Room membership**: Track `SCARD room:members:{roomID}`
4. **Message counts**: Track `ZCARD room:messages:{roomID}`

## Migration Notes

### From In-Memory to Redis

The current deployment will automatically start persisting to Redis when:
1. Environment variables are configured
2. Redis/Valkey is accessible
3. Server is restarted

No data migration is needed as this is a new feature.

### Valkey vs Redis

Valkey is a Redis fork and is fully compatible with the Redis protocol. The go-redis client works seamlessly with both. The server logs will show "Valkey/Redis" to indicate compatibility with both systems.

## Troubleshooting

### Connection Issues

If you see connection errors:
```
WARNING: Valkey/Redis connection failed: dial tcp: connect: connection refused
```

Check:
1. Valkey/Redis is running: `redis-cli ping` should return `PONG`
2. Host/port are correct
3. Password is correct (if authentication is enabled)
4. Firewall allows connections

### Memory Issues

If Redis is running out of memory:
1. Increase Redis maxmemory setting
2. Adjust MESSAGE_TTL_HOURS to reduce retention
3. Monitor message volume per room

### Performance Issues

If Redis is slow:
1. Check network latency to Redis
2. Monitor Redis CPU/memory usage
3. Consider Redis cluster for high load
4. Adjust connection pool size if needed

