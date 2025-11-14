#!/bin/bash
#
# Run the 5M node capacity test
#
# This test validates that Cluso GraphDB can handle 5M nodes on 32GB RAM
# using disk-backed adjacency lists with LRU caching (Milestone 2).
#
# WARNING: This test takes 30-60 minutes and requires 15+ GB RAM
#
# Usage: ./scripts/run_capacity_test.sh
#

set -e

echo "======================================"
echo "5M Node Capacity Test"
echo "======================================"
echo ""
echo "This test will:"
echo "  - Create 5,000,000 nodes with ~10 edges each"
echo "  - Validate memory usage stays under 15 GB"
echo "  - Verify cache effectiveness"
echo "  - Runtime: 30-60 minutes"
echo ""
read -p "Continue? (y/n) " -n 1 -r
echo
if [[ ! $REPLY =~ ^[Yy]$ ]]
then
    echo "Cancelled"
    exit 1
fi

echo ""
echo "Starting capacity test..."
echo ""

# Enable capacity test via environment variable
export RUN_CAPACITY_TEST=1

# Run with extended timeout
go test -v \
    -run=Test5MNodeCapacity \
    -timeout=90m \
    ./pkg/storage/

echo ""
echo "======================================"
echo "Capacity Test Complete!"
echo "======================================"
