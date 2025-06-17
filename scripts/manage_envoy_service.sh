#!/bin/bash

# Script to manage Envoy systemd service
set -e

SERVICE_NAME="envoy"
SERVICE_FILE="/etc/systemd/system/${SERVICE_NAME}.service"
PROJECT_SERVICE_FILE="./envoy.service"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Function to print colored output
print_status() {
    local color="$1"
    local message="$2"
    echo -e "${color}${message}${NC}"
}

# Function to check if running as root
check_root() {
    if [ "$EUID" -ne 0 ]; then
        print_status "$RED" "❌ This script must be run as root (use sudo)"
        exit 1
    fi
}

# Function to install the service
install_service() {
    print_status "$BLUE" "📦 Installing Envoy systemd service..."
    
    # Check if service file exists in project
    if [ ! -f "$PROJECT_SERVICE_FILE" ]; then
        print_status "$RED" "❌ Service file not found: $PROJECT_SERVICE_FILE"
        exit 1
    fi
    
    # Copy service file
    cp "$PROJECT_SERVICE_FILE" "$SERVICE_FILE"
    print_status "$GREEN" "✅ Copied service file to $SERVICE_FILE"
    
    # Reload systemd
    systemctl daemon-reload
    print_status "$GREEN" "✅ Reloaded systemd daemon"
    
    # Enable service
    systemctl enable "$SERVICE_NAME"
    print_status "$GREEN" "✅ Enabled $SERVICE_NAME service"
    
    print_status "$YELLOW" "💡 Service installed successfully!"
    print_status "$YELLOW" "   Use 'sudo systemctl start envoy' to start the service"
}

# Function to uninstall the service
uninstall_service() {
    print_status "$BLUE" "🗑️  Uninstalling Envoy systemd service..."
    
    # Stop service if running
    if systemctl is-active --quiet "$SERVICE_NAME"; then
        systemctl stop "$SERVICE_NAME"
        print_status "$GREEN" "✅ Stopped $SERVICE_NAME service"
    fi
    
    # Disable service
    if systemctl is-enabled --quiet "$SERVICE_NAME"; then
        systemctl disable "$SERVICE_NAME"
        print_status "$GREEN" "✅ Disabled $SERVICE_NAME service"
    fi
    
    # Remove service file
    if [ -f "$SERVICE_FILE" ]; then
        rm "$SERVICE_FILE"
        print_status "$GREEN" "✅ Removed service file"
    fi
    
    # Reload systemd
    systemctl daemon-reload
    print_status "$GREEN" "✅ Reloaded systemd daemon"
    
    print_status "$YELLOW" "💡 Service uninstalled successfully!"
}

# Function to update the service
update_service() {
    print_status "$BLUE" "🔄 Updating Envoy systemd service..."
    
    if [ ! -f "$PROJECT_SERVICE_FILE" ]; then
        print_status "$RED" "❌ Service file not found: $PROJECT_SERVICE_FILE"
        exit 1
    fi
    
    # Check if service is running
    local was_running=false
    if systemctl is-active --quiet "$SERVICE_NAME"; then
        was_running=true
        systemctl stop "$SERVICE_NAME"
        print_status "$YELLOW" "⏸️  Stopped service for update"
    fi
    
    # Update service file
    cp "$PROJECT_SERVICE_FILE" "$SERVICE_FILE"
    systemctl daemon-reload
    print_status "$GREEN" "✅ Updated service file"
    
    # Restart if it was running
    if [ "$was_running" = true ]; then
        systemctl start "$SERVICE_NAME"
        print_status "$GREEN" "✅ Restarted service"
    fi
    
    print_status "$YELLOW" "💡 Service updated successfully!"
}

# Function to show service status
show_status() {
    print_status "$BLUE" "📊 Envoy Service Status"
    echo "========================"
    
    # Service status
    echo -n "Service Status: "
    if systemctl is-active --quiet "$SERVICE_NAME"; then
        print_status "$GREEN" "ACTIVE"
    else
        print_status "$RED" "INACTIVE"
    fi
    
    # Service enabled status
    echo -n "Service Enabled: "
    if systemctl is-enabled --quiet "$SERVICE_NAME"; then
        print_status "$GREEN" "ENABLED"
    else
        print_status "$RED" "DISABLED"
    fi
    
    echo ""
    systemctl status "$SERVICE_NAME" --no-pager -l
    
    echo ""
    print_status "$BLUE" "📋 Recent Logs:"
    journalctl -u "$SERVICE_NAME" --no-pager -l -n 10
}

