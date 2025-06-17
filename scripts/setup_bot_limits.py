#!/usr/bin/env python3

import argparse
import redis
import sys
import time
from datetime import datetime, timezone
from typing import Optional


def main():
    parser = argparse.ArgumentParser(description="Manage bot rate limits in Redis")
    parser.add_argument("--redis", default="localhost:6379", help="Redis address (default: localhost:6379)")
    parser.add_argument("--bot", required=True, help="Bot ID (part before colon in token)")
    parser.add_argument("--rate", type=int, default=50, help="Rate limit per second (default: 50)")
    parser.add_argument("--quota", type=int, default=5000, help="Quota limit for the period (default: 5000)")
    parser.add_argument("--action", choices=["set", "get", "delete"], default="set", help="Action to perform (default: set)")
    
    args = parser.parse_args()
    
    # Parse Redis address
    if ":" in args.redis:
        host, port = args.redis.split(":", 1)
        port = int(port)
    else:
        host = args.redis
        port = 6379
    
    # Initialize Redis client
    try:
        client = redis.Redis(host=host, port=port, decode_responses=True)
        # Test connection
        client.ping()
        print(f"✅ Connected to Redis at {args.redis}")
    except Exception as e:
        print(f"❌ Failed to connect to Redis: {e}")
        sys.exit(1)
    
    # Execute the requested action
    if args.action == "set":
        setup_bot_limits(client, args.bot, args.rate, args.quota)
    elif args.action == "get":
        get_bot_status(client, args.bot)
    elif args.action == "delete":
        delete_bot_limits(client, args.bot)


def setup_bot_limits(client: redis.Redis, bot_id: str, rate_limit: int, quota_limit: int):
    """Set up rate limits for a bot in Redis"""
    rate_limit_key = f"rate_limit:{bot_id}"
    
    try:
        # Set rate limit
        client.set(rate_limit_key, rate_limit)
        
        print("✅ Bot limits configured successfully:")
        print(f"   Bot ID: {bot_id}")
        print(f"   Rate Limit: {rate_limit} requests/second")
        print(f"   Quota Limit: {quota_limit} requests/period")
        print(f"   Redis Key: {rate_limit_key}")
        
    except Exception as e:
        print(f"❌ Failed to set rate limit: {e}")
        sys.exit(1)


def get_bot_status(client: redis.Redis, bot_id: str):
    """Get current status of a bot from Redis"""
    # Keys for the bot
    rate_limit_key = f"rate_limit:{bot_id}"
    current_second = get_current_second()
    counter_key = f"counter:{bot_id}:{current_second}"
    usage_key = f"usage_total:{bot_id}:{get_current_period()}"
    
    try:
        # Get values
        rate_limit = client.get(rate_limit_key) or "50 (default)"
        current_rate = client.get(counter_key) or "0"
        current_usage = client.get(usage_key) or "0"
        
        print(f"🤖 Bot Status: {bot_id}")
        print(f"   Rate Limit: {rate_limit}/second")
        print(f"   Current Rate: {current_rate} requests this second")
        print(f"   Usage This Period: {current_usage} requests")
        print("   Keys:")
        print(f"     - Rate Limit: {rate_limit_key}")
        print(f"     - Counter: {counter_key}")
        print(f"     - Usage: {usage_key}")
        
    except Exception as e:
        print(f"❌ Error getting bot status: {e}")


def delete_bot_limits(client: redis.Redis, bot_id: str):
    """Delete all keys for a bot from Redis"""
    try:
        # Find all keys for this bot
        pattern = f"*{bot_id}*"
        keys = client.keys(pattern)
        
        if not keys:
            print(f"No keys found for bot: {bot_id}")
            return
        
        # Delete all found keys
        deleted = client.delete(*keys)
        
        print(f"🗑️  Deleted {deleted} keys for bot {bot_id}:")
        for key in keys:
            print(f"   - {key}")
            
    except Exception as e:
        print(f"❌ Failed to delete keys: {e}")
        sys.exit(1)


def get_current_second() -> int:
    """Get current timestamp in seconds"""
    return int(time.time())


def get_current_period() -> int:
    """Get current billing period start timestamp"""
    # For simplicity, use monthly periods starting from the beginning of the month
    now = datetime.now(timezone.utc)
    period_start = datetime(now.year, now.month, 1, tzinfo=timezone.utc)
    return int(period_start.timestamp())


if __name__ == "__main__":
    main()


# Example usage:
# python setup_bot_limits.py --bot 6148450508 --rate 100 --quota 10000 --action set
# python setup_bot_limits.py --bot 6148450508 --action get
# python setup_bot_limits.py --bot 6148450508 --action delete 