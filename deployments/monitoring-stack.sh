#!/bin/bash
# Monitoring Stack Management Script

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
COMPOSE_FILE="docker-compose.monitoring.yml"

# Colors
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

log_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

show_usage() {
    cat << EOF
GraphDB Monitoring Stack Management

Usage: $0 <command>

Commands:
    start       - Start the monitoring stack
    stop        - Stop the monitoring stack
    restart     - Restart the monitoring stack
    status      - Show status of all services
    logs        - Show logs from all services
    validate    - Run validation tests
    clean       - Stop and remove all containers and volumes
    urls        - Show service URLs
    help        - Show this help message

Examples:
    $0 start              # Start all services
    $0 logs graphdb       # Show GraphDB logs
    $0 validate           # Run validation tests
EOF
}

check_prereqs() {
    if ! command -v docker &> /dev/null; then
        log_error "Docker is not installed"
        exit 1
    fi

    if ! command -v docker-compose &> /dev/null; then
        log_error "Docker Compose is not installed"
        exit 1
    fi
}

start_stack() {
    log_info "Starting monitoring stack..."

    cd "$SCRIPT_DIR"

    # Check if binary exists
    if [ ! -f "../bin/server" ]; then
        log_warn "Server binary not found. Building..."
        cd ..
        make build || {
            log_error "Build failed"
            exit 1
        }
        cd "$SCRIPT_DIR"
    fi

    docker-compose -f "$COMPOSE_FILE" up -d

    log_info "Waiting for services to be ready..."
    sleep 5

    show_urls
}

stop_stack() {
    log_info "Stopping monitoring stack..."
    cd "$SCRIPT_DIR"
    docker-compose -f "$COMPOSE_FILE" down
    log_info "Stack stopped"
}

restart_stack() {
    stop_stack
    sleep 2
    start_stack
}

show_status() {
    cd "$SCRIPT_DIR"
    docker-compose -f "$COMPOSE_FILE" ps
}

show_logs() {
    cd "$SCRIPT_DIR"
    if [ -n "$1" ]; then
        docker-compose -f "$COMPOSE_FILE" logs -f "$1"
    else
        docker-compose -f "$COMPOSE_FILE" logs -f
    fi
}

run_validation() {
    cd "$SCRIPT_DIR"

    if [ ! -f "./validate-monitoring.sh" ]; then
        log_error "Validation script not found"
        exit 1
    fi

    chmod +x ./validate-monitoring.sh
    ./validate-monitoring.sh
}

clean_stack() {
    log_warn "This will remove all containers and volumes. Data will be lost!"
    read -p "Are you sure? (yes/no): " -r
    echo

    if [[ $REPLY =~ ^[Yy][Ee][Ss]$ ]]; then
        cd "$SCRIPT_DIR"
        docker-compose -f "$COMPOSE_FILE" down -v
        log_info "Stack cleaned"
    else
        log_info "Cancelled"
    fi
}

show_urls() {
    cat << EOF

${GREEN}Service URLs:${NC}
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
GraphDB API:      http://localhost:8080
GraphDB Metrics:  http://localhost:9090/metrics
GraphDB Health:   http://localhost:9090/health

Prometheus:       http://localhost:9091
  - Targets:      http://localhost:9091/targets
  - Alerts:       http://localhost:9091/alerts
  - Graph:        http://localhost:9091/graph

Grafana:          http://localhost:3000
  - Username:     admin
  - Password:     admin

Alertmanager:     http://localhost:9093
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

EOF
}

# Main script
cd "$SCRIPT_DIR"

case "${1:-}" in
    start)
        check_prereqs
        start_stack
        ;;
    stop)
        stop_stack
        ;;
    restart)
        restart_stack
        ;;
    status)
        show_status
        ;;
    logs)
        show_logs "$2"
        ;;
    validate)
        run_validation
        ;;
    clean)
        clean_stack
        ;;
    urls)
        show_urls
        ;;
    help|--help|-h|"")
        show_usage
        ;;
    *)
        log_error "Unknown command: $1"
        echo ""
        show_usage
        exit 1
        ;;
esac
