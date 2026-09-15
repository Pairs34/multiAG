# multiAG

An experimental, extension-free bridge connecting Antigravity IDE agent requests to your own [9Router](https://github.com/decolua/9router) instance. A local browser panel lets you inspect the connection, override the model, or switch routing back to Google.

[Türkçe](README.tr.md) · [Setup and recovery](docs/setup.md) · [Architecture and limitations](docs/architecture.md)

The IDE selects the model; 9Router selects an account from its configured pool. Changing the Google profile in the IDE does **not** select a matching router account. Pool rotation and account management happen in 9Router.

## Quick start

Requires Go 1.23 or newer, Python 3.10 or newer, Antigravity IDE, and a working 9Router API key. Go and Python code use only standard libraries. Start the IDE once and ensure its user `settings.json` exists before setup.

Build on Linux or macOS:

```sh
go build -trimpath -buildvcs=false -o bin/agrouter ./cmd/agrouter
python3 scripts/router_trial.py setup --router https://router.example.com --wire-format openai
```

On Windows, the recommended PowerShell setup is:

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\setup-windows.ps1 -Router "https://router.example.com"
```

The script builds the Windows executable, prompts for the API key without echoing it, and launches the bridge without a visible console. To run the steps manually:

```powershell
go build -trimpath -buildvcs=false -o bin/agrouter.exe ./cmd/agrouter
python scripts/router_trial.py setup --router https://router.example.com --wire-format openai
```

Replace the example URL with your router's base URL, without `/v1`. Enter the API key at the hidden prompt. Remote routers require HTTPS; local routers may use HTTP. Pass `--settings "/path/to/User/settings.json"` if automatic detection selects the wrong installation.

Reload the IDE window after the first setup. Open the panel:

```sh
python3 scripts/router_trial.py panel
```

Use `python` instead of `python3` on Windows. Linux and macOS also provide `sh router-panel.sh`. Leave the model override empty to follow the IDE's model selection. Changes in the panel apply to subsequent requests without restarting the IDE. The panel links to your router dashboard for account management. Windows supports `status`, `restart`, and `stop`; before stopping a recorded PID, the controller verifies its executable path and process start identity.

## Optional Linux autostart

After setup, install the bridge as a user systemd service:

```sh
python3 scripts/install_router_autostart.py
systemctl --user status multiag-router.service
```

This copies the application and private configuration out of the source directory. It starts with the user session and restarts after a process failure. Windows and macOS autostart installers are not provided.

## Restore direct Google routing

```sh
python3 scripts/router_trial.py stop
```

Reload the IDE window afterward. The controller removes its inserted setting while preserving other edits. For a managed Linux installation, it also disables and stops the service. On Windows, stop also terminates the verified bridge process. On macOS, the bridge process must still be closed separately; see [recovery](docs/setup.md).

## Compatibility and data flow

Linux was tested with a real Antigravity IDE 1.107.0 session, streamed responses, and a tool round trip. Windows was tested natively for build and the `setup`, `status`, `restart`, and `stop` process lifecycle; a real Windows IDE request remains unverified. macOS remains a cross-compilation target. The IDE setting is undocumented and may change between releases.

Agent prompts, relevant project context, and tool results travel to your router and its provider. IDE OAuth credentials are kept on the Google metadata route and are not forwarded to 9Router. Login, quota metadata, and tab completion continue to use the IDE account. Consequently, the IDE quota display does not represent the router pool.

`openai` mode translates Cloud Code messages to chat completions. `native` mode preserves Cloud Code payloads for compatible routers. File-URI media, native built-in tools, and some advanced tool constraints are unsupported; see [limitations](docs/architecture.md). This project does not create Google accounts or provide a router subscription.

## Development and public source package

```sh
go test ./...
go vet ./...
python3 -m unittest discover -s tests -v
python3 scripts/check_public.py
python3 scripts/package_source.py
```

The source packager checks an explicit allowlist and writes `dist/multiAG-source.zip`. Runtime data, keys, local archives, binaries, and IDE settings are excluded. GitHub CI is configured for Linux, Windows, and macOS.

MIT licensed. Independent community project; not affiliated with Google. See [LICENSE](LICENSE) and [NOTICE](NOTICE).
