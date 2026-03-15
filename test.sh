#!/bin/bash
set -e

echo "========================================"
echo "  PipeWire Go Bindings - Test Suite"
echo "========================================"
echo

# Colors
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Test counter
TESTS_PASSED=0
TESTS_FAILED=0

# Helper function
test_status() {
    if [ $1 -eq 0 ]; then
        echo -e "${GREEN}✓ PASS${NC}: $2"
        ((TESTS_PASSED++))
    else
        echo -e "${RED}✗ FAIL${NC}: $2"
        ((TESTS_FAILED++))
    fi
}

echo "1. Checking prerequisites..."
echo "----------------------------"

# Check for Go
if command -v go &> /dev/null; then
    GO_VERSION=$(go version | awk '{print $3}')
    echo -e "${GREEN}✓${NC} Go found: $GO_VERSION"
else
    echo -e "${RED}✗${NC} Go not found"
    exit 1
fi

# Check for pkg-config
if command -v pkg-config &> /dev/null; then
    echo -e "${GREEN}✓${NC} pkg-config found"
else
    echo -e "${RED}✗${NC} pkg-config not found"
    exit 1
fi

# Check for PipeWire
if pkg-config --exists libpipewire-0.3; then
    PW_VERSION=$(pkg-config --modversion libpipewire-0.3)
    echo -e "${GREEN}✓${NC} PipeWire dev libraries found: $PW_VERSION"
else
    echo -e "${RED}✗${NC} PipeWire dev libraries not found"
    echo "Install with: sudo apt install libpipewire-0.3-dev"
    exit 1
fi

# Check if PipeWire is running
if systemctl --user is-active --quiet pipewire 2>/dev/null; then
    echo -e "${GREEN}✓${NC} PipeWire daemon is running"
elif pgrep -x pipewire > /dev/null; then
    echo -e "${YELLOW}⚠${NC} PipeWire is running (not via systemd)"
else
    echo -e "${YELLOW}⚠${NC} PipeWire daemon may not be running"
    echo "Start with: systemctl --user start pipewire"
fi

echo
echo "2. Building bindings..."
echo "-----------------------"

# Build the library
if go build ./pipewire/pipewire.go 2>&1 | grep -q "error"; then
    test_status 1 "Build pipewire bindings"
    exit 1
else
    test_status 0 "Build pipewire bindings"
fi

echo
echo "3. Running unit tests..."
echo "------------------------"

# Run tests
if go test ./pipewire/ -v > /tmp/test_output.txt 2>&1; then
    test_status 0 "Unit tests"
    cat /tmp/test_output.txt | grep -E "(PASS|SKIP|ok)"
else
    test_status 1 "Unit tests"
    cat /tmp/test_output.txt
fi

echo
echo "4. Building examples..."
echo "-----------------------"

# Build tone generator
if go build -o simple_tone ./examples/simple_tone.go 2>&1 > /dev/null; then
    test_status 0 "Build simple_tone"
else
    test_status 1 "Build simple_tone"
fi

# Build FLAC player
if go build -o play_flac ./examples/play_flac.go 2>&1 > /dev/null; then
    test_status 0 "Build play_flac"
else
    test_status 1 "Build play_flac"
fi

echo
echo "5. Testing tone generator..."
echo "----------------------------"

# Test tone generator (run for 1 second)
if timeout -s INT 1 ./simple_tone 440 > /tmp/tone_output.txt 2>&1; then
    if grep -q "Generating 440 Hz tone" /tmp/tone_output.txt; then
        test_status 0 "Run tone generator (440 Hz)"
    else
        test_status 1 "Run tone generator (output check)"
        cat /tmp/tone_output.txt
    fi
else
    # Check if it was just the timeout
    if grep -q "Generating 440 Hz tone" /tmp/tone_output.txt; then
        test_status 0 "Run tone generator (440 Hz)"
    else
        test_status 1 "Run tone generator (crashed)"
        cat /tmp/tone_output.txt
    fi
fi

echo
echo "6. Testing FLAC player..."
echo "-------------------------"

# Create test FLAC if ffmpeg is available
if command -v ffmpeg &> /dev/null; then
    echo "Creating test FLAC file..."
    ffmpeg -f lavfi -i "sine=frequency=440:duration=1" -ac 2 -ar 44100 test.flac -y 2>&1 | tail -1

    # Test FLAC player
    if timeout 2 ./play_flac test.flac > /tmp/flac_output.txt 2>&1; then
        if grep -q "FLAC info" /tmp/flac_output.txt && grep -q "Playback finished" /tmp/flac_output.txt; then
            test_status 0 "Run FLAC player"
        else
            test_status 1 "Run FLAC player (incomplete playback)"
            cat /tmp/flac_output.txt
        fi
    else
        test_status 1 "Run FLAC player (crashed)"
        cat /tmp/flac_output.txt
    fi
else
    echo -e "${YELLOW}⚠${NC} ffmpeg not found, skipping FLAC player test"
fi

echo
echo "========================================"
echo "           Test Summary"
echo "========================================"
echo -e "Tests passed: ${GREEN}$TESTS_PASSED${NC}"
echo -e "Tests failed: ${RED}$TESTS_FAILED${NC}"
echo

if [ $TESTS_FAILED -eq 0 ]; then
    echo -e "${GREEN}All tests passed!${NC}"
    exit 0
else
    echo -e "${RED}Some tests failed.${NC}"
    exit 1
fi
