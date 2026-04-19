#!/bin/bash
# tmux Installation Script for TmuxMonitor Testing
# This script installs tmux and verifies the installation

set -e

echo "====================================="
echo "tmux Installation Script"
echo "====================================="

# Check if tmux is already installed
if command -v tmux &> /dev/null; then
    echo "✓ tmux is already installed: $(tmux -V)"
    exit 0
fi

# Detect OS
OS=$(uname -s)

case "$OS" in
    Darwin)
        echo "Detected macOS"
        echo "Installing tmux via Homebrew..."
        
        # Check if Homebrew is installed
        if ! command -v brew &> /dev/null; then
            echo "✗ Homebrew not found. Installing Homebrew first..."
            /bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
        fi
        
        brew install tmux
        ;;
    
    Linux)
        echo "Detected Linux"
        
        # Detect package manager
        if command -v apt-get &> /dev/null; then
            echo "Installing tmux via apt-get..."
            sudo apt-get update
            sudo apt-get install -y tmux
        elif command -v yum &> /dev/null; then
            echo "Installing tmux via yum..."
            sudo yum install -y tmux
        elif command -v dnf &> /dev/null; then
            echo "Installing tmux via dnf..."
            sudo dnf install -y tmux
        else
            echo "✗ Unsupported package manager. Please install tmux manually."
            exit 1
        fi
        ;;
    
    *)
        echo "✗ Unsupported OS: $OS"
        echo "Please install tmux manually from https://github.com/tmux/tmux"
        exit 1
        ;;
esac

# Verify installation
echo ""
echo "====================================="
echo "Verifying tmux installation"
echo "====================================="

if command -v tmux &> /dev/null; then
    echo "✓ tmux installed successfully: $(tmux -V)"
    
    # Run a quick test
    echo ""
    echo "Running quick tmux test..."
    SESSION_NAME="test_install_$$"
    
    # Create test session
    tmux new-session -d -s "$SESSION_NAME" "echo hello"
    sleep 1
    
    # Check if session exists
    if tmux has-session -t "$SESSION_NAME" 2>/dev/null; then
        echo "✓ tmux session creation: OK"
        tmux kill-session -t "$SESSION_NAME"
        echo "✓ tmux session cleanup: OK"
    else
        echo "✗ tmux session test failed"
        exit 1
    fi
    
    echo ""
    echo "====================================="
    echo "tmux is ready for TmuxMonitor testing!"
    echo "====================================="
else
    echo "✗ tmux installation failed"
    exit 1
fi
