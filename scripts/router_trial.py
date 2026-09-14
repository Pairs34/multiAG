#!/usr/bin/env python3
"""Configure and manage the extension-free Antigravity router bridge."""
import argparse
import getpass
import json
import os
import pathlib
import secrets
import shutil
import signal
import sqlite3
import subprocess
import sys
import tempfile
import time
import urllib.request
import urllib.parse
import webbrowser


def default_settings():
    if sys.platform == 'win32':
        root = pathlib.Path(os.environ['APPDATA'])
    elif sys.platform == 'darwin':
        root = pathlib.Path.home() / 'Library/Application Support'
    else:
        root = pathlib.Path(os.environ.get('XDG_CONFIG_HOME', pathlib.Path.home() / '.config'))
    for name in ('Antigravity IDE', 'Antigravity'):
        settings = root / name / 'User/settings.json'
        if settings.exists():
            return settings
    return root / 'Antigravity/User/settings.json'


def settings_insertion(original, endpoint):
    """Validate JSONC before inserting an override, preserving original text."""
    cleaned = list(original)
    i = 0
    quoted = False
    while i < len(original):
        ch = original[i]
        if quoted:
            if ch == '\\':
                i += 2
                continue
            if ch == '"':
                quoted = False
        elif ch == '"':
            quoted = True
        elif original.startswith('//', i):
            end = original.find('\n', i)
            end = len(original) if end < 0 else end
            cleaned[i:end] = ' ' * (end - i)
            i = end
            continue
        elif original.startswith('/*', i):
            end = original.find('*/', i + 2)
            if end < 0:
                raise RuntimeError('Unterminated JSONC comment; no settings changed.')
            end += 2
            for pos in range(i, end):
                if cleaned[pos] not in '\r\n':
                    cleaned[pos] = ' '
            i = end
            continue
        i += 1
    text = ''.join(cleaned)
    quoted = False
    i = 0
    while i < len(text):
        ch = text[i]
        if quoted:
            if ch == '\\':
                i += 2
                continue
            if ch == '"':
                quoted = False
        elif ch == '"':
            quoted = True
        elif ch == ',':
            end = i + 1
            while end < len(text) and text[end].isspace():
                end += 1
            if end < len(text) and text[end] in '}]':
                cleaned[i] = ' '
        i += 1
    value = json.loads(''.join(cleaned).lstrip('\ufeff'))
    if not isinstance(value, dict):
        raise RuntimeError('Settings must be a JSON object; no settings changed.')
    if 'jetski.cloudCodeUrl' in value:
        raise RuntimeError('An existing Cloud Code override was found; no settings changed.')
    first = text.find('{')
    insertion = '\n  "jetski.cloudCodeUrl": ' + json.dumps(endpoint) + (',' if value else '')
    return first, insertion


def private_write(path, data):
    fd, name = tempfile.mkstemp(prefix=path.name + '.multiag-', dir=str(path.parent))
    try:
        with os.fdopen(fd, 'w', encoding='utf-8', newline='') as stream:
            stream.write(data)
            stream.flush()
            os.fsync(stream.fileno())
        os.replace(name, path)
    finally:
        pathlib.Path(name).unlink(missing_ok=True)


def replace_settings(path, data):
    private_write(path, data)


def stop_owned_bridge(state):
    if not sys.platform.startswith('linux'):
        raise RuntimeError('Stop the bridge using its console on this platform; no process was signalled.')
    actual = pathlib.Path('/proc') / str(state['pid']) / 'exe'
    if not actual.exists():
        return
    if actual.resolve() != pathlib.Path(state['binary']).resolve():
        raise RuntimeError('Bridge PID belongs to another executable; no process was signalled.')
    os.kill(state['pid'], signal.SIGTERM)
    for _ in range(50):
        if not actual.exists():
            return
        time.sleep(0.1)
    raise RuntimeError('Bridge did not stop gracefully.')


MANAGED_UNIT = 'multiag-router.service'


