#!/bin/bash
# Test graphdb-admin CLI tool

set -e

echo "=== GraphDB Admin CLI Test ==="
echo ""

# Clean up any existing test data
TEST_DIR="/tmp/graphdb-cli-test"
rm -rf "$TEST_DIR"
mkdir -p "$TEST_DIR"

# Generate a master key for testing
MASTER_KEY=$(./bin/graphdb-admin security init --generate-key | grep "^[0-9a-f]\{64\}$")
if [ -z "$MASTER_KEY" ]; then
    echo "✗ Failed to generate master key"
    exit 1
fi
echo "✓ Generated master key: $MASTER_KEY"
echo ""

# Test 1: Start server with encryption
echo "Test 1: Starting server with encryption enabled..."
ENCRYPTION_ENABLED=true \
ENCRYPTION_MASTER_KEY="$MASTER_KEY" \
ADMIN_PASSWORD="test1234!" \
PORT=9998 \
./bin/server --data="$TEST_DIR/data" > /tmp/graphdb-cli-test.log 2>&1 &
SERVER_PID=$!

# Wait for server to be ready
echo "Waiting for server to start..."
for i in {1..10}; do
    if curl -s http://localhost:9998/health > /dev/null 2>&1; then
        echo "✓ Server started"
        break
    fi
    sleep 1
done

# Test 2: Login and get token
echo ""
echo "Test 2: Authenticating..."
LOGIN_RESPONSE=$(curl -s -X POST "http://localhost:9998/auth/login" \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"test1234!"}')

TOKEN=$(echo "$LOGIN_RESPONSE" | grep -o '"access_token":"[^"]*"' | cut -d'"' -f4)

if [ -z "$TOKEN" ]; then
    echo "✗ Failed to authenticate"
    cat /tmp/graphdb-cli-test.log
    kill $SERVER_PID 2>/dev/null || true
    exit 1
fi
echo "✓ Authentication successful"

# Test 3: Security health check
echo ""
echo "Test 3: Checking security health..."
./bin/graphdb-admin security health --server-url=http://localhost:9998 --token="$TOKEN"

if [ $? -eq 0 ]; then
    echo "✓ Security health check passed"
else
    echo "✗ Security health check failed"
    kill $SERVER_PID 2>/dev/null || true
    exit 1
fi

# Test 4: Key info
echo ""
echo "Test 4: Checking key information..."
./bin/graphdb-admin security key-info --server-url=http://localhost:9998 --token="$TOKEN"

if [ $? -eq 0 ]; then
    echo "✓ Key info retrieved"
else
    echo "✗ Key info failed"
    kill $SERVER_PID 2>/dev/null || true
    exit 1
fi

# Test 5: Create some audit events
echo ""
echo "Test 5: Creating audit events..."
curl -s -X POST "http://localhost:9998/nodes" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"labels":["Test"],"properties":{"name":"test1"}}' > /dev/null

curl -s -X POST "http://localhost:9998/nodes" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"labels":["Test"],"properties":{"name":"test2"}}' > /dev/null

echo "✓ Audit events created"

# Test 6: Audit export
echo ""
echo "Test 6: Exporting audit logs..."
./bin/graphdb-admin security audit-export \
  --server-url=http://localhost:9998 \
  --token="$TOKEN" \
  --output="$TEST_DIR/audit.json"

if [ -f "$TEST_DIR/audit.json" ]; then
    echo "✓ Audit logs exported"
    echo "  Sample:"
    head -n 5 "$TEST_DIR/audit.json"
else
    echo "✗ Audit export failed"
    kill $SERVER_PID 2>/dev/null || true
    exit 1
fi

# Test 7: Key rotation
echo ""
echo "Test 7: Rotating encryption keys..."
./bin/graphdb-admin security rotate-keys \
  --server-url=http://localhost:9998 \
  --token="$TOKEN"

if [ $? -eq 0 ]; then
    echo "✓ Key rotation successful"
else
    echo "✗ Key rotation failed"
    kill $SERVER_PID 2>/dev/null || true
    exit 1
fi

# Test 8: Verify new key version
echo ""
echo "Test 8: Verifying new key version..."
./bin/graphdb-admin security key-info --server-url=http://localhost:9998 --token="$TOKEN" | grep "Version 2"

if [ $? -eq 0 ]; then
    echo "✓ New key version confirmed"
else
    echo "✗ New key version not found"
    kill $SERVER_PID 2>/dev/null || true
    exit 1
fi

# Cleanup
echo ""
echo "Cleaning up..."
kill $SERVER_PID 2>/dev/null || true
wait $SERVER_PID 2>/dev/null || true
rm -rf "$TEST_DIR"
rm -f /tmp/graphdb-cli-test.log

echo ""
echo "=== All CLI Admin Tests Passed ==="
