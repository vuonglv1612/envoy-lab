#!/bin/bash

# Test script for rate limiting system
set -e

ENVOY_URL="${ENVOY_URL:-http://localhost:10000}"
BOT_TOKEN="6148450508:AAFl4_SrGS-wh4wiO70XFb3dY_74ikigA1M"
BOT_ID="6148450508"

echo "🧪 Testing Rate Limiting System"
echo "================================"
echo "📍 Envoy URL: $ENVOY_URL"

# Function to test API endpoint
test_api_call() {
    local endpoint="$1"
    local description="$2"
    local expected_status="$3"
    
    echo -n "Testing $description... "
    
    response=$(curl -s -w "\n%{http_code}" "$ENVOY_URL/bot$BOT_TOKEN/$endpoint" 2>/dev/null)
    status_code=$(echo "$response" | tail -n1)
    body=$(echo "$response" | head -n -1)
    
    if [ "$status_code" = "$expected_status" ]; then
        echo "✅ PASS (Status: $status_code)"
        if [ "$status_code" = "200" ]; then
            echo "   Response: $(echo "$body" | jq -r '.message' 2>/dev/null || echo "$body")"
        fi
    else
        echo "❌ FAIL (Expected: $expected_status, Got: $status_code)"
        echo "   Response: $body"
    fi
    
    # Show rate limit headers if present
    headers=$(curl -s -I "$ENVOY_URL/bot$BOT_TOKEN/$endpoint" 2>/dev/null | grep -i "x-.*:")
    if [ -n "$headers" ]; then
        echo "   Headers:"
        echo "$headers" | sed 's/^/     /'
    fi
    
    echo
}

# Function to setup bot limits
setup_bot() {
    local rate_limit="$1"
    local description="$2"
    
    echo "🔧 Setting up bot with $description (rate: $rate_limit/sec)"
    
    if [ -f "scripts/setup_bot_limits.py" ]; then
        uv run scripts/setup_bot_limits.py --bot "$BOT_ID" --rate "$rate_limit" --action set
    else
        # Fallback: set directly in Redis
        redis-cli SET "rate_limit:$BOT_ID" "$rate_limit"
        echo "✅ Set rate limit via Redis CLI"
    fi
    echo
}

# Function to check Redis keys
check_redis_keys() {
    echo "🔍 Checking Redis Keys"
    echo "====================="
    
    echo "Rate limit keys:"
    redis-cli KEYS "rate_limit:*" | sed 's/^/  /'
    
    echo "Counter keys:"
    redis-cli KEYS "counter:*" | sed 's/^/  /'
    
    echo "Usage keys:"
    redis-cli KEYS "usage_total:*" | sed 's/^/  /'
    
    echo
}

# Function to flood test
flood_test() {
    local requests="$1"
    local description="$2"
    
    echo "🌊 Flood Test: $description ($requests requests)"
    echo "================================================"
    
    success_count=0
    rate_limited_count=0
    
    for i in $(seq 1 $requests); do
        status_code=$(curl -s -o /dev/null -w "%{http_code}" "$ENVOY_URL/bot$BOT_TOKEN/getUpdates")
        
        case $status_code in
            200) ((success_count++)) ;;
            429) ((rate_limited_count++)) ;;
            *) echo "Unexpected status: $status_code" ;;
        esac
        
        # Progress indicator
        if [ $((i % 10)) -eq 0 ]; then
            echo -n "."
        fi
    done
    
    echo
    echo "Results:"
    echo "  ✅ Successful requests: $success_count"
    echo "  🚫 Rate limited requests: $rate_limited_count"
    echo "  📊 Success rate: $(( success_count * 100 / requests ))%"
    echo
}

# Main test sequence
main() {
    echo "Starting tests at $(date)"
    echo
    
    # Wait for services to be ready
    echo "⏳ Waiting for services to be ready..."
    sleep 5
    
    # Test 1: Basic functionality
    echo "Test 1: Basic API Functionality"
    echo "==============================="
    setup_bot 50 "Trial limits"
    test_api_call "getUpdates" "valid bot endpoint" "200"
    test_api_call "getMe" "another valid endpoint" "200"
    
    # Test 2: Invalid bot token
    echo "Test 2: Invalid Bot Token"
    echo "========================="
    invalid_response=$(curl -s -w "\n%{http_code}" "$ENVOY_URL/bot_invalid_token/getUpdates")
    invalid_status=$(echo "$invalid_response" | tail -n1)
    echo "Invalid token test: $([ "$invalid_status" = "401" ] && echo "✅ PASS" || echo "❌ FAIL") (Status: $invalid_status)"
    echo
    
    # Test 3: Rate limiting
    echo "Test 3: Rate Limiting"
    echo "===================="
    setup_bot 5 "Low rate limit for testing"
    flood_test 20 "Testing rate limits"
    
    # Test 4: Check Redis state
    check_redis_keys
    
    # Test 5: Higher rate limits
    echo "Test 4: Higher Rate Limits"
    echo "=========================="
    setup_bot 100 "Pro limits"
    test_api_call "getUpdates" "with higher limits" "200"
    
    echo "🎉 Tests completed at $(date)"
}

# Helper function to show usage
usage() {
    echo "Usage: $0 [options]"
    echo "Options:"
    echo "  -h, --help     Show this help"
    echo "  --flood N      Run flood test with N requests"
    echo "  --setup RATE   Setup bot with specific rate limit"
    echo "  --keys         Show Redis keys only"
    echo
    echo "Examples:"
    echo "  $0                    # Run all tests"
    echo "  $0 --flood 100       # Flood test with 100 requests"
    echo "  $0 --setup 25        # Setup bot with 25 req/sec limit"
    echo "  $0 --keys            # Show current Redis keys"
}

# Parse command line arguments
case "${1:-}" in
    -h|--help)
        usage
        exit 0
        ;;
    --flood)
        setup_bot 10 "Flood test limits"
        flood_test "${2:-50}" "Manual flood test"
        ;;
    --setup)
        setup_bot "${2:-50}" "Manual setup"
        ;;
    --keys)
        check_redis_keys
        ;;
    "")
        main
        ;;
    *)
        echo "Unknown option: $1"
        usage
        exit 1
        ;;
esac 