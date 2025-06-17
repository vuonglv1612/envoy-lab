from fastapi import FastAPI, HTTPException
from fastapi.responses import JSONResponse
from typing import Optional
from pydantic import BaseModel

app = FastAPI(title="Test API", description="Simple FastAPI server for testing")

class MessageRequest(BaseModel):
    message: str
    status: Optional[int] = None

@app.post("/sendMessage")
async def send_message(request: MessageRequest):
    """
    Send a message endpoint for testing.
    
    - **message**: The message content to send
    - **status**: Optional HTTP status code to return (defaults to 200)
    """
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
    return {"message": "FastAPI Test Server is running"}

if __name__ == "__main__":
    import uvicorn
    uvicorn.run(app, host="0.0.0.0", port=8000)
