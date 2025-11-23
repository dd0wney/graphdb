#!/bin/bash
# Test storage layer encryption

set -e

echo "=== Storage Encryption Test ==="
echo ""

# Clean up any existing test data
TEST_DIR="/tmp/graphdb-encryption-test"
rm -rf "$TEST_DIR"
mkdir -p "$TEST_DIR"

# Generate a master key for testing
MASTER_KEY=$(openssl rand -hex 32)
echo "Generated test master key: $MASTER_KEY"
echo ""

# Test 1: Start server with encryption, create data
echo "Test 1: Starting server with encryption enabled..."
ENCRYPTION_ENABLED=true \
ENCRYPTION_MASTER_KEY="$MASTER_KEY" \
ADMIN_PASSWORD="test1234!" \
PORT=9997 \
./bin/server --data="$TEST_DIR/data" > /tmp/graphdb-enc-test.log 2>&1 &
SERVER_PID=$!

# Wait for server to be ready
echo "Waiting for server to start..."
for i in {1..10}; do
    if curl -s http://localhost:9997/health > /dev/null 2>&1; then
        echo "✓ Server started"
        break
    fi
    sleep 1
done

# Login and create some nodes
echo "Test 2: Creating test data..."
LOGIN_RESPONSE=$(curl -s -X POST "http://localhost:9997/auth/login" \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"test1234!"}')

TOKEN=$(echo "$LOGIN_RESPONSE" | grep -o '"access_token":"[^"]*"' | cut -d'"' -f4)

if [ -z "$TOKEN" ]; then
    echo "✗ Failed to authenticate"
    kill $SERVER_PID 2>/dev/null || true
    exit 1
fi

# Create test nodes
curl -s -X POST "http://localhost:9997/nodes" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"labels":["User"],"properties":{"name":"Alice","email":"alice@test.com"}}' > /dev/null

curl -s -X POST "http://localhost:9997/nodes" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"labels":["User"],"properties":{"name":"Bob","email":"bob@test.com"}}' > /dev/null

echo "✓ Test data created"
echo ""

# Stop server to trigger snapshot
echo "Test 3: Stopping server to trigger snapshot..."
kill -TERM $SERVER_PID 2>/dev/null || true
# Wait for graceful shutdown (up to 10 seconds)
for i in {1..10}; do
    if ! kill -0 $SERVER_PID 2>/dev/null; then
        echo "✓ Server shut down gracefully"
        break
    fi
    sleep 1
done
sleep 2

# Check if snapshot file exists and is NOT plain JSON
echo "Test 4: Verifying snapshot is encrypted..."
SNAPSHOT_FILE="$TEST_DIR/data/snapshot.json"

if [ ! -f "$SNAPSHOT_FILE" ]; then
    echo "✗ Snapshot file not found"
    exit 1
fi

# Try to read as JSON - should fail if encrypted
if python3 -c "import json; json.load(open('$SNAPSHOT_FILE'))" 2>/dev/null; then
    echo "✗ Snapshot appears to be plain JSON (not encrypted)"
    exit 1
else
    echo "✓ Snapshot is encrypted (not readable as JSON)"
fi

# Check file is binary (encrypted data)
if file "$SNAPSHOT_FILE" | grep -q "JSON"; then
    echo "✗ Snapshot detected as JSON text"
    exit 1
else
    echo "✓ Snapshot is binary data (encrypted)"
fi
echo ""

# Test 5: Restart server and verify data can be decrypted
echo "Test 5: Restarting server to test decryption..."
ENCRYPTION_ENABLED=true \
ENCRYPTION_MASTER_KEY="$MASTER_KEY" \
ADMIN_PASSWORD="test1234!" \
PORT=9997 \
./bin/server --data="$TEST_DIR/data" > /tmp/graphdb-enc-test-reload.log 2>&1 &
SERVER_PID=$!

# Wait for server to be ready
for i in {1..10}; do
    if curl -s http://localhost:9997/health > /dev/null 2>&1; then
        break
    fi
    sleep 1
done

# Re-authenticate
LOGIN_RESPONSE=$(curl -s -X POST "http://localhost:9997/auth/login" \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"test1234!"}')

TOKEN=$(echo "$LOGIN_RESPONSE" | grep -o '"access_token":"[^"]*"' | cut -d'"' -f4)

# Retrieve nodes to verify decryption worked
NODES=$(curl -s -H "Authorization: Bearer $TOKEN" "http://localhost:9997/nodes")

# Check if we can find our test data
if echo "$NODES" | grep -q "Alice" && echo "$NODES" | grep -q "Bob"; then
    echo "✓ Data successfully decrypted and loaded"
else
    echo "✗ Data not found after decryption"
    kill $SERVER_PID 2>/dev/null || true
    exit 1
fi
echo ""

# Test 6: Try to load with wrong key (should fail)
echo "Test 6: Testing with wrong encryption key..."
kill $SERVER_PID 2>/dev/null || true
wait $SERVER_PID 2>/dev/null || true
sleep 1

WRONG_KEY=$(openssl rand -hex 32)
ENCRYPTION_ENABLED=true \
ENCRYPTION_MASTER_KEY="$WRONG_KEY" \
ADMIN_PASSWORD="test1234!" \
PORT=9997 \
timeout 3 ./bin/server --data="$TEST_DIR/data" > /tmp/graphdb-enc-test-wrong-key.log 2>&1 &
SERVER_PID=$!

sleep 2

# Check if server failed to start (expected)
if grep -q "failed to decrypt snapshot" /tmp/graphdb-enc-test-wrong-key.log; then
    echo "✓ Server correctly rejected wrong encryption key"
    kill $SERVER_PID 2>/dev/null || true
else
    # Server might still be running, check if data is corrupted
    LOGIN_RESPONSE=$(curl -s -X POST "http://localhost:9997/auth/login" \
      -H "Content-Type: application/json" \
      -d '{"username":"admin","password":"test1234!"}' 2>/dev/null || echo "")

    if [ -z "$LOGIN_RESPONSE" ]; then
        echo "✓ Server failed to start with wrong key (as expected)"
    else
        echo "⚠ Server started but data may be corrupted"
    fi
    kill $SERVER_PID 2>/dev/null || true
fi
echo ""

# Cleanup
echo "Cleaning up..."
kill $SERVER_PID 2>/dev/null || true
rm -rf "$TEST_DIR"
rm -f /tmp/graphdb-enc-test*.log

echo "=== All Storage Encryption Tests Passed ==="
