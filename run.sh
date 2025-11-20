#!/bin/bash

# run.sh - Build and run the web server
# Always runs from project root to ensure correct file paths

set -e  # Exit on error

# Colors for output
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

echo -e "${GREEN}=== Web Server Build & Run Script ===${NC}"
echo ""

# Ensure we're in project root (where run.sh is located)
cd "$(dirname "$0")"
PROJECT_ROOT=$(pwd)

echo -e "${YELLOW}Project root:${NC} $PROJECT_ROOT"
echo ""

# Check if public/static exists
if [ ! -d "public/static" ]; then
    echo -e "${YELLOW}Creating public/static directory...${NC}"
    mkdir -p public/static
fi

# Check if there are any files in public/static
echo -e "${YELLOW}Files in public/static:${NC}"
if [ -z "$(ls -A public/static 2>/dev/null)" ]; then
    echo -e "${RED}  (empty)${NC}"
    echo ""
    echo -e "${YELLOW}Tip:${NC} Add your static files (HTML, CSS, JS, videos) to public/static/"
else
    ls -lh public/static/ | tail -n +2 | awk '{printf "  - %-30s %10s\n", $9, $5}'
fi
echo ""

# Build the server
echo -e "${GREEN}Building server...${NC}"
go build -o bin/server cmd/main.go

if [ $? -eq 0 ]; then
    echo -e "${GREEN}✓ Build successful${NC}"
else
    echo -e "${RED}✗ Build failed${NC}"
    exit 1
fi
echo ""

# Run the server
echo -e "${GREEN}Starting server...${NC}"
echo -e "${YELLOW}Press Ctrl+C to stop${NC}"
echo ""
echo -e "${GREEN}Available Endpoints:${NC}"
echo ""
echo -e "${YELLOW}Web Pages:${NC}"
echo "  GET  http://localhost:8080/                     - Homepage (beautiful UI)"
echo "  GET  http://localhost:8080/static/*             - Static files (HTML, CSS, JS, images, videos)"
echo ""
echo -e "${YELLOW}API Endpoints:${NC}"
echo "  GET  http://localhost:8080/hello                - Simple hello message"
echo "  GET  http://localhost:8080/version              - Server version info (JSON)"
echo "  GET  http://localhost:8080/api/users            - Get users list (JSON)"
echo "  POST http://localhost:8080/echo                 - Echo request body (JSON)"
echo ""
echo -e "${GREEN}Test Commands:${NC}"
echo ""
echo -e "${YELLOW}# Test homepage${NC}"
echo "  curl http://localhost:8080/"
echo ""
echo -e "${YELLOW}# Test API endpoints${NC}"
echo "  curl http://localhost:8080/hello"
echo "  curl http://localhost:8080/version"
echo "  curl http://localhost:8080/api/users"
echo "  curl -X POST http://localhost:8080/echo -d 'Hello Server'"
echo ""
echo -e "${YELLOW}# Test static files${NC}"
echo "  curl http://localhost:8080/static/index.html"
echo "  curl -I http://localhost:8080/static/css/style.css"
echo ""
echo -e "${YELLOW}# Test gzip compression${NC}"
echo "  curl -H 'Accept-Encoding: gzip' -I http://localhost:8080/api/users"
echo "  curl -H 'Accept-Encoding: gzip' -I http://localhost:8080/"
echo ""
echo -e "${YELLOW}# Test caching (304 Not Modified)${NC}"
echo "  curl -I http://localhost:8080/static/index.html"
echo "  curl -I http://localhost:8080/static/index.html -H 'If-Modified-Since: Wed, 01 Jan 2025 00:00:00 GMT'"
echo ""
echo -e "${YELLOW}# Test video streaming (Range requests)${NC}"
echo "  curl -I http://localhost:8080/static/videos/video.mp4"
echo "  curl -I http://localhost:8080/static/videos/video.mp4 -H 'Range: bytes=0-1048575'"
echo ""

./bin/server
