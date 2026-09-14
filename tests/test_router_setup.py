import contextlib
import io
import json
import os
import pathlib
import sys
import tempfile
import unittest
from unittest.mock import Mock, patch

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[1] / 'scripts'))
from router_trial import main, private_write, settings_insertion, stop_owned_bridge
from install_router_autostart import unit_quote


class SettingsTests(unittest.TestCase):
    def test_preserves_comments_strings_and_trailing_commas(self):
        original = '// { a comment\n{\n "url": "https://example.com/{x}",\n "list": [1, /* } */],\n}\n'
        first, insertion = settings_insertion(original, 'http://127.0.0.1:20129/example')
        self.assertEqual(first, original.index('{\n'))
        changed = original[:first + 1] + insertion + original[first + 1:]
        self.assertEqual(changed.replace(insertion, '', 1), original)
        self.assertTrue(insertion.endswith(','))
        with self.assertRaisesRegex(RuntimeError, 'existing Cloud Code override'):
            settings_insertion(changed, 'unused')

    def test_empty_object_including_comments_needs_no_comma(self):
        for original in ('{}', '\ufeff{/* comment */}', '// comment\n{\n}'):
            first, insertion = settings_insertion(original, 'example')
            self.assertFalse(insertion.endswith(','))
            changed = original[:first + 1] + insertion + original[first + 1:]
            self.assertEqual(changed.replace(insertion, '', 1), original)

    def test_quoted_comment_markers_and_escaped_quotes(self):
        original = json.dumps({'text': '/* // \\" { }', 'nested': {'value': '},'}})
        _, insertion = settings_insertion(original, 'example')
        self.assertTrue(insertion.endswith(','))

    def test_existing_override_rejected_even_when_key_is_escaped(self):
        original = '{"jetski.cloudCodeU\\u0072l": "existing"}'
        with self.assertRaisesRegex(RuntimeError, 'existing Cloud Code override'):
            settings_insertion(original, 'example')

    def test_invalid_or_non_object_settings_rejected(self):
        for original in ('[]', '{broken}', '{/* unfinished', '{"a":1} {}'):
            with self.assertRaises((RuntimeError, ValueError)):
                settings_insertion(original, 'example')

    def test_private_atomic_write(self):
        with tempfile.TemporaryDirectory() as directory:
            path = pathlib.Path(directory) / 'settings.json'
            path.write_text('before')
            private_write(path, 'Türkçe kullanıcı ayarı')
            self.assertEqual(path.read_bytes(), 'Türkçe kullanıcı ayarı'.encode('utf-8'))
            self.assertEqual(list(path.parent.iterdir()), [path])
            if os.name != 'nt':
                self.assertEqual(path.stat().st_mode & 0o777, 0o600)

    def test_non_linux_does_not_signal_unverified_pid(self):
        with patch('router_trial.sys.platform', 'darwin'), patch('router_trial.os.kill') as kill:
            with self.assertRaisesRegex(RuntimeError, 'no process was signalled'):
                stop_owned_bridge({'pid': 123, 'binary': 'synthetic'})
            kill.assert_not_called()

    def test_service_path_quoting_and_invalid_newline(self):
        self.assertEqual(unit_quote('a % "b"'), '"a %% \\"b\\""')
        with self.assertRaises(RuntimeError):
            unit_quote('a\nb')


class ControllerTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = pathlib.Path(self.temp.name)
        self.settings = self.root / 'settings.json'
        self.original = '// preserved comment\n{"editor.fontSize": 14}\n'
        self.settings.write_text(self.original)
        self.binary = self.root / 'agrouter'
        self.binary.write_text('synthetic executable placeholder')
        self.state_dir = self.root / 'runtime'
        self.argv = ['router_trial.py', 'setup', '--router', 'https://router.example.com',
                     '--wire-format', 'openai', '--bridge', str(self.binary),
                     '--settings', str(self.settings), '--state-dir', str(self.state_dir)]

    def test_fresh_setup_with_hidden_key_and_mock_services(self):
        child = Mock(pid=123)
        child.poll.return_value = None
        opener = Mock()
        opener.open.return_value = contextlib.closing(io.BytesIO(b'{"data": []}'))
        with patch('sys.argv', self.argv), patch('router_trial.getpass.getpass', return_value='synthetic'), \
             patch('router_trial.urllib.request.build_opener', return_value=opener), \
             patch('router_trial.urllib.request.urlopen', return_value=contextlib.closing(io.BytesIO(b'{}'))), \
             patch('router_trial.subprocess.Popen', return_value=child) as launch, \
             contextlib.redirect_stdout(io.StringIO()):
            main()
        state = json.loads((self.state_dir / 'state.json').read_text())
        self.assertEqual(state['router'], 'https://router.example.com')
        self.assertEqual(state['wireFormat'], 'openai')
        self.assertEqual((self.state_dir / 'api-key').read_text(), 'synthetic\n')
        self.assertEqual((self.state_dir / 'settings.before.jsonc').read_text(), self.original)
        self.assertEqual(self.settings.read_text().replace(state['insertion'], '', 1), self.original)
        self.assertNotIn('synthetic', json.dumps(state))
        self.assertNotIn('synthetic', str(launch.call_args.args))
        self.assertEqual(launch.call_args.kwargs['env']['AG_ROUTER_API_KEY'], 'synthetic')
        request = opener.open.call_args.args[0]
        self.assertEqual(request.full_url, 'https://router.example.com/v1/models')

    def test_failed_preflight_does_not_launch_or_change_settings(self):
        opener = Mock()
        opener.open.side_effect = OSError('synthetic connection failure')
        with patch('sys.argv', self.argv), patch('router_trial.getpass.getpass', return_value='synthetic'), \
             patch('router_trial.urllib.request.build_opener', return_value=opener), \
             patch('router_trial.subprocess.Popen') as launch:
            with self.assertRaisesRegex(OSError, 'connection failure'):
                main()
        launch.assert_not_called()
        self.assertEqual(self.settings.read_text(), self.original)
        self.assertFalse((self.state_dir / 'state.json').exists())

    def test_stop_restores_only_inserted_setting_preserving_new_edits(self):
        self.state_dir.mkdir()
        first, insertion = settings_insertion(self.original, 'example')
        changed = self.original[:first + 1] + insertion + self.original[first + 1:] + '// later edit\n'
        self.settings.write_text(changed)
        state = dict(settings=str(self.settings), insertion=insertion, pid=123, binary=str(self.binary))
        (self.state_dir / 'state.json').write_text(json.dumps(state))
        argv = ['router_trial.py', 'stop', '--state-dir', str(self.state_dir)]
        with patch('sys.argv', argv), patch('router_trial.sys.platform', 'linux'), \
             patch('router_trial.stop_owned_bridge') as stop, contextlib.redirect_stdout(io.StringIO()):
            main()
        stop.assert_called_once_with(state)
        self.assertEqual(self.settings.read_text(), self.original + '// later edit\n')
        self.assertFalse((self.state_dir / 'state.json').exists())


if __name__ == '__main__':
    unittest.main()