def wait_ready(endpoint):
    for _ in range(100):
        try:
            with urllib.request.urlopen(endpoint + '/health', timeout=0.3):
                return
        except OSError:
            time.sleep(0.1)
    raise RuntimeError('Bridge did not become ready; check the service status.')


def serve_from_state(state_dir):
    """Exec the bridge in the foreground so systemd owns its actual PID."""
    statefile = state_dir / 'state.json'
    state = json.loads(statefile.read_text(encoding='utf-8'))
    binary = pathlib.Path(state['binary']).resolve(strict=True)
    keyfile = pathlib.Path(state['apiKeyFile']).resolve(strict=True)
    if keyfile.stat().st_mode & 0o077:
        raise RuntimeError('Service API key file must be private (chmod 600).')
    api_key = keyfile.read_text(encoding='utf-8').strip()
    if not api_key or '\n' in api_key or '\r' in api_key:
        raise RuntimeError('Invalid service API key.')
    url = urllib.parse.urlsplit(state['endpoint'])
    if url.scheme != 'http' or url.hostname != '127.0.0.1' or url.port != 20129:
        raise RuntimeError('Invalid local service endpoint.')
    capability = url.path.lstrip('/')
    if len(capability) < 32 or '/' in capability:
        raise RuntimeError('Invalid service capability.')
    env = os.environ.copy()
    env['AG_ROUTER_API_KEY'] = api_key
    env['AG_ROUTER_CAPABILITY'] = capability
    state['pid'] = os.getpid()
    private_write(statefile, json.dumps(state))
    command = [str(binary), '--router', state['router'], '--model', state.get('model', ''),
               '--wire-format', state.get('wireFormat', 'native')]
    os.execve(str(binary), command, env)


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('action', choices=['setup', 'start', 'status', 'stop', 'restart', 'panel', 'serve'])
    p.add_argument('--bridge')
    p.add_argument('--db', type=pathlib.Path, default=pathlib.Path.home() / '.9router/db/data.sqlite')
    p.add_argument('--router', help='9Router base URL; remembered on restart')
    p.add_argument('--api-key-file', type=pathlib.Path, help='private API key file; never passed on the command line')
    p.add_argument('--wire-format', choices=['native','openai'], help='router API compatibility mode; remembered on restart')
    p.add_argument('--settings', type=pathlib.Path, default=default_settings())
    p.add_argument('--state-dir', type=pathlib.Path, default=pathlib.Path(__file__).resolve().parent.parent / '.local/router-trial')
    p.add_argument('--model', default='')
    p.add_argument('--open-ide', action='store_true')
    p.add_argument('--ide', default=shutil.which('antigravity-ide') or shutil.which('antigravity') or 'antigravity')
    p.add_argument('--debug-port', type=int, default=0)
    a = p.parse_args()
    a.state_dir = a.state_dir.resolve()
    if a.action == 'serve':
        serve_from_state(a.state_dir)
        return
    show_panel = a.action == 'panel'
    statefile = a.state_dir / 'state.json'
    if a.action == 'setup':
        if statefile.exists():
            raise RuntimeError('Already configured; use panel, status or restart.')
        if not a.api_key_file:
            a.state_dir.mkdir(mode=0o700, parents=True, exist_ok=True)
            a.api_key_file = a.state_dir / 'api-key'
            if a.api_key_file.exists():
                raise RuntimeError('A key file already exists; pass --api-key-file explicitly to reuse it.')
            api_key = getpass.getpass('9Router API key (hidden): ').strip()
            if not api_key or '\n' in api_key or '\r' in api_key:
                raise RuntimeError('Invalid API key.')
            private_write(a.api_key_file, api_key + '\n')
        a.action = 'start'
    previous = None
    if a.action != 'start':
        state = json.loads(statefile.read_text(encoding='utf-8'))
        if a.action == 'panel':
            try:
                with urllib.request.urlopen(state['endpoint'] + '/health', timeout=3):
                    pass
                webbrowser.open(state['endpoint'] + '/manage')
                print('Router control panel opened.')
                return
            except OSError:
                if state.get('managedUnit') == MANAGED_UNIT:
                    subprocess.run(['systemctl', '--user', 'start', MANAGED_UNIT], check=True)
                    wait_ready(state['endpoint'])
                    webbrowser.open(state['endpoint'] + '/manage')
                    print('Router control panel opened.')
                    return
                a.action = 'restart'
                a.bridge = state['binary']
        if a.action == 'status':
            with urllib.request.urlopen(state['endpoint'] + '/health', timeout=3) as response:
                print(response.read().decode())
            return
        if a.action == 'restart':
            try:
                with urllib.request.urlopen(state['endpoint'] + '/health', timeout=3) as response:
                    state['model'] = json.load(response)['model']
            except OSError:
                pass
            previous = state
            a.settings = pathlib.Path(state['settings'])
            a.model = state['model']
        else:
            settings = pathlib.Path(state['settings'])
            current = settings.read_text(encoding='utf-8')
            insertion = state['insertion']
            if insertion not in current:
                raise RuntimeError('Endpoint setting changed; refusing to overwrite your settings. Bridge kept running.')
            if state.get('managedUnit') == MANAGED_UNIT:
                subprocess.run(['systemctl', '--user', 'disable', '--now', MANAGED_UNIT], check=True)
            elif sys.platform.startswith('linux'):
                stop_owned_bridge(state)
            else:
                print('Settings restored. Close the bridge process using its console on this platform.')
            replace_settings(settings, current.replace(insertion, '', 1))
            statefile.unlink()
            print('Settings restored. Reload the IDE window to use Google directly again.')
            return
    if statefile.exists() and previous is None:
        raise RuntimeError('Bridge already configured; use status or stop first.')
    suffix = '.exe' if os.name == 'nt' else ''
    default_binary = pathlib.Path(__file__).resolve().parent.parent / 'bin' / ('agrouter' + suffix)
    binary = pathlib.Path(a.bridge or (previous or {}).get('binary') or default_binary).resolve(strict=True)
    original = a.settings.read_text(encoding='utf-8')
    a.state_dir.mkdir(mode=0o700, parents=True, exist_ok=True)
    capability = previous['endpoint'].rsplit('/',1)[-1] if previous else secrets.token_hex(24)
    endpoint = 'http://127.0.0.1:20129/' + capability
    first, insertion = (0, previous['insertion']) if previous else settings_insertion(original, endpoint)
    router = (a.router or (previous or {}).get('router') or 'http://127.0.0.1:20128').rstrip('/')
    wire_format = a.wire_format or (previous or {}).get('wireFormat') or 'native'
    parsed = urllib.parse.urlsplit(router)
    if parsed.scheme not in ('http', 'https') or not parsed.hostname or parsed.username or parsed.password or parsed.query or parsed.fragment:
        raise RuntimeError('Invalid router URL; existing bridge kept running.')
    local_router = parsed.hostname in ('localhost', '127.0.0.1', '::1')
    if not local_router and parsed.scheme != 'https':
        raise RuntimeError('Cloud routers require HTTPS; existing bridge kept running.')
    keyfile = a.api_key_file or ((previous or {}).get('apiKeyFile') and pathlib.Path(previous['apiKeyFile']))
    if keyfile:
        keyfile = keyfile.resolve(strict=True)
        if os.name != 'nt' and keyfile.stat().st_mode & 0o077:
            raise RuntimeError('API key file must be private (chmod 600); existing bridge kept running.')
        api_key = keyfile.read_text(encoding='utf-8').strip()
    elif os.environ.get('AG_ROUTER_API_KEY'):
        api_key = os.environ['AG_ROUTER_API_KEY']
    elif local_router:
        db = sqlite3.connect('file:' + str(a.db.resolve()) + '?mode=ro', uri=True)
        row = db.execute('SELECT key FROM apiKeys WHERE isActive=1 LIMIT 1').fetchone()
        db.close()
        api_key = row[0] if row else ''
    else:
        raise RuntimeError('Cloud router needs --api-key-file or AG_ROUTER_API_KEY; existing bridge kept running.')
    if not api_key or '\n' in api_key or '\r' in api_key:
        raise RuntimeError('Invalid API key; existing bridge kept running.')
    # Check a new destination before stopping the working bridge. Never follow
    # redirects while carrying the Authorization header.
    if a.router or a.api_key_file:
        class NoRedirect(urllib.request.HTTPRedirectHandler):
            def redirect_request(self, *args, **kwargs):
                return None
        request = urllib.request.Request(router + '/v1/models', headers={'Authorization': 'Bearer ' + api_key})
        with urllib.request.build_opener(NoRedirect()).open(request, timeout=30) as response:
            model_list = json.load(response)
            if not isinstance(model_list.get('data'), list):
                raise RuntimeError('Invalid cloud model list; existing bridge kept running.')
    env = os.environ.copy()
    env['AG_ROUTER_API_KEY'] = api_key
    env['AG_ROUTER_CAPABILITY'] = capability
    if previous and previous.get('managedUnit') == MANAGED_UNIT:
        state = dict(previous, binary=str(binary), model=a.model, router=router,
                     apiKeyFile=str(keyfile) if keyfile else None, wireFormat=wire_format)
        private_write(statefile, json.dumps(state))
        subprocess.run(['systemctl', '--user', 'restart', MANAGED_UNIT], check=True)
        wait_ready(endpoint)
        print('Managed router service restarted.')
        if show_panel:
            webbrowser.open(endpoint + '/manage')
        return
    if previous:
        stop_owned_bridge(previous)
    if previous is None:
        private_write(a.state_dir / 'settings.before.jsonc', original)
    logfd = os.open(str(a.state_dir / 'bridge.log'), os.O_WRONLY | os.O_CREAT | os.O_APPEND, 0o600)
    with os.fdopen(logfd, 'a') as log:
        child = subprocess.Popen([str(binary), '--router', router, '--model', a.model, '--wire-format', wire_format], env=env, stdin=subprocess.DEVNULL,
                                 stdout=log, stderr=log, start_new_session=True)
    try:
        for _ in range(30):
            if child.poll() is not None:
                raise RuntimeError('Bridge exited; see private bridge.log.')
            try:
                with urllib.request.urlopen(endpoint + '/health', timeout=0.3):
                    break
            except OSError:
                time.sleep(0.1)
        else:
            raise RuntimeError('Bridge did not become ready.')
        state = dict(endpoint=endpoint, insertion=insertion, settings=str(a.settings.resolve()),
                     binary=str(binary), pid=child.pid, model=a.model, router=router,
                     apiKeyFile=str(keyfile) if keyfile else None, wireFormat=wire_format)
        private_write(statefile, json.dumps(state))
        if previous is None:
            replace_settings(a.settings, original[:first + 1] + insertion + original[first + 1:])
    except BaseException:
        child.terminate()
        if statefile.exists() and previous is None:
            statefile.unlink()
        raise
    print('Bridge ready; IDE settings backed up. Model:', a.model or 'IDE selection')
    if show_panel:
        webbrowser.open(endpoint + '/manage')
        print('Router control panel opened.')
    if a.open_ide:
        command = [a.ide, '--new-window']
        if a.debug_port:
            command.append('--remote-debugging-port=' + str(a.debug_port))
        with open(a.state_dir / 'ide-launch.log', 'a') as log:
            subprocess.Popen(command, stdin=subprocess.DEVNULL, stdout=log, stderr=log, start_new_session=True)
        print('Antigravity IDE launched. Existing account and workspaces are preserved.')


if __name__ == '__main__':
    try:
        main()
    except Exception as error:
        print('Router setup error:', error, file=sys.stderr)
        sys.exit(1)
