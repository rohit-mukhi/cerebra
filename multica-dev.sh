#!/bin/bash

# Multica Development Environment Manager
# Starts/stops PostgreSQL, backend, and frontend with a single command

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$SCRIPT_DIR"
BACKEND_LOG="/tmp/multica-backend.log"
FRONTEND_LOG="/tmp/multica-frontend.log"
PID_FILE="/tmp/multica-pids"
BACKEND_PORT="${PORT:-8080}"
FRONTEND_PORT="${FRONTEND_PORT:-3000}"

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
            echo $backend_pid >> "$PID_FILE"
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
        while IFS= read -r pid; do
            if ps -p "$pid" > /dev/null 2>&1; then
                kill "$pid" 2>/dev/null || true
                log_success "Backend stopped (PID: $pid)"
            fi
        done < "$PID_FILE"
        rm -f "$PID_FILE"
    fi
    
    # Fallback: kill by port
    if is_port_in_use $BACKEND_PORT; then
        lsof -ti:$BACKEND_PORT | xargs kill -9 2>/dev/null || true
        log_success "Backend stopped (via port)"
    fi
}

# Start frontend
start_frontend() {
    log_info "Starting frontend on port $FRONTEND_PORT..."
    log_warning "Note: Next.js dev server can use significant memory. Monitor with: free -h"
    
    if is_port_in_use $FRONTEND_PORT; then
        log_warning "Port $FRONTEND_PORT already in use"
        return 1
    fi
    
    cd "$PROJECT_ROOT"
    # Limit Node.js memory to prevent system freeze
    NODE_OPTIONS="--max-old-space-size=2048" pnpm dev:web > "$FRONTEND_LOG" 2>&1 &
    local frontend_pid=$!
    
    # Wait for frontend to be ready
    log_info "Waiting for frontend to be ready..."
    for i in {1..60}; do
        if curl -s http://localhost:$FRONTEND_PORT >/dev/null 2>&1; then
            echo $frontend_pid >> "$PID_FILE"
            log_success "Frontend started (PID: $frontend_pid)"
            return 0
        fi
        sleep 1
    done
    
    log_error "Frontend failed to start within 60 seconds"
    kill $frontend_pid 2>/dev/null || true
    return 1
}

# Stop frontend
stop_frontend() {
    log_info "Stopping frontend..."
    
    if [ -f "$PID_FILE" ]; then
        while IFS= read -r pid; do
            if ps -p "$pid" > /dev/null 2>&1; then
                kill "$pid" 2>/dev/null || true
                log_success "Frontend stopped (PID: $pid)"
            fi
        done < "$PID_FILE"
        rm -f "$PID_FILE"
    fi
    
    # Fallback: kill by port
    if is_port_in_use $FRONTEND_PORT; then
        lsof -ti:$FRONTEND_PORT | xargs kill -9 2>/dev/null || true
        log_success "Frontend stopped (via port)"
    fi
}

# Start all components
start_all() {
    echo ""
    log_info "Starting Multica development environment..."
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
    
    if ! start_frontend; then
        log_error "Failed to start frontend"
        stop_backend
        stop_postgres
        return 1
    fi
    
    echo ""
    log_success "All components started successfully!"
    echo ""
    echo -e "${BLUE}URLs:${NC}"
    echo "  Frontend:  ${GREEN}http://localhost:$FRONTEND_PORT${NC}"
    echo "  Backend:   ${GREEN}http://localhost:$BACKEND_PORT${NC}"
    echo "  Database:  ${GREEN}localhost:5432${NC}"
    echo ""
    echo -e "${BLUE}Logs:${NC}"
    echo "  Backend:   $BACKEND_LOG"
    echo "  Frontend:  $FRONTEND_LOG"
    echo ""
    echo -e "${BLUE}To stop all components:${NC}"
    echo "  $0 stop"
    echo ""
}

# Stop all components
stop_all() {
    echo ""
    log_info "Stopping Multica development environment..."
    echo ""
    
    stop_frontend
    stop_backend
    stop_postgres
    
    echo ""
    log_success "All components stopped"
    echo ""
}

# Status check
status() {
    echo ""
    echo -e "${BLUE}Multica Development Environment Status:${NC}"
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
    
    # Frontend
    if is_port_in_use $FRONTEND_PORT; then
        log_success "Frontend: Running (port $FRONTEND_PORT)"
    else
        log_error "Frontend: Stopped"
    fi
    
    echo ""
}

# Show logs
show_logs() {
    case "$2" in
        backend)
            log_info "Backend logs:"
            tail -f "$BACKEND_LOG"
            ;;
        frontend)
            log_info "Frontend logs:"
            tail -f "$FRONTEND_LOG"
            ;;
        *)
            log_info "Backend logs (last 50 lines):"
            tail -50 "$BACKEND_LOG"
            echo ""
            log_info "Frontend logs (last 50 lines):"
            tail -50 "$FRONTEND_LOG"
            ;;
    esac
}

# Show memory usage
show_memory() {
    echo ""
    log_info "System Memory Usage:"
    free -h
    echo ""
    log_info "Process Memory Usage:"
    ps aux | grep -E "node|go run" | grep -v grep | awk '{print $2, $6, $11}' | column -t
    echo ""
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
        show_logs "$@"
        ;;
    memory)
        show_memory
        ;;
    *)
        echo "Multica Development Environment Manager"
        echo ""
        echo "Usage: $0 {start|stop|restart|status|logs|memory}"
        echo ""
        echo "Commands:"
        echo "  start              Start all components (PostgreSQL, backend, frontend)"
        echo "  stop               Stop all components"
        echo "  restart            Restart all components"
        echo "  status             Show status of all components"
        echo "  logs [backend|frontend]  Show logs (default: last 50 lines of both)"
        echo "  memory             Show system and process memory usage"
        echo ""
        echo "Examples:"
        echo "  $0 start           # Start everything"
        echo "  $0 stop            # Stop everything"
        echo "  $0 logs backend    # Follow backend logs"
        echo "  $0 memory          # Check memory usage"
        echo ""
        echo "Memory Issues:"
        echo "  If Next.js consumes too much memory, the script limits it to 2GB."
        echo "  Monitor with: $0 memory"
        echo "  Or manually: free -h && ps aux | grep node"
        echo ""
        exit 1
        ;;
esac
