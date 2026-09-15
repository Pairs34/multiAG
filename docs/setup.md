# Setup and recovery

## First configuration

Build the binary as described in the README. The setup script expects an existing IDE user settings file, accepts JSON with comments and trailing commas, and refuses to replace an existing `jetski.cloudCodeUrl` override. If settings are absent, use the IDE's “Open User Settings (JSON)” command and save an empty object first.

Automatic settings detection checks `Antigravity IDE` and `Antigravity` under:

| OS | Configuration root |
| --- | --- |
| Linux | `$XDG_CONFIG_HOME` or `~/.config` |
| macOS | `~/Library/Application Support` |
| Windows | `%APPDATA%` |

Use `--settings` for custom installations. Setup checks the router's `/v1/models` before launching the bridge or changing IDE settings. Use `--wire-format openai` for standard chat completions routers. Use `native` only if the router accepts unchanged Antigravity Cloud Code payloads; it is the low-level controller default.

For an existing private key file:

```sh
python3 scripts/router_trial.py setup --router https://router.example.com --wire-format openai --api-key-file /private/path/api-key
```

On POSIX systems, the key file must be accessible only to its owner (`chmod 600`). On Windows, store it in your own user directory and use Windows file permissions to restrict access. Keys are never passed as command-line arguments. If setup fails after the hidden prompt, the private key file may remain; explicitly reuse it with `--api-key-file` when retrying.

## Runtime files

Standalone state is stored in `.local/router-trial/`: `api-key`, `state.json`, `settings.before.jsonc`, and diagnostic logs. Never publish this directory. The capability URL in state and IDE settings grants access to the local bridge; keep it private too.

The bridge listens on `127.0.0.1:20129`. Port conflicts fail setup without replacing the IDE setting. Do not expose the listener through a tunnel or reverse proxy.

```sh
python3 scripts/router_trial.py status
python3 scripts/router_trial.py panel
```

The panel's model override and pause toggle are in memory. Restart preserves the current model override when the bridge is reachable; pause resets. Linux and Windows support `restart`. On Windows, the controller verifies the recorded executable path and process creation time before terminating a PID. Standalone restart on macOS is not automated because the controller deliberately does not signal an unverified PID.

## Linux user service

Install once after a successful setup. The installer refuses to overwrite an existing installation. It uses:

- `~/.local/lib/multiag-router/` for a copy of the binary and controller;
- `$XDG_CONFIG_HOME/multiag-router/` (or `~/.config/multiag-router/`) for private configuration;
- the user systemd unit `multiag-router.service`.

The source runtime directory becomes a symlink to persistent state; the original runtime is retained as `router-trial.before-autostart`. Application copies allow the source directory to be moved or removed. Start/stop/restart may be controlled through `systemctl --user`; account authentication remains in the IDE and router.

```sh
systemctl --user status multiag-router.service
journalctl --user -u multiag-router.service -n 50
```

Autostart begins with the user session. It does not configure boot-time login or system-wide services. Do not rerun the installer as an upgrade procedure.

## Restore or recover

Normally run the controller's `stop` action, then reload the IDE window. If the inserted setting has been edited externally, the controller refuses to overwrite it and leaves the bridge running.

For manual recovery, open IDE user settings and remove only the `jetski.cloudCodeUrl` property while keeping JSON syntax valid, then reload the window. Preserve your other settings. If a Linux service exists, disable and stop it using `systemctl --user disable --now multiag-router.service`.

On Windows, `stop` restores settings and terminates the bridge only after verifying the recorded executable path and process creation time. On macOS, close only the bridge process associated with this installation, using the PID recorded in private state **before** running stop and verifying its executable path in your OS process manager. Never stop processes by a broad application name. Afterward, fresh setup can be run again.

A model request that fails is not automatically retried through the IDE account. Inspect panel counters and private logs, verify the router model list, and check provider availability in the router dashboard. Do not attach raw state, keys, prompts, or logs to public issues.
