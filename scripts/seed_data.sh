#!/bin/bash

# Seed script for SMS Mock Server
# Creates sample messages and calls with various scenarios

# Configuration
ACCOUNT_SID="ACXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX"
AUTH_TOKEN="your_auth_token_here"
SERVER_URL="http://localhost:8080"
CALLBACK_URL="http://localhost:8080/callback-test"

# From numbers (must be in allowed_from_numbers in config.yaml)
FROM_NUMBERS=(
    "+15550000001"
    "+15550000002"
)

# To numbers - different scenarios
TO_REGISTERED="+15551234567"      # In registered_numbers - will succeed
TO_REGISTERED_2="+15559876543"    # In registered_numbers - will succeed
TO_FAILURE="+15559999999"         # In failure_numbers - will fail
TO_UNKNOWN="+15553334444"         # Not in any list - uses default_behavior

# TwiML URLs for calls
TWIML_URLS=(
    "http://example.com/welcome.xml"
    "http://example.com/menu.xml"
    "http://example.com/voicemail.xml"
)

# Colors for output
GREEN='\033[0;32m'
RED='\033[0;31m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Counters
msg_success=0
msg_failed=0
call_success=0
call_failed=0

echo -e "${BLUE}======================================${NC}"
echo -e "${BLUE}SMS Mock Server - Data Seeder${NC}"
echo -e "${BLUE}======================================${NC}"
echo ""

# Function to send SMS
send_sms() {
    local from=$1
    local to=$2
    local body=$3
    local with_callback=$4

    local callback_param=""
    if [ "$with_callback" = "true" ]; then
        callback_param="--data-urlencode StatusCallback=${CALLBACK_URL}"
    fi

    response=$(curl -s -X POST \
        "${SERVER_URL}/2010-04-01/Accounts/${ACCOUNT_SID}/Messages.json" \
        -u "${ACCOUNT_SID}:${AUTH_TOKEN}" \
        --data-urlencode "From=${from}" \
        --data-urlencode "To=${to}" \
        --data-urlencode "Body=${body}" \
        $callback_param)

    if echo "$response" | grep -q '"sid"'; then
        message_sid=$(echo "$response" | grep -o '"sid":"[^"]*"' | head -1 | cut -d'"' -f4)
        echo -e "  ${GREEN}✓${NC} SMS: ${from} -> ${to} (${message_sid})"
        ((msg_success++))
    else
        echo -e "  ${RED}✗${NC} SMS: ${from} -> ${to} - Failed"
        ((msg_failed++))
    fi
}

# Function to make call
make_call() {
    local from=$1
    local to=$2
    local twiml_url=$3
    local with_callback=$4

    local callback_param=""
    if [ "$with_callback" = "true" ]; then
        callback_param="--data-urlencode StatusCallback=${CALLBACK_URL}"
    fi

    response=$(curl -s -X POST \
        "${SERVER_URL}/2010-04-01/Accounts/${ACCOUNT_SID}/Calls.json" \
        -u "${ACCOUNT_SID}:${AUTH_TOKEN}" \
        --data-urlencode "From=${from}" \
        --data-urlencode "To=${to}" \
        --data-urlencode "Url=${twiml_url}" \
        $callback_param)

    if echo "$response" | grep -q '"sid"'; then
        call_sid=$(echo "$response" | grep -o '"sid":"[^"]*"' | head -1 | cut -d'"' -f4)
        echo -e "  ${GREEN}✓${NC} Call: ${from} -> ${to} (${call_sid})"
        ((call_success++))
    else
        echo -e "  ${RED}✗${NC} Call: ${from} -> ${to} - Failed"
        ((call_failed++))
    fi
}

# Check if server is running
echo -e "${YELLOW}Checking server connectivity...${NC}"
if ! curl -s "${SERVER_URL}/health" > /dev/null 2>&1; then
    echo -e "${RED}Error: Server is not running at ${SERVER_URL}${NC}"
    echo "Please run 'make up' first"
    exit 1
fi
echo -e "${GREEN}Server is running${NC}"
echo ""

# ============================================
# SEED MESSAGES
# ============================================
echo -e "${BLUE}--- Seeding Messages ---${NC}"

