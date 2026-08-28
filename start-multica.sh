#!/bin/bash
set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}  Multica Startup Script${NC}"
echo -e "${BLUE}========================================${NC}"

# Change to project directory
cd ~/Agentic\ AI\ Programme/Project/multica

# Export environment variables
export DATABASE_URL=postgresql://multica:multica@localhost:5432/multica?sslmode=disable

# Step 1: Check if PostgreSQL container is running
echo -e "\n${YELLOW}[1/5] Checking PostgreSQL container...${NC}"
if docker ps | grep -q multica-postgres; then
    echo -e "${GREEN}✓ PostgreSQL container is already running${NC}"
else
    echo -e "${YELLOW}Starting PostgreSQL container...${NC}"
    if docker ps -a | grep -q multica-postgres; then
        docker start multica-postgres
    else
        docker run -d \
          --name multica-postgres \
          -e POSTGRES_USER=multica \
          -e POSTGRES_PASSWORD=multica \
          -e POSTGRES_DB=multica \
          -p 5432:5432 \
          pgvector/pgvector:pg17
    fi
    echo -e "${GREEN}✓ PostgreSQL container started${NC}"
    sleep 5
fi

# Step 2: Check if server is running
echo -e "\n${YELLOW}[2/5] Checking Multica server...${NC}"
if lsof -Pi :8080 -sTCP:LISTEN -t >/dev/null 2>&1; then
    echo -e "${GREEN}✓ Server is already running on port 8080${NC}"
else
    echo -e "${YELLOW}Starting Multica server...${NC}"
    cd server
    nohup ./bin/server > ../logs/server.log 2>&1 &
    SERVER_PID=$!
    echo $SERVER_PID > ../logs/server.pid
    cd ..
    sleep 3
    
    if lsof -Pi :8080 -sTCP:LISTEN -t >/dev/null 2>&1; then
        echo -e "${GREEN}✓ Server started (PID: $SERVER_PID)${NC}"
    else
        echo -e "${RED}✗ Server failed to start. Check logs/server.log${NC}"
        exit 1
    fi
fi

# Step 3: Check if openclaw wrapper exists
echo -e "\n${YELLOW}[3/5] Checking OpenClaw CLI wrapper...${NC}"
if command -v openclaw >/dev/null 2>&1; then
    echo -e "${GREEN}✓ OpenClaw CLI wrapper exists${NC}"
else
    echo -e "${YELLOW}Creating OpenClaw CLI wrapper...${NC}"
    sudo tee /usr/local/bin/openclaw > /dev/null << 'EOF'
#!/bin/bash
docker exec -i openclaw-openclaw-cli-1 openclaw "$@"
EOF
    sudo chmod +x /usr/local/bin/openclaw
    echo -e "${GREEN}✓ OpenClaw CLI wrapper created${NC}"
fi

# Step 4: Check if daemon is running
echo -e "\n${YELLOW}[4/5] Checking Multica daemon...${NC}"
if pgrep -f "multica daemon" > /dev/null 2>&1; then
    echo -e "${GREEN}✓ Daemon is already running${NC}"
else
    echo -e "${YELLOW}Starting Multica daemon...${NC}"
    cd server
    nohup ./bin/multica daemon start --profile desktop-api.multica.ai > ../logs/daemon-startup.log 2>&1 &
    cd ..
    sleep 3
    
    if pgrep -f "multica daemon" > /dev/null 2>&1; then
        echo -e "${GREEN}✓ Daemon started${NC}"
    else
        echo -e "${RED}✗ Daemon failed to start. Check logs/daemon-startup.log or ~/.multica/profiles/desktop-api.multica.ai/daemon.log${NC}"
        exit 1
    fi
fi

# Step 5: Get runtime ID and configure Cerebra
echo -e "\n${YELLOW}[5/5] Configuring Cerebra...${NC}"
sleep 2  # Wait for daemon to register

