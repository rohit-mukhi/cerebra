#!/bin/bash

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}  Stopping Multica${NC}"
echo -e "${BLUE}========================================${NC}"

cd ~/Agentic\ AI\ Programme/Project/multica

# Stop daemon
echo -e "\n${YELLOW}[1/3] Stopping daemon...${NC}"
if pgrep -f "multica daemon" > /dev/null 2>&1; then
    pkill -f "multica daemon"
    echo -e "${GREEN}✓ Daemon stopped${NC}"
else
    echo -e "${YELLOW}⚠ Daemon was not running${NC}"
fi

# Stop server
echo -e "\n${YELLOW}[2/3] Stopping server...${NC}"
if [ -f logs/server.pid ]; then
    SERVER_PID=$(cat logs/server.pid)
    if kill -0 $SERVER_PID 2>/dev/null; then
        kill $SERVER_PID
        echo -e "${GREEN}✓ Server stopped (PID: $SERVER_PID)${NC}"
    else
        echo -e "${YELLOW}⚠ Server PID $SERVER_PID not running${NC}"
    fi
    rm logs/server.pid
else
    # Try to kill by port
    if lsof -Pi :8080 -sTCP:LISTEN -t >/dev/null 2>&1; then
        lsof -ti:8080 | xargs kill -9 2>/dev/null
        echo -e "${GREEN}✓ Server stopped (by port)${NC}"
    else
        echo -e "${YELLOW}⚠ Server was not running${NC}"
    fi
fi

# PostgreSQL container (leave running by default)
echo -e "\n${YELLOW}[3/3] PostgreSQL container...${NC}"
echo -e "${BLUE}Leaving PostgreSQL running (use 'docker stop multica-postgres' to stop it)${NC}"

echo -e "\n${GREEN}========================================${NC}"
echo -e "${GREEN}  Multica stopped${NC}"
echo -e "${GREEN}========================================${NC}"
echo ""
echo -e "${BLUE}To restart:${NC} ${YELLOW}~/Agentic\\ AI\\ Programme/Project/multica/start-multica.sh${NC}"
