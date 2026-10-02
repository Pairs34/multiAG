# macOS autostart (LaunchAgent)

The project ships Linux (systemd) and Windows autostart installers only. On macOS the
bridge runs as a launchd user agent that starts at login and restarts on failure.

## Architecture

Setup stores private state under the repo `.local/router-trial/`. The macOS autostart
installer copies the binary, the controller, and the private configuration into a
persistent agent directory and points the LaunchAgent at that copy, so the source
directory may be moved or removed afterward.

| Item | Path |
| --- | --- |
| Agent directory | `~/Library/Application Support/multiag-router/` |
| LaunchAgent | `~/Library/LaunchAgents/com.multiag.router.plist` |
| Repo state (symlink) | `<repo>/.local/router-trial` → agent directory |
| Pre-autostart backup | `<repo>/.local/router-trial.before-autostart` |

## Commands

Read the bridge health and counters:

```sh
cd <repo>
python3 scripts/router_trial.py status
```

Open the control panel (model override, pause, router dashboard link):

```sh
python3 scripts/router_trial.py panel
```

Stop and restart the bridge (agent-managed, no manual signaling needed):

```sh
launchctl kickstart -k gui/$(id -u)/com.multiag.router        # restart
launchctl bootout   gui/$(id -u)/com.multiag.router          # stop
launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/com.multiag.router.plist   # start again
```

Remove autostart and restore direct Google routing (in this order):

```sh
launchctl bootout gui/$(id -u)/com.multiag.router
python3 scripts/router_trial.py stop
```

Then reload the IDE window. Unlike the Linux service, `stop` alone cannot stop the
bridge process here because macOS has no verified-PID signaling; the bootout above is
the macOS equivalent of disabling the service.

## Notes

- The agent uses `serve --state-dir`, which validates the API key (chmod 600) and the
  loopback capability endpoint, then execs the Go binary in the foreground so launchd
  owns the real PID (`KeepAlive`).
- Keep private logs: `~/Library/Application Support/multiag-router/launchd.{out,err}.log`.
- Do not expose the 127.0.0.1:20129 listener through a tunnel or reverse proxy.