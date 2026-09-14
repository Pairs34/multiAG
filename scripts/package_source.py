#!/usr/bin/env python3
"""Create a reproducible, audited public source ZIP; never follow symlinks."""
import hashlib
import pathlib
import sys
import zipfile

from check_public import ROOT, audit


def package(root=ROOT):
    files = audit(root)
    # Read everything before creating the output; audit failure leaves it intact.
    contents = [(path.relative_to(root).as_posix(), path.read_bytes()) for path in files]
    target = root / 'dist/multiAG-source.zip'
    target.parent.mkdir(exist_ok=True)
    temporary = target.with_suffix('.zip.tmp')
    try:
        with zipfile.ZipFile(temporary, 'w', compression=zipfile.ZIP_DEFLATED) as archive:
            for name, data in contents:
                info = zipfile.ZipInfo(name, date_time=(2026, 1, 1, 0, 0, 0))
                info.create_system = 3
                info.external_attr = (0o100755 if name.endswith('.sh') else 0o100644) << 16
                info.compress_type = zipfile.ZIP_DEFLATED
                archive.writestr(info, data)
        temporary.replace(target)
    finally:
        temporary.unlink(missing_ok=True)
    return target, len(files), hashlib.sha256(target.read_bytes()).hexdigest()


if __name__ == '__main__':
    try:
        path, count, digest = package()
        print(f'Created {path.relative_to(ROOT)} ({count} source files)\nSHA256: {digest}')
    except (OSError, UnicodeError, ValueError) as error:
        print(f'Packaging failed:\n{error}', file=sys.stderr)
        sys.exit(1)
