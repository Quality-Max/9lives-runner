"""Release admission must reject missing, foreign or unsafe Windows payloads."""
import contextlib
import hashlib
import importlib.util
import io
import os
from pathlib import Path
import shutil
import stat
import subprocess
import tarfile
import tempfile
import unittest
import zipfile

root = Path(__file__).resolve().parent.parent
spec = importlib.util.spec_from_file_location('archives', root / 'scripts/check-cli-archives.py')
archives = importlib.util.module_from_spec(spec)
spec.loader.exec_module(archives)


class ArchiveAdmission(unittest.TestCase):
    def bundles(self, directory, zip_override=None):
        for system in ('darwin', 'linux', 'windows'):
            for arch in ('amd64', 'arm64'):
                bundle = f'9l-{system}-{arch}'
                binary = '9l.exe' if system == 'windows' else '9l'
                files = {binary: b'fixture executable', 'LICENSE': (root / 'LICENSE').read_bytes(), 'NOTICE': (root / 'NOTICE').read_bytes(), 'THIRD-PARTY-NOTICES.txt': (root / 'THIRD-PARTY-NOTICES.txt').read_bytes()}
                if system == 'windows':
                    with zipfile.ZipFile(directory / (bundle + '.zip'), 'w') as zip:
                        zip.writestr(bundle + '/', b'')
                        for name, content in files.items():
                            member = zipfile.ZipInfo(bundle + '/' + name)
                            member.external_attr = (stat.S_IFREG | 0o644) << 16
                            if arch == 'arm64' and name == binary and zip_override:
                                member, content = zip_override(member, content)
                            zip.writestr(member, content)
                else:
                    with tarfile.open(directory / (bundle + '.tar.gz'), 'w:gz') as tar:
                        folder = tarfile.TarInfo(bundle)
                        folder.type = tarfile.DIRTYPE
                        tar.addfile(folder)
                        for name, content in files.items():
                            member = tarfile.TarInfo(bundle + '/' + name)
                            member.mode = 0o755 if name == binary else 0o644
                            member.size = len(content)
                            tar.addfile(member, io.BytesIO(content))

    def test_six_platform_archives_admitted_with_checksums(self):
        with tempfile.TemporaryDirectory() as temporary:
            directory = Path(temporary)
            self.bundles(directory)
            with contextlib.redirect_stdout(io.StringIO()):
                archives.check(directory)
            lines = (directory / 'SHA256SUMS').read_text().splitlines()
            self.assertEqual(len(lines), 6)
            self.assertTrue(any(line.endswith('9l-windows-arm64.zip') for line in lines))

    def test_missing_and_foreign_artifacts_rejected(self):
        for mutation in ('missing', 'foreign'):
            with self.subTest(mutation=mutation), tempfile.TemporaryDirectory() as temporary:
                directory = Path(temporary)
                self.bundles(directory)
                if mutation == 'missing':
                    (directory / '9l-windows-arm64.zip').unlink()
                else:
                    (directory / '9l-windows-x86.zip').write_bytes(b'foreign')
                with self.assertRaises(AssertionError):
                    archives.check(directory)
                self.assertFalse((directory / 'SHA256SUMS').exists())

    def test_windows_unsafe_members_rejected(self):
        def mutate(kind):
            def override(member, content):
                if kind == 'traversal': member.filename = '../9l.exe'
                if kind == 'symlink': member.external_attr = (stat.S_IFLNK | 0o777) << 16
                if kind == 'empty': content = b''
                if kind == 'duplicate': member.filename = member.filename.replace('9l.exe', 'NOTICE')
                return member, content
            return override
        for kind in ('traversal', 'symlink', 'empty', 'duplicate'):
            with self.subTest(kind=kind), tempfile.TemporaryDirectory() as temporary:
                directory = Path(temporary)
                self.bundles(directory, mutate(kind))
                with self.assertRaises(AssertionError): archives.check(directory)
                self.assertFalse((directory / 'SHA256SUMS').exists())


def documented(language, first, last):
    """Lines of the install guide's code block from `first` up to `last`."""
    text = (root / 'docs/install.md').read_text()
    blocks = [part.split('```', 1)[0].splitlines() for part in text.split('```' + language + '\n')[1:]]
    block = next(block for block in blocks if any(line.startswith(first) for line in block))
    start = next(i for i, line in enumerate(block) if line.startswith(first))
    end = next(i for i, line in enumerate(block) if line.startswith(last))
    return block[start:end]


class DocumentedChecksumSelection(unittest.TestCase):
    """The install guide's own commands must admit exactly one well-formed line."""

    def cases(self, archive):
        digest = hashlib.sha256(b'fixture archive').hexdigest()
        line = f'{digest}  {archive}'
        other = f'{"0" * 64}  9l-other.tar.gz'
        return {
            'exact line among others': (f'{other}\n{line}\n', True),
            'trailing content': (f'{line} extra\n', False),
            'trailing whitespace': (f'{line} \n', False),
            'duplicate entry': (f'{line}\n{line}\n', False),
            'CRLF line endings': (f'{other}\r\n{line}\r\n', False),
            'uppercase digest': (f'{digest.upper()}  {archive}\n', False),
            'wrong digest': (f'{"0" * 64}  {archive}\n', False),
            'only another archive': (f'{other}\n', False),
        }

    def check(self, command, script_name, script, archive):
        for name, (sums, accepted) in self.cases(archive).items():
            with self.subTest(case=name), tempfile.TemporaryDirectory() as temporary:
                directory = Path(temporary)
                (directory / archive).write_bytes(b'fixture archive')
                (directory / 'SHA256SUMS').write_bytes(sums.encode())
                (directory / script_name).write_text(script)
                result = subprocess.run([*command, str(directory / script_name)], cwd=directory, capture_output=True, timeout=120)
                self.assertEqual(result.returncode == 0, accepted, result.stderr.decode(errors='replace'))

    @unittest.skipIf(os.name == 'nt' or not shutil.which('sh') or not shutil.which('shasum'), 'needs sh and shasum')
    def test_shell_selection(self):
        lines = documented('sh', 'grep -Ex', 'tar -xzf')
        script = 'set -e\nBUNDLE=9l-darwin-arm64\n' + '\n'.join(lines) + '\n'
        self.check(['sh'], 'select.sh', script, '9l-darwin-arm64.tar.gz')

    @unittest.skipIf(not (shutil.which('pwsh') or shutil.which('powershell')), 'needs PowerShell')
    def test_powershell_selection(self):
        lines = documented('powershell', '$Lines =', 'Expand-Archive')
        script = "$ErrorActionPreference = 'Stop'\n$Bundle = '9l-windows-amd64'\n" + '\n'.join(lines) + '\n'
        shell = shutil.which('pwsh') or shutil.which('powershell')
        self.check([shell, '-NoProfile', '-NonInteractive', '-File'], 'select.ps1', script, '9l-windows-amd64.zip')


if __name__ == '__main__':
    unittest.main()
