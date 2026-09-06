#!/usr/bin/env bash
# Install pwplay-server as a per-user service.
#
# The server plays through PipeWire, which runs inside the user's session, so
# it is installed as a *user* service — not a system service — for whichever
# user runs the installer. Root is only needed to place the binary in
# /usr/local/bin; run it as `sudo make install` so the service is still set up
# for the invoking (audio) user, not root. Running via plain `su`/`sudo su`
# would target root, which has no PipeWire session, and is refused.
#
# What it does:
#   1. builds pwplay-server (needs the Go toolchain and PipeWire dev libs),
#   2. installs the binary to $BINDIR (root only, default /usr/local/bin),
#   3. installs the service file for the detected init system:
#        systemd -> ~/.config/systemd/user/pwplay-server.service
#        OpenRC  -> ~/.config/rc/init.d/pwplay-server
#      and enables it (systemctl --user enable / rc-update --user add).
#
# Usage:
#   sudo make install       # from the repo root (binary + service)
#   make install            # service file only, binary skipped
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

# --- Which user owns the service ------------------------------------------
# The service must live in the session that owns the PipeWire socket. With
# sudo that is SUDO_USER; otherwise it is the current user. Root with no
# SUDO_USER means plain `su` / a root shell — refuse, since root has no audio
# session and a root-owned service would be useless.
SERVICE_USER="${SUDO_USER:-$(id -un)}"
if [ "$(id -u)" -eq 0 ] && [ "$SERVICE_USER" = "root" ]; then
	die "installing a user service for root makes no sense (no PipeWire session).
Run it as the audio user via sudo instead:
    sudo -u <user> make install        # from the repo root
or log in as that user and run 'make install' (binary install is then skipped)."
fi
if [ -n "${SUDO_USER:-}" ]; then
	SERVICE_HOME="$(getent passwd "$SERVICE_USER" | cut -d: -f6)"
else
	# Honor $HOME when run directly so a sandboxed/test HOME is respected.
	SERVICE_HOME="$HOME"
fi
[ -n "$SERVICE_HOME" ] || die "cannot determine home directory for user '$SERVICE_USER'"

# --- Detect init system ----------------------------------------------------
INIT=""
if [ -d /run/systemd/system ]; then
	INIT="systemd"
elif command -v rc-update >/dev/null 2>&1; then
	INIT="openrc"
else
	warn "no supported init system detected; the binary will be installed, but no service will be set up"
fi
log "init system: ${INIT:-none} (service user: $SERVICE_USER)"

# --- Build -----------------------------------------------------------------
command -v go >/dev/null 2>&1 || die "go toolchain not found in PATH"
log "building $BINARY"
# PipeWire is cgo, so this is not a static build; it needs
# libpipewire-0.3-dev (Debian) / pipewire-devel (Fedora) / pipewire (Arch).
( cd "$REPO_ROOT" && go build -buildvcs=false -o "$SCRIPT_DIR/$BINARY" "./cmd/server" )

# --- Binary (root only, or when $BINDIR is writable) -----------------------
if [ "$(id -u)" -eq 0 ] || { mkdir -p "$BINDIR" 2>/dev/null && [ -w "$BINDIR" ]; }; then
	log "installing binary to $BINDIR"
	install -d "$BINDIR"
	install -m 0755 "$SCRIPT_DIR/$BINARY" "$BINDIR/$BINARY"
else
	warn "not root and $BINDIR not writable: skipping binary install (service expects $BINDIR/$BINARY)"
	warn "re-run with 'sudo make install' to install the binary, or edit the service file's ExecStart"
fi
rm -f "$SCRIPT_DIR/$BINARY"

# Only chown service files when installing for a different user (via sudo).
INSTALL_OWNER=()
if [ "$(id -un)" != "$SERVICE_USER" ]; then
	INSTALL_OWNER=(-o "$SERVICE_USER" -g "$SERVICE_USER")
fi

# --- Service file -----------------------------------------------------------
# run_as_service_user <cmd...>: run a command as the service user with a
# user-session environment (XDG_RUNTIME_DIR, and a user D-Bus for systemd).
run_as_service_user() {
	local uid
	uid="$(id -u "$SERVICE_USER")"
	if [ "$(id -un)" = "$SERVICE_USER" ]; then
		XDG_RUNTIME_DIR="${XDG_RUNTIME_DIR:-/run/user/$uid}" "$@"
	else
		sudo -u "$SERVICE_USER" \
			HOME="$SERVICE_HOME" \
			XDG_RUNTIME_DIR="/run/user/$uid" \
			DBUS_SESSION_BUS_ADDRESS="unix:path=/run/user/$uid/bus" \
			"$@"
	fi
}

case "$INIT" in
systemd)
	UNITDIR="$SERVICE_HOME/.config/systemd/user"
	log "installing systemd user unit to $UNITDIR"
	install -d "${INSTALL_OWNER[@]}" -m 0755 "$UNITDIR"
	install "${INSTALL_OWNER[@]}" -m 0644 \
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
	# OpenRC user services live under XDG_CONFIG_HOME/rc (i.e. ~/.config/rc),
	# NOT XDG_DATA_HOME. rc-update/rc-service --user only see them there.
	INITDIR="$SERVICE_HOME/.config/rc/init.d"
	log "installing OpenRC user service to $INITDIR"
	install -d "${INSTALL_OWNER[@]}" -m 0755 "$INITDIR"
	install "${INSTALL_OWNER[@]}" -m 0755 \
		"$SCRIPT_DIR/openrc/$BINARY" "$INITDIR/$BINARY"
	if run_as_service_user rc-update add --user "$BINARY" default; then
		log "added service to the user default runlevel"
	else
		warn "could not add to the user runlevel automatically; add it as '$SERVICE_USER' with:"
		warn "    rc-update --user add $BINARY default"
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