# 1. Successful messages (to registered numbers)
echo -e "${YELLOW}Creating successful messages...${NC}"
send_sms "${FROM_NUMBERS[0]}" "$TO_REGISTERED" "Welcome to our service! Your account is now active." "true"
send_sms "${FROM_NUMBERS[0]}" "$TO_REGISTERED" "Your verification code is: 123456" "true"
send_sms "${FROM_NUMBERS[1]}" "$TO_REGISTERED_2" "Your order #1001 has been shipped!" "true"
send_sms "${FROM_NUMBERS[0]}" "$TO_REGISTERED" "Reminder: Your appointment is tomorrow at 2pm" "false"
send_sms "${FROM_NUMBERS[1]}" "$TO_REGISTERED" "Flash sale! 50% off all items today only." "true"

# 2. Failed messages (to failure numbers)
echo -e "${YELLOW}Creating failed messages...${NC}"
send_sms "${FROM_NUMBERS[0]}" "$TO_FAILURE" "This message will fail delivery" "true"
send_sms "${FROM_NUMBERS[1]}" "$TO_FAILURE" "Another failed message attempt" "true"

# 3. Messages to unknown numbers (uses default_behavior)
echo -e "${YELLOW}Creating messages to unknown numbers...${NC}"
send_sms "${FROM_NUMBERS[0]}" "$TO_UNKNOWN" "Message to unknown number" "true"
send_sms "${FROM_NUMBERS[1]}" "+15556667777" "Testing unknown recipient" "false"

# 4. Unicode/emoji messages
echo -e "${YELLOW}Creating unicode messages...${NC}"
send_sms "${FROM_NUMBERS[0]}" "$TO_REGISTERED" "Hello! 你好! مرحبا! 🎉" "true"
send_sms "${FROM_NUMBERS[1]}" "$TO_REGISTERED_2" "Payment received ✅ Amount: €50.00" "true"

# 5. Long messages (multi-segment)
echo -e "${YELLOW}Creating long messages...${NC}"
send_sms "${FROM_NUMBERS[0]}" "$TO_REGISTERED" "This is a long message that will span multiple SMS segments. Lorem ipsum dolor sit amet, consectetur adipiscing elit. Sed do eiusmod tempor incididunt ut labore et dolore magna aliqua. Ut enim ad minim veniam, quis nostrud exercitation ullamco laboris." "true"

echo ""

# ============================================
# SEED CALLS
# ============================================
echo -e "${BLUE}--- Seeding Calls ---${NC}"

# 1. Successful calls (to registered numbers)
echo -e "${YELLOW}Creating successful calls...${NC}"
make_call "${FROM_NUMBERS[0]}" "$TO_REGISTERED" "${TWIML_URLS[0]}" "true"
make_call "${FROM_NUMBERS[1]}" "$TO_REGISTERED_2" "${TWIML_URLS[1]}" "true"
make_call "${FROM_NUMBERS[0]}" "$TO_REGISTERED" "${TWIML_URLS[2]}" "false"

# 2. Failed calls (to failure numbers)
echo -e "${YELLOW}Creating failed calls...${NC}"
make_call "${FROM_NUMBERS[0]}" "$TO_FAILURE" "${TWIML_URLS[0]}" "true"
make_call "${FROM_NUMBERS[1]}" "$TO_FAILURE" "${TWIML_URLS[1]}" "true"

# 3. Calls to unknown numbers
echo -e "${YELLOW}Creating calls to unknown numbers...${NC}"
make_call "${FROM_NUMBERS[0]}" "$TO_UNKNOWN" "${TWIML_URLS[0]}" "true"
make_call "${FROM_NUMBERS[1]}" "+15558889999" "${TWIML_URLS[2]}" "true"

echo ""

# ============================================
# SUMMARY
# ============================================
echo -e "${BLUE}======================================${NC}"
echo -e "${BLUE}Seeding Complete!${NC}"
echo -e "${BLUE}======================================${NC}"
echo ""
echo -e "Messages: ${GREEN}${msg_success} created${NC}, ${RED}${msg_failed} failed${NC}"
echo -e "Calls:    ${GREEN}${call_success} created${NC}, ${RED}${call_failed} failed${NC}"
echo ""
echo -e "View results at: ${YELLOW}${SERVER_URL}${NC}"
echo ""
