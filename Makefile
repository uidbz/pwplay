.PHONY: all build clean test install-deps

all: build

build:
	@echo "Building..."
	@go build -o pwplay-server ./cmd/server
	@go build -o pwplay-client ./cmd/client
	@go build -o pwplay-player ./cmd/player
	@echo "Build complete: pwplay-server, pwplay-client, pwplay-player"

clean:
	@echo "Cleaning..."
	@rm -f pwplay-server pwplay-client pwplay-player
	@rm -f pw-server pw-client pw-player simple_tone simple_playlist
	@rm -f play_flac playlist_player playlist_interactive webservice webservice_v2 interactive_playlist pwclient
	@go clean

test:
	@echo "Running tests..."
	@go test $(shell go list ./... | grep -v /examples)

install-deps:
	@echo "Installing Go dependencies..."
	@go mod download
	@echo "Done. Make sure you have PipeWire development libraries installed:"
	@echo "  Ubuntu/Debian: sudo apt install libpipewire-0.3-dev pkg-config"
	@echo "  Fedora: sudo dnf install pipewire-devel pkg-config"
	@echo "  Arch: sudo pacman -S pipewire pkg-config"
