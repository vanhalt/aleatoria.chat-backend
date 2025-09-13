#!/bin/bash

# Test script for Direct Message functionality
# This script tests that the WebSocket server properly routes direct messages

echo "Testing Direct Message functionality..."
echo "This script will simulate two users sending DMs to each other"
echo ""

# Start the server in the background if not already running
echo "Make sure the WebSocket server is running on :8085"
echo ""

# Test 1: Connect two users and send a DM
echo "Test 1: Basic DM between two users"
echo "=================================="

# Using wscat to test - install with: npm install -g wscat
# User 1 connects
echo "User1 connecting..."
echo "wscat -c 'ws://localhost:8085/ws?userId=user1&room=Lobby'"
echo ""

# User 2 connects  
echo "User2 connecting..."
echo "wscat -c 'ws://localhost:8085/ws?userId=user2&room=Lobby'"
echo ""

# Example message to send from User1 to User2:
echo "User1 should send this JSON to DM User2:"
echo '{"type":"chat_message","fromUser":"user1","toUser":"user2","payload":{"content":"Hello User2, this is a DM!"},"messageId":"test-dm-1","createTime":"2025-09-13T10:00:00Z","incoming":false}'
echo ""

# Example message to send from User2 to User1:
echo "User2 should send this JSON to reply:"
echo '{"type":"chat_message","fromUser":"user2","toUser":"user1","payload":{"content":"Hi User1, I got your DM!"},"messageId":"test-dm-2","createTime":"2025-09-13T10:00:01Z","incoming":false}'
echo ""

echo "Expected behavior:"
echo "- User2 should receive the DM from User1"
echo "- User1 should also receive a copy of their sent message (marked as outgoing)"
echo "- User1 should receive the reply from User2"
echo "- No other users in the room should see these DMs"
echo ""

echo "To run this test manually:"
echo "1. Open two terminal windows"
echo "2. Run the wscat commands above in each window"
echo "3. Copy and paste the JSON messages to test DM functionality"
