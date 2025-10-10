# Quick Start: Redis Integration

## What Was Added

Redis/Valkey storage for:
- ✅ Room messages (24h retention)
- ✅ Direct messages (24h retention)
- ✅ Room membership tracking
- ✅ WebRTC peer matching pool

## Before You Run

### 1. Download Dependencies
```bash
go mod tidy
```

### 2. Set Environment Variables
```bash
export VALKEY_HOST=localhost
export VALKEY_PORT=6379
export VALKEY_PASSWORD=           # Leave empty if no auth
```

### 3. Start Valkey/Redis
```bash
# Using Docker (easiest)
docker run -d -p 6379:6379 valkey/valkey

# Or Homebrew
brew install valkey && brew services start valkey
```

### 4. Run the Server
```bash
go run main.go redis_store.go validation.go
```

You should see:
```
Valkey/Redis connected successfully at localhost:6379
HTTP server started on :8085
```

## Testing It Works

### Test Persistence
```javascript
// Connect two clients
const ws1 = new WebSocket('ws://localhost:8085/ws?userId=user1&room=lobby');
const ws2 = new WebSocket('ws://localhost:8085/ws?userId=user2&room=lobby');

// Send a message
ws1.send(JSON.stringify({
  type: 'chat_message',
  room: 'lobby',
  payload: { content: 'Hello!' }
}));

// Later, retrieve messages
ws2.send(JSON.stringify({
  type: 'get_recent_messages',
  room: 'lobby',
  payload: { limit: 50 }
}));
// Will receive message history
```

## What If Redis Is Down?

No problem! The server continues working:
- Messages are still broadcast live
- No persistence (messages not saved)
- You'll see a warning in logs

## Next Steps

- See `SETUP.md` for full deployment instructions
- See `REDIS_INTEGRATION.md` for API details
- See `IMPLEMENTATION_SUMMARY.md` for technical details

## Quick Reference

### New Message Types
- `get_recent_messages` - Get room message history
- `get_direct_messages` - Get DM history with another user

### Environment Variables
- `VALKEY_HOST` - Server host (default: localhost)
- `VALKEY_PORT` - Server port (default: 6379)
- `VALKEY_PASSWORD` - Auth password (optional)
- `MESSAGE_TTL_HOURS` - How long to keep messages (default: 24)

That's it! Your server now has persistent storage.

