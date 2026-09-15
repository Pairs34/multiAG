#!/usr/bin/env python3
"""Audit the explicit public source set without printing matched secrets."""
import pathlib
import re
import sys

ROOT = pathlib.Path(__file__).resolve().parent.parent
ROOT_FILES = frozenset({
    'README.md', 'README.tr.md', 'LICENSE', 'NOTICE', 'CONTRIBUTING.md',
    'SECURITY.md', 'go.mod', '.gitignore', '.gitattributes', 'router-panel.sh',
})
SOURCE_DIRS = frozenset({'cmd', 'internal', 'scripts', 'tests', 'docs', '.github'})
PRIVATE_DIRS = frozenset({'.local', '.git', '.agents', '.codex', 'bin', 'dist', '__pycache__'})
SOURCE_SUFFIXES = frozenset({'.go', '.py', '.ps1', '.md', '.html', '.sh', '.yml', '.yaml'})
PATTERNS = (
    ('API key', re.compile(r'\bsk-[A-Za-z0-9_-]{16,}')),
    ('OAuth token', re.compile(r'\bya29\.[A-Za-z0-9_-]{16,}')),
    ('private key', re.compile(r'-----BEGIN (?:RSA |EC |OPENSSH )?PRIVATE KEY-----')),
    ('personal home path', re.compile(r'(?:/home/|/Users/)[A-Za-z0-9_.-]+/')),
    ('private deployment URL', re.compile(r'https?://[A-Za-z0-9.-]+\.up\.railway\.app')),
    ('email address', re.compile(r'\b[A-Za-z0-9_.+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}\b')),
    ('capability URL', re.compile(r'http://127\.0\.0\.1:20129/[a-f0-9]{32,}')),
)


def public_files(root=ROOT):
    """Return regular source files only; refuse unknown files and symlinks."""
    result = []
    for entry in sorted(root.iterdir()):
        if entry.name in PRIVATE_DIRS:
            continue
        if entry.is_symlink():
            raise ValueError(f'{entry.name}: symlink in public source')
        if entry.is_file() and entry.name in ROOT_FILES:
            result.append(entry)
        elif entry.is_dir() and entry.name in SOURCE_DIRS:
            for item in sorted(entry.rglob('*')):
                if any(part == '__pycache__' for part in item.relative_to(entry).parts):
                    continue
                if item.is_symlink():
                    raise ValueError(f'{item.relative_to(root)}: symlink in public source')
                if item.is_file():
                    if item.suffix not in SOURCE_SUFFIXES:
                        raise ValueError(f'{item.relative_to(root)}: unexpected file type')
                    result.append(item)
        else:
            raise ValueError(f'{entry.name}: unexpected root entry; review before publication')
    return sorted(result)


def audit(root=ROOT):
    files = public_files(root)
    findings = []
    for path in files:
        text = path.read_text(encoding='utf-8')
        for number, line in enumerate(text.splitlines(), 1):
            for kind, pattern in PATTERNS:
                if pattern.search(line):
                    findings.append(f'{path.relative_to(root)}:{number}: {kind}')
    if findings:
        raise ValueError('\n'.join(findings))
    return files


if __name__ == '__main__':
    try:
        files = audit()
        print(f'Public source check passed: {len(files)} files; private directories excluded.')
    except (OSError, UnicodeError, ValueError) as error:
        print(f'Public source check failed:\n{error}', file=sys.stderr)
        sys.exit(1)
