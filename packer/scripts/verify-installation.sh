#!/bin/bash
set -euo pipefail

#######################################################################
# Installation Verification Script
#
# Verifies that GraphDB and all dependencies are correctly installed
# before creating the marketplace snapshot.
#
#######################################################################

echo "========================================="
echo "Installation Verification"
echo "========================================="

ERRORS=0

check_command() {
    local cmd=$1
    local desc=$2

    if command -v "$cmd" &> /dev/null; then
        echo "✓ $desc installed"
    else
        echo "✗ $desc NOT installed"
        ERRORS=$((ERRORS + 1))
    fi
}

check_file() {
    local file=$1
    local desc=$2

    if [ -f "$file" ]; then
        echo "✓ $desc exists"
    else
        echo "✗ $desc NOT found"
        ERRORS=$((ERRORS + 1))
    fi
}

check_directory() {
    local dir=$1
    local desc=$2

    if [ -d "$dir" ]; then
        echo "✓ $desc exists"
    else
        echo "✗ $desc NOT found"
        ERRORS=$((ERRORS + 1))
    fi
}

check_service() {
    local service=$1
    local desc=$2

    if systemctl is-enabled "$service" &>/dev/null; then
        echo "✓ $desc enabled"
    else
        echo "✗ $desc NOT enabled"
        ERRORS=$((ERRORS + 1))
    fi
}

#######################################################################
# Check Commands
#######################################################################

echo ""
echo "[1/6] Verifying commands..."
check_command "docker" "Docker"
check_command "jq" "jq"
check_command "curl" "curl"
check_command "s3cmd" "s3cmd"

#######################################################################
# Check Docker
#######################################################################

echo ""
echo "[2/6] Verifying Docker installation..."

if docker version &>/dev/null; then
    echo "✓ Docker is working"
    docker version | grep "Version:" | head -2
else
    echo "✗ Docker is NOT working"
    ERRORS=$((ERRORS + 1))
fi

if docker compose version &>/dev/null; then
    echo "✓ Docker Compose plugin is working"
    docker compose version
else
    echo "✗ Docker Compose plugin is NOT working"
    ERRORS=$((ERRORS + 1))
fi

#######################################################################
# Check Files
#######################################################################

echo ""
echo "[3/6] Verifying required files..."
check_file "/var/lib/graphdb/docker-compose.yml" "Docker Compose config"
check_file "/usr/local/bin/backup-graphdb.sh" "Backup script"
check_file "/usr/local/bin/restore-graphdb.sh" "Restore script"
check_file "/usr/local/bin/test-dr.sh" "DR test script"
check_file "/usr/local/bin/graphdb-first-boot.sh" "First-boot script"
check_file "/etc/systemd/system/graphdb-first-boot.service" "First-boot service"

#######################################################################
# Check Directories
#######################################################################

echo ""
echo "[4/6] Verifying required directories..."
check_directory "/var/lib/graphdb" "GraphDB directory"
check_directory "/var/lib/graphdb/data" "GraphDB data directory"
check_directory "/var/lib/graphdb/backups" "GraphDB backups directory"
check_directory "/var/lib/graphdb/logs" "GraphDB logs directory"
check_directory "/etc/graphdb" "GraphDB config directory"
check_directory "/root/graphdb-docs" "GraphDB documentation"

#######################################################################
# Check Services
#######################################################################

echo ""
echo "[5/6] Verifying services..."
check_service "docker" "Docker service"
check_service "graphdb" "GraphDB service"
check_service "graphdb-first-boot" "GraphDB first-boot service"
check_service "fail2ban" "fail2ban service"
check_service "node_exporter" "node_exporter service"

#######################################################################
# Check Firewall
#######################################################################

echo ""
echo "[6/6] Verifying firewall..."

if ufw status | grep -q "Status: active"; then
    echo "✓ UFW firewall is active"

    # Check for required rules
    if ufw status | grep -q "8080"; then
        echo "✓ Port 8080 (GraphDB) is allowed"
    else
        echo "✗ Port 8080 NOT allowed in firewall"
        ERRORS=$((ERRORS + 1))
    fi

    if ufw status | grep -q "22"; then
        echo "✓ Port 22 (SSH) is allowed"
    else
        echo "⚠ Port 22 may not be explicitly allowed (could be default)"
    fi
else
    echo "⚠ UFW firewall is not active (will be enabled on first boot)"
fi

#######################################################################
# Check Docker Images
#######################################################################

echo ""
echo "[Additional] Checking Docker images..."

if docker images | grep -q "graphdb"; then
    echo "✓ GraphDB image is present"
    docker images | grep "graphdb"
else
    echo "⚠ GraphDB image not present (will be pulled on first boot)"
fi

#######################################################################
# Check Scripts are Executable
#######################################################################

echo ""
echo "[Additional] Checking script permissions..."

for script in \
    /usr/local/bin/backup-graphdb.sh \
    /usr/local/bin/restore-graphdb.sh \
    /usr/local/bin/test-dr.sh \
    /usr/local/bin/graphdb-first-boot.sh \
    /root/graphdb-docs/*.md 2>/dev/null || true
do
    if [ -x "$script" ]; then
        echo "✓ $(basename $script) is executable"
    elif [ -f "$script" ]; then
        echo "✗ $(basename $script) is NOT executable"
        ERRORS=$((ERRORS + 1))
    fi
done

#######################################################################
# Summary
#######################################################################

echo ""
echo "========================================="
if [ $ERRORS -eq 0 ]; then
    echo "✓ Installation Verification PASSED"
    echo "========================================="
    echo ""
    echo "All checks passed! Image is ready for marketplace."
    exit 0
else
    echo "✗ Installation Verification FAILED"
    echo "========================================="
    echo ""
    echo "Found $ERRORS errors. Please fix before creating snapshot."
    exit 1
fi
