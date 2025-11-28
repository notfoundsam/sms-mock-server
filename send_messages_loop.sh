#!/bin/bash

# Configuration
ACCOUNT_SID="ACXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX"
AUTH_TOKEN="your_auth_token_here"
FROM_NUMBER="+15550000001"
TO_NUMBER="+15551234567"
CALLBACK_URL="http://localhost:9090/callback-test"
SERVER_URL="http://localhost:9090"
DELAY_SECONDS=1

# Colors for output
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Counter
count=0

echo -e "${BLUE}====================================${NC}"
echo -e "${BLUE}SMS Mock Server - Message Loop${NC}"
echo -e "${BLUE}====================================${NC}"
echo -e "Server: ${YELLOW}${SERVER_URL}${NC}"
echo -e "From: ${YELLOW}${FROM_NUMBER}${NC}"
echo -e "To: ${YELLOW}${TO_NUMBER}${NC}"
echo -e "Callback: ${YELLOW}${CALLBACK_URL}${NC}"
echo -e "Delay: ${YELLOW}${DELAY_SECONDS}s${NC}"
echo -e "${BLUE}====================================${NC}"
echo -e "Press ${YELLOW}Ctrl+C${NC} to stop"
echo ""

# Trap Ctrl+C to show summary
trap 'echo -e "\n${BLUE}====================================${NC}"; echo -e "Stopped. Total messages sent: ${GREEN}${count}${NC}"; exit 0' INT

# Infinite loop
while true; do
    count=$((count + 1))

    # Generate unique message body
    timestamp=$(date +%s)
    body="Test message #${count} at ${timestamp}"

    echo -e "${BLUE}[${count}]${NC} Sending message: ${body}"

    # Send message with curl
    response=$(curl -s -w "\n%{http_code}" -X POST \
        "${SERVER_URL}/2010-04-01/Accounts/${ACCOUNT_SID}/Messages.json" \
        -u "${ACCOUNT_SID}:${AUTH_TOKEN}" \
        -d "From=${FROM_NUMBER}" \
        -d "To=${TO_NUMBER}" \
        -d "Body=${body}" \
        -d "StatusCallback=${CALLBACK_URL}")

    # Extract HTTP status code (last line)
    http_code=$(echo "$response" | tail -n 1)
    response_body=$(echo "$response" | head -n -1)

    # Check response
    if [ "$http_code" -eq 201 ]; then
        # Extract message SID from response
        message_sid=$(echo "$response_body" | grep -o '"sid":"[^"]*"' | head -1 | cut -d'"' -f4)
        echo -e "${GREEN}✓${NC} Success! SID: ${message_sid} (HTTP ${http_code})"
    else
        echo -e "${YELLOW}✗${NC} Failed with HTTP ${http_code}"
        echo "$response_body" | head -c 200
        echo ""
    fi

    # Wait before next iteration
    sleep $DELAY_SECONDS
done
