#!/usr/bin/env python3
"""Install a configured router bridge as a Linux user systemd service."""
import json
import argparse
import os
import pathlib
import shutil
import subprocess
import sys

from router_trial import MANAGED_UNIT, private_write, stop_owned_bridge, wait_ready


def unit_quote(value):
    text = str(value)
    if '\n' in text or '\r' in text:
        raise RuntimeError('Invalid path for service installation.')
    return '"' + text.replace('\\', '\\\\').replace('"', '\\"').replace('%', '%%') + '"'


def main():
    if not sys.platform.startswith('linux'):
        raise RuntimeError('This installer uses Linux user systemd.')
    project = pathlib.Path(__file__).resolve().parent.parent
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--binary', type=pathlib.Path, default=project / 'bin/agrouter')
    parser.add_argument('--state-dir', type=pathlib.Path, default=project / '.local/router-trial')
    args = parser.parse_args()
    source_dir = args.state_dir.resolve()
    if not (source_dir / 'state.json').exists():
        raise RuntimeError('Run router_trial.py setup before installing autostart.')
    state = json.loads((source_dir / 'state.json').read_text(encoding='utf-8'))
    config_root = pathlib.Path(os.environ.get('XDG_CONFIG_HOME', pathlib.Path.home() / '.config'))
    state_dir = config_root / 'multiag-router'
    app_dir = pathlib.Path.home() / '.local/lib/multiag-router'
    unit_dir = config_root / 'systemd/user'
    unit_file = unit_dir / MANAGED_UNIT
    backup_dir = source_dir.with_name('router-trial.before-autostart')
    if any(path.exists() for path in [state_dir, app_dir, unit_file, backup_dir]):
        raise RuntimeError('An installation or migration backup already exists; no files replaced.')
    binary = args.binary.resolve(strict=True)
    keyfile = pathlib.Path(state['apiKeyFile']).resolve(strict=True)
    if keyfile.stat().st_mode & 0o077:
        raise RuntimeError('Source API key is not private.')
    # Stage all service files before touching the currently running process.
    state_dir.mkdir(mode=0o700, parents=True)
    app_dir.mkdir(mode=0o700, parents=True)
    unit_dir.mkdir(parents=True, exist_ok=True)
    shutil.copyfile(binary, app_dir / 'agrouter')
    os.chmod(app_dir / 'agrouter', 0o700)
    shutil.copyfile(project / 'scripts/router_trial.py', app_dir / 'router_trial.py')
    os.chmod(app_dir / 'router_trial.py', 0o600)
    private_write(state_dir / 'api-key', keyfile.read_text(encoding='utf-8'))
    shutil.copyfile(source_dir / 'settings.before.jsonc', state_dir / 'settings.before.jsonc')
    os.chmod(state_dir / 'settings.before.jsonc', 0o600)
    installed = dict(state, binary=str(app_dir / 'agrouter'), apiKeyFile=str(state_dir / 'api-key'),
                     managedUnit=MANAGED_UNIT)
    private_write(state_dir / 'state.json', json.dumps(installed))
    unit = '\n'.join([
        '[Unit]', 'Description=Antigravity router bridge', 'After=network.target', '',
        '[Service]', 'Type=simple',
        'ExecStart=' + ' '.join(unit_quote(x) for x in [sys.executable, app_dir / 'router_trial.py', 'serve', '--state-dir', state_dir]),
        'Restart=always', 'RestartSec=2', 'TimeoutStopSec=20', 'UMask=0077',
        'NoNewPrivileges=true', 'Environment=PYTHONUNBUFFERED=1', '',
        '[Install]', 'WantedBy=default.target', '',
    ])
    private_write(unit_file, unit)
    subprocess.run(['systemd-analyze', '--user', 'verify', str(unit_file)], check=True)
    subprocess.run(['systemctl', '--user', 'daemon-reload'], check=True)
    stop_owned_bridge(state)
    source_dir.rename(backup_dir)
    source_dir.symlink_to(state_dir, target_is_directory=True)
    try:
        subprocess.run(['systemctl', '--user', 'enable', '--now', MANAGED_UNIT], check=True)
        wait_ready(installed['endpoint'])
    except BaseException:
        # Preserve staged files and migration backup for diagnosis; do not
        # overwrite the IDE settings or account data during recovery.
        print('Service installation needs attention; the original runtime backup is preserved.', file=sys.stderr)
        raise
    print('Router user service installed, enabled and ready.')
    print('Application:', app_dir)
    print('Private configuration:', state_dir)
    print('Service:', unit_file)


if __name__ == '__main__':
    try:
        main()
    except Exception as error:
        print('Autostart installation error:', error, file=sys.stderr)
        sys.exit(1)
