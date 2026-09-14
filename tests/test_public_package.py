import pathlib
import sys
import tempfile
import unittest
import zipfile

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[1] / 'scripts'))
from check_public import audit
from package_source import package


class PublicPackageTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = pathlib.Path(self.temp.name)
        (self.root / 'README.md').write_text('Synthetic project\n')

    def test_private_runtime_is_excluded_and_zip_is_reproducible(self):
        private = self.root / '.local'
        private.mkdir()
        (private / 'api-key').write_text('sk-' + 'x' * 32)
        target, count, digest = package(self.root)
        with zipfile.ZipFile(target) as archive:
            self.assertEqual(archive.namelist(), ['README.md'])
        self.assertEqual(count, 1)
        self.assertEqual(package(self.root)[2], digest)

    def test_secret_rejected_without_echoing_value(self):
        key = 'sk-' + 'x' * 32
        (self.root / 'README.md').write_text(key)
        with self.assertRaises(ValueError) as caught:
            audit(self.root)
        self.assertIn('README.md:1: API key', str(caught.exception))
        self.assertNotIn(key, str(caught.exception))
        self.assertFalse((self.root / 'dist').exists())

    def test_unknown_root_file_rejected(self):
        (self.root / 'api-key').write_text('private')
        with self.assertRaisesRegex(ValueError, 'unexpected root entry'):
            audit(self.root)

    def test_database_in_source_rejected(self):
        (self.root / 'docs').mkdir()
        (self.root / 'docs/private.sqlite').write_bytes(b'database')
        with self.assertRaisesRegex(ValueError, 'unexpected file type'):
            audit(self.root)

    def test_public_symlink_rejected(self):
        (self.root / 'docs').mkdir()
        try:
            (self.root / 'docs/leak.md').symlink_to(self.root / 'README.md')
        except OSError:
            self.skipTest('Symlink creation requires OS permission')
        with self.assertRaisesRegex(ValueError, 'symlink in public source'):
            audit(self.root)


if __name__ == '__main__':
    unittest.main()
