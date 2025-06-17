from fastapi import FastAPI, HTTPException, Request
from fastapi.responses import JSONResponse
from typing import Optional, Dict, Any
from pydantic import BaseModel
import re
import time
import random

app = FastAPI(title="Bot API Backend", description="Bot API server for testing rate limiting")

class MessageRequest(BaseModel):
    message: str
    status: Optional[int] = None
    chat_id: Optional[str] = None

class SendMessageRequest(BaseModel):
    chat_id: str
    text: str
    parse_mode: Optional[str] = None

# Bot token validation regex
BOT_TOKEN_REGEX = re.compile(r'^([0-9]+):([A-Za-z0-9_-]+)$')

def validate_bot_token(token: str) -> bool:
    """Validate bot token format"""
    return bool(BOT_TOKEN_REGEX.match(token))

def extract_bot_id(token: str) -> str:
    """Extract bot ID from token"""
    return token.split(':')[0] if ':' in token else token

# Bot API Endpoints

@app.get("/bot{bot_token}/getMe")
async def get_me(bot_token: str):
    """
    Get bot information (Telegram Bot API getMe method)
    """
    if not validate_bot_token(bot_token):
        raise HTTPException(status_code=401, detail="Unauthorized: Invalid bot token")
    
    bot_id = extract_bot_id(bot_token)
    
    return {
        "ok": True,
        "result": {
            "id": int(bot_id),
            "is_bot": True,
            "first_name": f"TestBot{bot_id}",
            "username": f"testbot{bot_id}_bot",
            "can_join_groups": True,
            "can_read_all_group_messages": False,
            "supports_inline_queries": False
        }
    }

@app.get("/bot{bot_token}/getUpdates")
async def get_updates(bot_token: str, offset: Optional[int] = None, limit: Optional[int] = 100, timeout: Optional[int] = 0):
    """
    Get updates (Telegram Bot API getUpdates method)
    """
    if not validate_bot_token(bot_token):
        raise HTTPException(status_code=401, detail="Unauthorized: Invalid bot token")
    
    # Simulate some updates
    updates = []
    if random.random() > 0.7:  # 30% chance of having updates
        update_id = int(time.time()) + random.randint(1, 1000)
        updates = [{
            "update_id": update_id,
            "message": {
                "message_id": random.randint(1, 10000),
                "from": {
                    "id": 12345,
                    "is_bot": False,
                    "first_name": "Test",
                    "username": "testuser"
                },
                "chat": {
                    "id": 12345,
                    "first_name": "Test",
                    "username": "testuser",
                    "type": "private"
                },
                "date": int(time.time()),
                "text": "/start"
            }
        }]
    
    return {
        "ok": True,
        "result": updates
    }

@app.post("/bot{bot_token}/sendMessage")
async def send_message_bot(bot_token: str, request: SendMessageRequest):
    """
    Send message (Telegram Bot API sendMessage method)
    """
    if not validate_bot_token(bot_token):
        raise HTTPException(status_code=401, detail="Unauthorized: Invalid bot token")
    
    # Check if text is 'error' and raise 500 status code
    if request.text == "error":
        raise HTTPException(status_code=500, detail="Internal Server Error: Error message triggered")
    
    message_id = random.randint(1, 10000)
    bot_id = extract_bot_id(bot_token)
    
    return {
        "ok": True,
        "result": {
            "message_id": message_id,
            "from": {
                "id": int(bot_id),
                "is_bot": True,
                "first_name": f"TestBot{bot_id}",
                "username": f"testbot{bot_id}_bot"
            },
            "chat": {
                "id": int(request.chat_id),
                "type": "private"
            },
            "date": int(time.time()),
            "text": request.text
        }
    }

@app.get("/bot{bot_token}/getWebhookInfo")
async def get_webhook_info(bot_token: str):
    """
    Get webhook info (Telegram Bot API getWebhookInfo method)
    """
    if not validate_bot_token(bot_token):
        raise HTTPException(status_code=401, detail="Unauthorized: Invalid bot token")
    
    return {
        "ok": True,
        "result": {
            "url": "",
            "has_custom_certificate": False,
            "pending_update_count": 0
        }
    }

