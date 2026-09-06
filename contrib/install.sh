#!/usr/bin/env bash
# Install pwplay-server as a per-user service.
#
# The server plays through PipeWire, which runs inside the user's session, so
# it is installed as a *user* service — not a system service — for whichever
# user runs the installer (use sudo -E when installing for yourself from a
# root shell, or just run it as that user directly).
#
# What it does:
#   1. builds pwplay-server (needs the Go toolchain and PipeWire dev libs),
#   2. installs the binary to $BINDIR (root only, default /usr/local/bin),
#   3. installs the service file for the detected init system:
#        systemd -> ~/.config/systemd/user/pwplay-server.service
#        OpenRC  -> ~/.local/share/rc/init.d/pwplay-server
#      and enables it (systemctl --user enable / rc-update --user add).
#
# Usage:
#   make install            # from the repo root, as the audio user
#   contrib/install.sh      # same thing, direct
#
# Re-running is safe: the binary and unit are simply replaced.
set -euo pipefail

PREFIX="${PREFIX:-/usr/local}"
BINDIR="$PREFIX/bin"
BINARY="pwplay-server"

# Resolve repo root from this script's location (contrib/ -> repo root).
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

log()  { printf '\033[1;32m==>\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m==>\033[0m %s\n' "$*" >&2; }
die()  { printf '\033[1;31merror:\033[0m %s\n' "$*" >&2; exit 1; }

# When invoked via sudo, act on the invoking user, not root: the service must
# live in the session that owns the PipeWire socket. When run directly (not via
# sudo), honor $HOME so a sandboxed/test HOME is respected.
SERVICE_USER="${SUDO_USER:-$(id -un)}"
if [ -n "${SUDO_USER:-}" ]; then
	SERVICE_HOME="$(getent passwd "$SERVICE_USER" | cut -d: -f6)"
else
	SERVICE_HOME="$HOME"
fi
[ -n "$SERVICE_HOME" ] || die "cannot determine home directory for user '$SERVICE_USER'"

# --- Detect init system --------------------------------------------------
INIT=""
if [ -d /run/systemd/system ]; then
	INIT="systemd"
elif command -v rc-update >/dev/null 2>&1; then
	INIT="openrc"
else
	warn "no supported init system detected; the binary will be installed, but no service will be set up"
fi
log "init system: ${INIT:-none} (user: $SERVICE_USER)"

# --- Build ---------------------------------------------------------------
command -v go >/dev/null 2>&1 || die "go toolchain not found in PATH"
log "building $BINARY"
# PipeWire is cgo, so this is not a static build; it needs
# libpipewire-0.3-dev (Debian) / pipewire-devel (Fedora) / pipewire (Arch).
( cd "$REPO_ROOT" && go build -buildvcs=false -o "$SCRIPT_DIR/$BINARY" "./cmd/server" )

# --- Binary (root only, or when PREFIX is user-writable) -----------------
if [ "$(id -u)" -eq 0 ] || [ -w "${BINDIR%/bin}" ] 2>/dev/null || { mkdir -p "$BINDIR" 2>/dev/null && [ -w "$BINDIR" ]; }; then
	log "installing binary to $BINDIR"
	install -d "$BINDIR"
	install -m 0755 "$SCRIPT_DIR/$BINARY" "$BINDIR/$BINARY"
else
	warn "not root and $BINDIR not writable: skipping binary install (service expects $BINDIR/$BINARY)"
	warn "re-run with sudo to install the binary, or edit the service file's ExecStart"
fi
rm -f "$SCRIPT_DIR/$BINARY"

# Only chown service files when installing for a different user (via sudo).
INSTALL_OWNER=""
if [ -n "${SUDO_USER:-}" ] && [ "$(id -un)" != "$SERVICE_USER" ]; then
	INSTALL_OWNER="-o $SERVICE_USER -g $SERVICE_USER"
fi

# --- Service file ---------------------------------------------------------
# run_as_service_user <cmd...>: run a command as the service user with a
# user-session environment (XDG_RUNTIME_DIR, and a user D-Bus for systemd).
run_as_service_user() {
	local uid
	uid="$(id -u "$SERVICE_USER")"
	if [ "$(id -un)" = "$SERVICE_USER" ]; then
		"$@"
	else
		sudo -u "$SERVICE_USER" \
			XDG_RUNTIME_DIR="/run/user/$uid" \
			DBUS_SESSION_BUS_ADDRESS="unix:path=/run/user/$uid/bus" \
			"$@"
	fi
}

case "$INIT" in
systemd)
	UNITDIR="$SERVICE_HOME/.config/systemd/user"
	log "installing systemd user unit to $UNITDIR"
	install -d $INSTALL_OWNER -m 0755 "$UNITDIR"
	install $INSTALL_OWNER -m 0644 \
		"$SCRIPT_DIR/systemd/$BINARY.service" "$UNITDIR/$BINARY.service"
	log "enabling service"
	run_as_service_user systemctl --user daemon-reload
	run_as_service_user systemctl --user enable "$BINARY.service"
	cat <<-EOF

	Done. Start it as user '$SERVICE_USER' with:
	    systemctl --user start $BINARY
	    systemctl --user status $BINARY

	To have it start at boot (without login), enable lingering:
	    sudo loginctl enable-linger $SERVICE_USER
	EOF
	;;
openrc)
	INITDIR="$SERVICE_HOME/.local/share/rc/init.d"
	log "installing OpenRC user service to $INITDIR"
	install -d $INSTALL_OWNER -m 0755 "$INITDIR"
	install $INSTALL_OWNER -m 0755 \
		"$SCRIPT_DIR/openrc/$BINARY" "$INITDIR/$BINARY"
	if run_as_service_user rc-update add --user "$BINARY" default 2>/dev/null; then
		log "added service to the user default runlevel"
	else
		warn "could not add to a user runlevel automatically; add it with:"
		warn "    rc-update add --user $BINARY default"
	fi
	cat <<-EOF

	Done. Start it as user '$SERVICE_USER' with:
	    rc-service --user $BINARY start
	EOF
	;;
*)
	cat <<-EOF

	Binary installed. Start it manually from the audio session, e.g.:
	    $BINDIR/$BINARY &
	EOF
	;;
esac
