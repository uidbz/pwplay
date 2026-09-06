# Running pwplay-server as a service

`pwplay-server` exposes the player over HTTP on `127.0.0.1:8080` and plays
through PipeWire. Because PipeWire runs inside the user's graphical/audio
session, pwplay-server is installed as a **per-user service** — a system-wide
service running as a dedicated user usually cannot reach the audio socket.

## Quick install (systemd or OpenRC)

From a checkout of this repo, as the user whose session owns the audio:

```
sudo make install       # binary + service (uses SUDO_USER for the service)
# or, without root (service file only):
make install
```

The installer:

- builds `pwplay-server` (needs the Go toolchain and PipeWire dev libs) and
  installs it to `/usr/local/bin` (root only),
- detects systemd or OpenRC and installs the matching **user** service file:
  - systemd: `~/.config/systemd/user/pwplay-server.service`
  - OpenRC:  `~/.config/rc/init.d/pwplay-server`
- enables the service (`systemctl --user enable` / `rc-update --user add`).

Run it via `sudo`, not plain `su`: the service must belong to the user who owns
the PipeWire session, so the script targets `SUDO_USER` and refuses to install a
service for root. Without root it skips the `/usr/local/bin` binary (edit
`ExecStart` if you keep the binary elsewhere).

The service starts pwplay-server with **no arguments** — an empty queue driven
entirely over HTTP (`POST /add`, `/play`, …), which is how
[tie-audio](https://github.com/uidbz/tie-gui) controls it.

## Starting

```
# systemd
systemctl --user start pwplay-server
systemctl --user status pwplay-server

# start at boot without logging in:
sudo loginctl enable-linger $USER

# OpenRC (user runlevel)
rc-service --user pwplay-server start
```

## Logging

- **systemd**: stderr goes to the journal — `journalctl --user -u pwplay-server`.
- **OpenRC**: stderr goes to
  `${XDG_STATE_HOME:-~/.local/state}/pwplay/pwplay-server.log`.

## Layout

| Path                                        | Purpose                 |
|---------------------------------------------|-------------------------|
| `/usr/local/bin/pwplay-server`              | binary                  |
| `~/.config/systemd/user/pwplay-server.service` | systemd user unit    |
| `~/.config/rc/init.d/pwplay-server`         | OpenRC user service     |
| `~/.local/state/pwplay/`                    | OpenRC log              |

## Files

- `systemd/pwplay-server.service` — systemd user unit
- `openrc/pwplay-server` — OpenRC user service script
- `install.sh` — the installer
