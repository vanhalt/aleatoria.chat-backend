# Setup Instructions

## Prerequisites

- Go 1.24.3 or higher
- Redis or Valkey instance (local or remote)

## Installation Steps

### 1. Install Dependencies

```bash
go mod tidy
```

This will download:
- `github.com/gorilla/websocket` - WebSocket support
- `github.com/redis/go-redis/v9` - Redis/Valkey client

### 2. Configure Environment Variables

Create a `.env` file or set environment variables:

```bash
# Valkey/Redis Configuration
export VALKEY_HOST=localhost
export VALKEY_PORT=6379
export VALKEY_PASSWORD=          # Optional, leave empty for no auth

# Optional Configuration
export REDIS_DB=0                # Database number
export MESSAGE_TTL_HOURS=24      # Message retention time
```

### 3. Start Valkey/Redis (if running locally)

#### Using Docker:
```bash
# Valkey
docker run -d -p 6379:6379 --name valkey valkey/valkey

# Or Redis
docker run -d -p 6379:6379 --name redis redis:latest
```

#### Using Homebrew (macOS):
```bash
brew install valkey
brew services start valkey

# Or Redis
brew install redis
brew services start redis
```

### 4. Build and Run

```bash
# Build
go build -o aleatoria-server

# Run
./aleatoria-server
```

Or run directly:
```bash
go run main.go redis_store.go validation.go
```

### 5. Verify Installation

Check that the server starts and connects to Valkey/Redis:

```
2025/10/10 12:00:00 Valkey/Redis connected successfully at localhost:6379
2025/10/10 12:00:00 HTTP server started on :8085
```

Test the health endpoint:
```bash
curl http://localhost:8085/health
# Should return: {"status":"ok"}
```

## Running Tests

```bash
# Run all tests
go test -v

# Run only Redis tests
go test -v -run TestRedis

# Run with coverage
go test -v -cover
```

Note: Redis/Valkey tests will gracefully skip if the service is not available.

## Development Workflow

### Without Redis/Valkey

The server will operate with graceful degradation:
- Messages are broadcast but not persisted
- Room state is in-memory only
- WebRTC matching uses in-memory pool

You'll see this warning in logs:
```
WARNING: Valkey/Redis connection failed: dial tcp: connect: connection refused. Will operate with degraded functionality.
```

### With Redis/Valkey

Full functionality:
- ✅ Message persistence (24h)
- ✅ Room membership tracking
- ✅ Direct message history
- ✅ WebRTC peer matching

## Deployment

### Environment Variables for Production

```bash
# Required
export VALKEY_HOST=your-valkey-host.example.com
export VALKEY_PORT=6379
export VALKEY_PASSWORD=your-secure-password

# Optional but recommended
export MESSAGE_TTL_HOURS=24
export REDIS_DB=0
```

### Recommended Valkey/Redis Configuration

For production, configure your Redis/Valkey instance with:

```conf
# redis.conf or valkey.conf
maxmemory 256mb
maxmemory-policy allkeys-lru
tcp-keepalive 60
timeout 300
```

### Docker Compose Example

```yaml
version: '3.8'
services:
  valkey:
    image: valkey/valkey:latest
    ports:
      - "6379:6379"
    volumes:
      - valkey-data:/data
    environment:
      - VALKEY_PASSWORD=${VALKEY_PASSWORD}

  aleatoria-backend:
    build: .
    ports:
      - "8085:8085"
    environment:
      - VALKEY_HOST=valkey
      - VALKEY_PORT=6379
      - VALKEY_PASSWORD=${VALKEY_PASSWORD}
      - MESSAGE_TTL_HOURS=24
    depends_on:
      - valkey

volumes:
  valkey-data:
```

## Troubleshooting

### "undefined: redis" errors

Run `go mod tidy` to download dependencies.

### Connection refused

Check that Valkey/Redis is running:
```bash
# For Valkey
redis-cli ping
# Should return: PONG

# Or telnet
telnet localhost 6379
```

### Authentication errors

Verify VALKEY_PASSWORD matches your instance configuration.

### Out of memory

Increase Redis maxmemory or reduce MESSAGE_TTL_HOURS.

## Next Steps

- See [REDIS_INTEGRATION.md](REDIS_INTEGRATION.md) for detailed Redis/Valkey integration documentation
- See [README.md](README.md) for WebSocket API documentation
- Check `validation.go` for CORS origin configuration

