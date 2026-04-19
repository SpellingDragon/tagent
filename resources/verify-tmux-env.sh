#!/bin/bash
# TmuxMonitor Test Environment Verification Script
# This script verifies that the environment is ready for TmuxMonitor testing

set -e

echo "====================================="
echo "TmuxMonitor Test Environment Check"
echo "====================================="

PASS=0
FAIL=0

# Check 1: tmux installed
echo ""
echo "Check 1: tmux installation"
if command -v tmux &> /dev/null; then
    echo "  ✓ tmux found: $(tmux -V)"
    PASS=$((PASS + 1))
else
    echo "  ✗ tmux not found"
    echo "    Install with: brew install tmux (macOS) or apt-get install tmux (Linux)"
    FAIL=$((FAIL + 1))
fi

# Check 2: Go installed
echo ""
echo "Check 2: Go installation"
if command -v go &> /dev/null; then
    echo "  ✓ Go found: $(go version)"
    PASS=$((PASS + 1))
else
    echo "  ✗ Go not found"
    FAIL=$((FAIL + 1))
fi

# Check 3: Run tmux basic test
echo ""
echo "Check 3: tmux basic functionality"
if command -v tmux &> /dev/null; then
    SESSION_NAME="test_verify_$$"
    
    # Test session creation
    if tmux new-session -d -s "$SESSION_NAME" "echo test" 2>/dev/null; then
        sleep 0.5
        
        # Test session exists
        if tmux has-session -t "$SESSION_NAME" 2>/dev/null; then
            echo "  ✓ tmux session creation: OK"
            PASS=$((PASS + 1))
            
            # Test output capture
            OUTPUT=$(tmux capture-pane -p -t "$SESSION_NAME" 2>/dev/null)
            if [ -n "$OUTPUT" ]; then
                echo "  ✓ tmux output capture: OK"
                PASS=$((PASS + 1))
            else
                echo "  ✗ tmux output capture: FAILED"
                FAIL=$((FAIL + 1))
            fi
            
            # Cleanup
            tmux kill-session -t "$SESSION_NAME" 2>/dev/null
            echo "  ✓ tmux session cleanup: OK"
        else
            echo "  ✗ tmux session verification: FAILED"
            FAIL=$((FAIL + 1))
        fi
    else
        echo "  ✗ tmux session creation: FAILED"
        FAIL=$((FAIL + 1))
    fi
else
    echo "  ⊘ Skipped (tmux not installed)"
fi

# Check 4: Run Go tests
echo ""
echo "Check 4: Go test compilation"
cd "$(dirname "$0")/.."
if go test ./tool/... -run "^$" -timeout=10s 2>/dev/null; then
    echo "  ✓ Go tests compile successfully"
    PASS=$((PASS + 1))
else
    echo "  ✗ Go test compilation failed"
    FAIL=$((FAIL + 1))
fi

# Check 5: Run tmux-dependent tests
echo ""
echo "Check 5: tmux-dependent tests"
if command -v tmux &> /dev/null; then
    # Run a single tmux test
    if go test ./tool/... -run "TestTmuxExecutor_SessionManagement" -v -timeout=30s 2>&1 | grep -q "PASS"; then
        echo "  ✓ tmux tests pass"
        PASS=$((PASS + 1))
    else
        echo "  ✗ tmux tests failed"
        FAIL=$((FAIL + 1))
    fi
else
    echo "  ⊘ Skipped (tmux not installed)"
fi

# Summary
echo ""
echo "====================================="
echo "Summary: $PASS passed, $FAIL failed"
echo "====================================="

if [ $FAIL -eq 0 ]; then
    echo "✓ Environment is ready for TmuxMonitor testing!"
    exit 0
else
    echo "✗ Some checks failed. Please fix the issues above."
    exit 1
fi
