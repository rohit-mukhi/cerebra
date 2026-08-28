#!/bin/bash

# Multica Backend-Only Development Environment Manager
# Starts/stops PostgreSQL and backend only (no frontend)
# Use this for backend development to avoid Next.js memory issues

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$SCRIPT_DIR"
BACKEND_LOG="/tmp/multica-backend.log"
PID_FILE="/tmp/multica-backend-pid"
BACKEND_PORT="${PORT:-8080}"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Helper functions
log_info() {
    echo -e "${BLUE}ℹ${NC} $1"
}

log_success() {
    echo -e "${GREEN}✓${NC} $1"
}

log_error() {
    echo -e "${RED}✗${NC} $1"
}

log_warning() {
    echo -e "${YELLOW}⚠${NC} $1"
}

# Check if a port is in use
is_port_in_use() {
    lsof -Pi :$1 -sTCP:LISTEN -t >/dev/null 2>&1
}

# Start PostgreSQL
start_postgres() {
    log_info "Starting PostgreSQL..."
    cd "$PROJECT_ROOT"
    
    if docker ps | grep -q "multica-postgres-1"; then
        log_warning "PostgreSQL container already running"
        return 0
    fi
    
    if ! make db-up >/dev/null 2>&1; then
        log_error "Failed to start PostgreSQL"
        return 1
    fi
    
    # Wait for PostgreSQL to be ready
    log_info "Waiting for PostgreSQL to be ready..."
    for i in {1..30}; do
        if docker exec multica-postgres-1 pg_isready -U multica >/dev/null 2>&1; then
            log_success "PostgreSQL is ready"
            return 0
        fi
        sleep 1
    done
    
    log_error "PostgreSQL failed to start within 30 seconds"
    return 1
}

# Stop PostgreSQL
stop_postgres() {
    log_info "Stopping PostgreSQL..."
    cd "$PROJECT_ROOT"
    
    if ! docker ps | grep -q "multica-postgres-1"; then
        log_warning "PostgreSQL container not running"
        return 0
    fi
    
    if make db-down >/dev/null 2>&1; then
        log_success "PostgreSQL stopped"
        return 0
    else
        log_error "Failed to stop PostgreSQL"
        return 1
    fi
}

# Start backend
start_backend() {
    log_info "Starting backend on port $BACKEND_PORT..."
    
    if is_port_in_use $BACKEND_PORT; then
        log_warning "Port $BACKEND_PORT already in use"
        return 1
    fi
    
    cd "$PROJECT_ROOT/server"
    go run ./cmd/server > "$BACKEND_LOG" 2>&1 &
    local backend_pid=$!
    
    # Wait for backend to be ready
    log_info "Waiting for backend to be ready..."
    for i in {1..30}; do
        if curl -s http://localhost:$BACKEND_PORT/health >/dev/null 2>&1; then
            echo $backend_pid > "$PID_FILE"
            log_success "Backend started (PID: $backend_pid)"
            return 0
        fi
        sleep 1
    done
    
    log_error "Backend failed to start within 30 seconds"
    kill $backend_pid 2>/dev/null || true
    return 1
}

# Stop backend
stop_backend() {
    log_info "Stopping backend..."
    
    if [ -f "$PID_FILE" ]; then
        local pid=$(cat "$PID_FILE")
        if ps -p "$pid" > /dev/null 2>&1; then
            kill "$pid" 2>/dev/null || true
            log_success "Backend stopped (PID: $pid)"
        fi
        rm -f "$PID_FILE"
    fi
    
    # Fallback: kill by port
    if is_port_in_use $BACKEND_PORT; then
        lsof -ti:$BACKEND_PORT | xargs kill -9 2>/dev/null || true
        log_success "Backend stopped (via port)"
    fi
}

# Start all components
start_all() {
    echo ""
    log_info "Starting Multica backend-only environment..."
    echo ""
    
    # Clear old PID file
    rm -f "$PID_FILE"
    
    if ! start_postgres; then
        log_error "Failed to start PostgreSQL"
        return 1
    fi
    
    if ! start_backend; then
        log_error "Failed to start backend"
        stop_postgres
        return 1
    fi
    
    echo ""
    log_success "Backend environment started successfully!"
    echo ""
    echo -e "${BLUE}URLs:${NC}"
    echo "  Backend:   ${GREEN}http://localhost:$BACKEND_PORT${NC}"
    echo "  Database:  ${GREEN}localhost:5432${NC}"
    echo ""
    echo -e "${BLUE}Logs:${NC}"
    echo "  Backend:   $BACKEND_LOG"
    echo ""
    echo -e "${BLUE}To stop:${NC}"
    echo "  $0 stop"
    echo ""
    echo -e "${BLUE}To view logs:${NC}"
    echo "  $0 logs"
    echo ""
}

# Stop all components
stop_all() {
    echo ""
    log_info "Stopping Multica backend environment..."
    echo ""
    
    stop_backend
    stop_postgres
    
    echo ""
    log_success "Backend environment stopped"
    echo ""
}

# Status check
status() {
    echo ""
    echo -e "${BLUE}Multica Backend Environment Status:${NC}"
    echo ""
    
    # PostgreSQL
    if docker ps | grep -q "multica-postgres-1"; then
        log_success "PostgreSQL: Running"
    else
        log_error "PostgreSQL: Stopped"
    fi
    
    # Backend
    if is_port_in_use $BACKEND_PORT; then
        log_success "Backend: Running (port $BACKEND_PORT)"
    else
        log_error "Backend: Stopped"
    fi
    
    echo ""
}

# Show logs
show_logs() {
    log_info "Backend logs:"
    tail -f "$BACKEND_LOG"
}

# Main command handler
case "${1:-start}" in
    start)
        start_all
        ;;
    stop)
        stop_all
        ;;
    restart)
        stop_all
        sleep 2
        start_all
        ;;
    status)
        status
        ;;
    logs)
        show_logs
        ;;
    *)
        echo "Multica Backend-Only Development Environment Manager"
        echo ""
        echo "Usage: $0 {start|stop|restart|status|logs}"
        echo ""
        echo "Commands:"
        echo "  start              Start PostgreSQL and backend only"
        echo "  stop               Stop all components"
        echo "  restart            Restart all components"
        echo "  status             Show status of components"
        echo "  logs               Follow backend logs"
        echo ""
        echo "Examples:"
        echo "  $0 start           # Start backend environment"
        echo "  $0 stop            # Stop everything"
        echo "  $0 logs            # Follow backend logs"
        echo ""
        echo "Notes:"
        echo "  - This script starts ONLY the backend and database"
        echo "  - No Next.js frontend (avoids memory issues)"
        echo "  - Use for backend development and testing"
        echo "  - Access backend API at: http://localhost:8080"
        echo ""
        exit 1
        ;;
esac
