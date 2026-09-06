# Running pwplay-server as a service

`pwplay-server` exposes the player over HTTP on `127.0.0.1:8080` and plays
through PipeWire. Because PipeWire runs inside the user's graphical/audio
session, pwplay-server is installed as a **per-user service** — a system-wide
service running as a dedicated user usually cannot reach the audio socket.

## Quick install (systemd or OpenRC)

From a checkout of this repo, as the user whose session owns the audio:

```
sudo make install     # from the repo root
# or equivalently:
sudo ./contrib/install.sh
```

The installer:

- builds `pwplay-server` (needs the Go toolchain and PipeWire dev libs) and
  installs it to `/usr/local/bin`,
- detects systemd or OpenRC and installs the matching **user** service file:
  - systemd: `~/.config/systemd/user/pwplay-server.service`
  - OpenRC:  `~/.local/share/rc/init.d/pwplay-server`
- enables the service (`systemctl --user enable` / `rc-update --user add`).

When run under `sudo` it acts on `SUDO_USER`, not root. Without root it still
installs the service file but skips the `/usr/local/bin` binary (edit
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
| `~/.local/share/rc/init.d/pwplay-server`    | OpenRC user service     |
| `~/.local/state/pwplay/`                    | OpenRC log              |

## Files

- `systemd/pwplay-server.service` — systemd user unit
- `openrc/pwplay-server` — OpenRC user service script
- `install.sh` — the installer
