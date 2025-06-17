#!/bin/bash

# FastAPI Service Setup Script
SERVICE_NAME="fastapi-test"
SERVICE_FILE="/etc/systemd/system/${SERVICE_NAME}.service"
WORKING_DIR="/root/api"

echo "Setting up FastAPI service..."

# Check if running as root
if [ "$EUID" -ne 0 ]; then
    echo "Please run as root (use sudo)"
    exit 1
fi

# Check if virtual environment exists
if [ ! -d "/root/api/.venv" ]; then
    echo "Virtual environment not found at /root/api/.venv"
    echo "Please create it first with: python3 -m venv /root/api/.venv"
    echo "Then activate it and install dependencies: source /root/api/.venv/bin/activate && pip install fastapi uvicorn"
    exit 1
fi

echo "Using virtual environment at /root/api/.venv"

# Create the service file (copy the content from the artifact above)
cat > $SERVICE_FILE << 'EOF'
[Unit]
Description=FastAPI Test Server
After=network.target

[Service]
Type=simple
User=root
Group=root
WorkingDirectory=/root/api
Environment=PATH=/usr/local/bin:/usr/bin:/bin
ExecStart=/root/api/.venv/bin/python -m uvicorn main:app --host localhost --port 8000
ExecReload=/bin/kill -HUP $MAINPID
Restart=always
RestartSec=10
StandardOutput=journal
StandardError=journal
SyslogIdentifier=fastapi-test

[Install]
WantedBy=multi-user.target
EOF

# Set proper permissions
chmod 644 $SERVICE_FILE

# Reload systemd daemon
systemctl daemon-reload

# Enable the service to start on boot
systemctl enable $SERVICE_NAME

echo "Service setup complete!"
echo ""
echo "Available commands:"
echo "  Start service:    sudo systemctl start $SERVICE_NAME"
echo "  Stop service:     sudo systemctl stop $SERVICE_NAME"
echo "  Restart service:  sudo systemctl restart $SERVICE_NAME"
echo "  Check status:     sudo systemctl status $SERVICE_NAME"
echo "  View logs:        sudo journalctl -u $SERVICE_NAME -f"
echo "  Disable service:  sudo systemctl disable $SERVICE_NAME"
echo ""

# Ask if user wants to start the service now
read -p "Start the service now? (y/n): " -n 1 -r
echo
if [[ $REPLY =~ ^[Yy]$ ]]; then
    systemctl start $SERVICE_NAME
    echo "Service started!"
    echo "Your API is now running at http://localhost:8000"
    echo "API docs available at http://localhost:8000/docs"
else
    echo "Service not started. Use 'sudo systemctl start $SERVICE_NAME' to start it later."
fi
