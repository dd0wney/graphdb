#!/bin/bash
# ZeroMQ Replication Test Script

echo "🔥 Testing ZeroMQ Replication"
echo "=============================="
echo ""

# Clean up old data
rm -rf ./data/zmq-primary ./data/zmq-replica1 ./data/zmq-replica2

echo "📊 Architecture:"
echo "  Primary: PUB/SUB on port 9090, ROUTER on 9091, PULL on 9092"
echo "  Replica 1: SUB to 9090, DEALER to 9091, PUSH to 9092"
echo "  Replica 2: SUB to 9090, DEALER to 9091, PUSH to 9092"
echo ""

echo "Starting Primary node..."
./bin/graphdb-zmq-primary --data ./data/zmq-primary --http 8080 > /tmp/primary.log 2>&1 &
PRIMARY_PID=$!
sleep 2

echo "✅ Primary started (PID: $PRIMARY_PID)"
echo ""

echo "Starting Replica 1..."
./bin/graphdb-zmq-replica --data ./data/zmq-replica1 --http 8081 --primary localhost:9090 > /tmp/replica1.log 2>&1 &
REPLICA1_PID=$!
sleep 1

echo "✅ Replica 1 started (PID: $REPLICA1_PID)"
echo ""

echo "Starting Replica 2..."
./bin/graphdb-zmq-replica --data ./data/zmq-replica2 --http 8082 --primary localhost:9090 > /tmp/replica2.log 2>&1 &
REPLICA2_PID=$!
sleep 1

echo "✅ Replica 2 started (PID: $REPLICA2_PID)"
echo ""

echo "📊 Checking health..."
sleep 1

echo ""
echo "Primary health:"
curl -s http://localhost:8080/health | jq .

echo ""
echo "Replica 1 health:"
curl -s http://localhost:8081/health | jq .

echo ""
echo "Replica 2 health:"
curl -s http://localhost:8082/health | jq .

echo ""
echo "📝 Creating test nodes on primary..."
curl -s -X POST http://localhost:8080/nodes \
  -H "Content-Type: application/json" \
  -d '{"labels": ["User"], "properties": {"id": {"Type": 0, "Data": "YWxpY2U="}, "email": {"Type": 0, "Data": "YWxpY2VAZXhhbXBsZS5jb20="}}}' | jq .

echo ""
echo "⏱️  Waiting for replication..."
sleep 2

echo ""
echo "📊 Statistics:"
echo "Primary:"
curl -s http://localhost:8080/stats | jq .
echo ""
echo "Replica 1:"
curl -s http://localhost:8081/stats | jq .
echo ""
echo "Replica 2:"
curl -s http://localhost:8082/stats | jq .

echo ""
echo "🛑 Stopping all nodes..."
kill $PRIMARY_PID $REPLICA1_PID $REPLICA2_PID

echo ""
echo "✅ Test complete!"
echo ""
echo "📁 Logs:"
echo "  Primary: /tmp/primary.log"
echo "  Replica 1: /tmp/replica1.log"
echo "  Replica 2: /tmp/replica2.log"