# Function to show logs
show_logs() {
    local lines="${1:-50}"
    print_status "$BLUE" "📜 Envoy Service Logs (last $lines lines)"
    echo "========================================"
    journalctl -u "$SERVICE_NAME" --no-pager -l -n "$lines"
}

# Function to follow logs
follow_logs() {
    print_status "$BLUE" "📜 Following Envoy Service Logs (Ctrl+C to stop)"
    echo "================================================"
    journalctl -u "$SERVICE_NAME" -f
}

# Function to test Envoy endpoints
test_envoy() {
    print_status "$BLUE" "🧪 Testing Envoy Endpoints"
    echo "=========================="
    
    # Test admin interface
    echo -n "Admin Interface (9901): "
    if curl -s --connect-timeout 2 http://localhost:9901/ready > /dev/null; then
        print_status "$GREEN" "✅ READY"
    else
        print_status "$RED" "❌ NOT READY"
    fi
    
    # Test proxy interface
    echo -n "Proxy Interface (10000): "
    if curl -s --connect-timeout 2 http://localhost:10000/ > /dev/null; then
        print_status "$GREEN" "✅ ACCESSIBLE"
    else
        print_status "$RED" "❌ NOT ACCESSIBLE"
    fi
    
    # Test with bot endpoint if backend is running
    echo -n "Bot API Test: "
    if curl -s --connect-timeout 2 "http://localhost:10000/bot6148450508:AAFl4_SrGS-wh4wiO70XFb3dY_74ikigA1M/getMe" > /dev/null; then
        print_status "$GREEN" "✅ WORKING"
    else
        print_status "$YELLOW" "⚠️  NOT WORKING (check backend services)"
    fi
}

# Function to show help
show_help() {
    echo "Envoy Systemd Service Manager"
    echo "============================="
    echo ""
    echo "Usage: $0 [COMMAND]"
    echo ""
    echo "Commands:"
    echo "  install     Install the Envoy systemd service"
    echo "  uninstall   Uninstall the Envoy systemd service"
    echo "  update      Update the service configuration"
    echo "  start       Start the Envoy service"
    echo "  stop        Stop the Envoy service"
    echo "  restart     Restart the Envoy service"
    echo "  status      Show service status and recent logs"
    echo "  logs [N]    Show last N lines of logs (default: 50)"
    echo "  follow      Follow logs in real-time"
    echo "  test        Test Envoy endpoints"
    echo "  reload      Reload Envoy configuration"
    echo ""
    echo "Examples:"
    echo "  sudo $0 install         # Install and enable the service"
    echo "  sudo $0 start           # Start the service"
    echo "  $0 status               # Check service status"
    echo "  $0 logs 100             # Show last 100 log lines"
    echo "  $0 test                 # Test if Envoy is working"
}

# Main logic
case "${1:-}" in
    install)
        check_root
        install_service
        ;;
    uninstall)
        check_root
        uninstall_service
        ;;
    update)
        check_root
        update_service
        ;;
    start)
        check_root
        systemctl start "$SERVICE_NAME"
        print_status "$GREEN" "✅ Started $SERVICE_NAME service"
        ;;
    stop)
        check_root
        systemctl stop "$SERVICE_NAME"
        print_status "$GREEN" "✅ Stopped $SERVICE_NAME service"
        ;;
    restart)
        check_root
        systemctl restart "$SERVICE_NAME"
        print_status "$GREEN" "✅ Restarted $SERVICE_NAME service"
        ;;
    reload)
        check_root
        systemctl reload "$SERVICE_NAME"
        print_status "$GREEN" "✅ Reloaded $SERVICE_NAME configuration"
        ;;
    status)
        show_status
        ;;
    logs)
        show_logs "${2:-50}"
        ;;
    follow)
        follow_logs
        ;;
    test)
        test_envoy
        ;;
    -h|--help|help|"")
        show_help
        ;;
    *)
        print_status "$RED" "❌ Unknown command: $1"
        echo ""
        show_help
        exit 1
        ;;
esac 