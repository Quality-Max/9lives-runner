"""Release admission must reject missing, foreign or unsafe Windows payloads."""
import contextlib
import importlib.util
import io
from pathlib import Path
import stat
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
                files = {binary: b'fixture executable', 'LICENSE': (root / 'LICENSE').read_bytes(), 'NOTICE': (root / 'NOTICE').read_bytes()}
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


if __name__ == '__main__':
    unittest.main()
