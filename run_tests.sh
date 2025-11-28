#!/bin/bash
# Test runner script for SMS Mock Server

set -e  # Exit on error

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${YELLOW}SMS Mock Server - Test Runner${NC}"
echo "================================"
echo ""

# Check if we should rebuild
if [ "$1" == "--rebuild" ] || [ "$1" == "-r" ]; then
    echo -e "${YELLOW}Rebuilding Docker container...${NC}"
    docker-compose build
    echo ""
fi

# Check if container is running
if ! docker-compose ps | grep -q "sms-mock-server.*Up"; then
    echo -e "${YELLOW}Starting Docker container...${NC}"
    docker-compose up -d
    echo "Waiting for container to be ready..."
    sleep 2
    echo ""
fi

# Install dev dependencies (if not already installed)
echo -e "${YELLOW}Installing test dependencies...${NC}"
docker-compose exec -T sms-mock-server pip install -q -r requirements-dev.txt
echo ""

# Run tests
echo -e "${GREEN}Running tests inside Docker container...${NC}"
echo ""

# Run pytest with coverage
docker-compose exec -T sms-mock-server pytest "$@"

# Check exit code
if [ $? -eq 0 ]; then
    echo ""
    echo -e "${GREEN}✓ All tests passed!${NC}"
else
    echo ""
    echo -e "${RED}✗ Some tests failed${NC}"
    exit 1
fi