@app.post("/bot{bot_token}/setWebhook")
async def set_webhook(bot_token: str, request: Dict[str, Any]):
    """
    Set webhook (Telegram Bot API setWebhook method)
    """
    if not validate_bot_token(bot_token):
        raise HTTPException(status_code=401, detail="Unauthorized: Invalid bot token")
    
    return {
        "ok": True,
        "result": True,
        "description": "Webhook was set"
    }

@app.get("/bot{bot_token}/getChat")
async def get_chat(bot_token: str, chat_id: str):
    """
    Get chat info (Telegram Bot API getChat method)
    """
    if not validate_bot_token(bot_token):
        raise HTTPException(status_code=401, detail="Unauthorized: Invalid bot token")
    
    return {
        "ok": True,
        "result": {
            "id": int(chat_id),
            "type": "private",
            "username": "testuser",
            "first_name": "Test",
            "last_name": "User"
        }
    }

@app.get("/bot{bot_token}/getChatMember")
async def get_chat_member(bot_token: str, chat_id: str, user_id: int):
    """
    Get chat member info (Telegram Bot API getChatMember method)
    """
    if not validate_bot_token(bot_token):
        raise HTTPException(status_code=401, detail="Unauthorized: Invalid bot token")
    
    return {
        "ok": True,
        "result": {
            "user": {
                "id": user_id,
                "is_bot": False,
                "first_name": "Test",
                "username": "testuser"
            },
            "status": "member"
        }
    }

# Generic handler for any other bot API methods
@app.api_route("/bot{bot_token}/{method_name}", methods=["GET", "POST"])
async def generic_bot_method(bot_token: str, method_name: str, request: Request):
    """
    Generic handler for any bot API method
    """
    if not validate_bot_token(bot_token):
        raise HTTPException(status_code=401, detail="Unauthorized: Invalid bot token")
    
    # Get request body if it's a POST request
    body = {}
    if request.method == "POST":
        try:
            body = await request.json()
        except:
            body = {}
    
    # Get query parameters
    query_params = dict(request.query_params)
    
    bot_id = extract_bot_id(bot_token)
    
    return {
        "ok": True,
        "result": {
            "method": method_name,
            "bot_id": bot_id,
            "timestamp": int(time.time()),
            "body": body,
            "query_params": query_params,
            "message": f"Method {method_name} executed successfully for bot {bot_id}"
        }
    }

# Legacy endpoint for testing (backward compatibility)
@app.post("/sendMessage")
async def send_message_legacy(request: MessageRequest):
    """
    Legacy send message endpoint for testing.
    
    - **message**: The message content to send
    - **status**: Optional HTTP status code to return (defaults to 200)
    """
    # Check if message is 'error' and raise 500 status code
    if request.message == "error":
        raise HTTPException(status_code=500, detail="Internal Server Error: Error message triggered")
    
    # Use provided status or default to 200
    status_code = request.status if request.status is not None else 200
    
    # Validate status code range
    if status_code < 100 or status_code > 599:
        raise HTTPException(status_code=400, detail="Invalid status code. Must be between 100-599")
    
    response_data = {
        "message": f"Message received: {request.message}",
        "status": status_code,
        "success": True
    }
    
    return JSONResponse(content=response_data, status_code=status_code)

@app.get("/")
async def root():
    """Root endpoint for health check"""
    return {
        "message": "Bot API Backend is running",
        "version": "1.0.0",
        "endpoints": [
            "/bot{token}/getMe",
            "/bot{token}/getUpdates", 
            "/bot{token}/sendMessage",
            "/bot{token}/getWebhookInfo",
            "/bot{token}/setWebhook",
            "/bot{token}/getChat",
            "/bot{token}/getChatMember",
            "/bot{token}/{method_name}"
        ]
    }

if __name__ == "__main__":
    import uvicorn
    uvicorn.run(app, host="0.0.0.0", port=8000)
