#!/bin/bash
echo "Quick Test Suite"
echo "================"
echo

echo "1. Build bindings..."
go build ./pipewire/pipewire.go || exit 1
echo "✓ Bindings build"

echo
echo "2. Run tests..."
go test ./pipewire/ -timeout 5s || exit 1
echo "✓ Tests pass"

echo
echo "3. Build examples..."
go build -o simple_tone ./examples/simple_tone.go || exit 1
echo "✓ Tone generator built"

go build -o play_flac ./examples/play_flac.go || exit 1
echo "✓ FLAC player built"

echo
echo "4. Test tone generator..."
timeout -s INT 1 ./simple_tone 440 > /tmp/tone.log 2>&1 || true
if grep -q "Generating 440 Hz tone" /tmp/tone.log; then
    echo "✓ Tone generator works"
else
    echo "✗ Tone generator failed"
    cat /tmp/tone.log
    exit 1
fi

echo
echo "5. Test FLAC player..."
if [ -f test.flac ]; then
    timeout 3 ./play_flac test.flac > /tmp/flac.log 2>&1 || true
    if grep -q "Playback finished" /tmp/flac.log; then
        echo "✓ FLAC player works"
    else
        echo "✗ FLAC player failed"
        cat /tmp/flac.log
        exit 1
    fi
else
    echo "⚠ No test.flac file, skipping"
fi

echo
echo "========================"
echo "All tests passed! ✓"
echo "========================"