# Get runtime ID from daemon logs
RUNTIME_ID=$(grep "registered runtime" ~/.multica/profiles/desktop-api.multica.ai/daemon.log 2>/dev/null | tail -1 | grep -oP 'runtime_id=\K[a-f0-9-]+' || echo "")

if [ -z "$RUNTIME_ID" ]; then
    echo -e "${YELLOW}⚠ Runtime ID not found yet. Daemon may still be starting...${NC}"
    echo -e "${YELLOW}Run this script again in 10 seconds, or configure manually:${NC}"
    echo -e "${BLUE}  docker exec multica-postgres psql -U multica -d multica -c \"UPDATE agent_runtime SET tier_model_map = '{\\\"simple\\\": \\\"gpt-4o-mini\\\", \\\"standard\\\": \\\"gpt-4o\\\", \\\"heavy\\\": \\\"claude-opus-4-5\\\"}'::jsonb WHERE id::text LIKE '$(grep 'runtime_id=' ~/.multica/profiles/desktop-api.multica.ai/daemon.log 2>/dev/null | tail -1 | cut -d= -f2 | cut -c1-8)%';\"${NC}"
else
    echo -e "${BLUE}Runtime ID: ${RUNTIME_ID}${NC}"
    
    # Configure Cerebra tier model map
    docker exec multica-postgres psql -U multica -d multica -c \
        "UPDATE agent_runtime 
         SET tier_model_map = '{\"simple\": \"gpt-4o-mini\", \"standard\": \"gpt-4o\", \"heavy\": \"claude-opus-4-5\"}'::jsonb 
         WHERE id = '${RUNTIME_ID}'::uuid;" >/dev/null 2>&1
    
    # Verify
    TIER_MAP=$(docker exec multica-postgres psql -U multica -d multica -t -c \
        "SELECT tier_model_map FROM agent_runtime WHERE id = '${RUNTIME_ID}'::uuid;" 2>/dev/null | tr -d '[:space:]')
    
    if [ -n "$TIER_MAP" ] && [ "$TIER_MAP" != "" ]; then
        echo -e "${GREEN}✓ Cerebra tier model map configured${NC}"
    else
        echo -e "${YELLOW}⚠ Cerebra configuration may not have applied. Verify manually.${NC}"
    fi
fi

# Summary
echo -e "\n${GREEN}========================================${NC}"
echo -e "${GREEN}  Multica is ready!${NC}"
echo -e "${GREEN}========================================${NC}"
echo ""
echo -e "${BLUE}Services running:${NC}"
echo -e "  • PostgreSQL:  ${GREEN}running${NC} (port 5432)"
echo -e "  • Server:      ${GREEN}running${NC} (http://localhost:8080)"
echo -e "  • Daemon:      ${GREEN}running${NC}"
if [ -n "$RUNTIME_ID" ]; then
    echo -e "  • Runtime ID:  ${BLUE}${RUNTIME_ID}${NC}"
fi
echo ""
echo -e "${BLUE}Logs:${NC}"
echo -e "  • Server:      logs/server.log"
echo -e "  • Daemon:      ~/.multica/profiles/desktop-api.multica.ai/daemon.log"
echo ""
echo -e "${BLUE}Quick commands:${NC}"
echo -e "  • List agents:     ${YELLOW}cd server && ./bin/multica agent list${NC}"
echo -e "  • Create issue:    ${YELLOW}cd server && ./bin/multica issue create --title \"Test\" --description \"Testing Cerebra\"${NC}"
echo -e "  • Watch daemon:    ${YELLOW}tail -f ~/.multica/profiles/desktop-api.multica.ai/daemon.log | grep cerebra${NC}"
echo -e "  • Stop all:        ${YELLOW}~/Agentic\\ AI\\ Programme/Project/multica/stop-multica.sh${NC}"
echo ""
echo -e "${GREEN}Ready to test Cerebra! 🚀${NC}"
