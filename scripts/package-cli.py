"""Build and exercise a native CLI, then archive it with its legal files."""
import os
from pathlib import Path
import platform
import shutil
import subprocess
import tarfile
import zipfile

root = Path(__file__).resolve().parent.parent
system = {'Windows': 'windows', 'Darwin': 'darwin', 'Linux': 'linux'}[platform.system()]
arch = {'AMD64': 'amd64', 'x86_64': 'amd64', 'ARM64': 'arm64', 'aarch64': 'arm64', 'arm64': 'arm64'}[platform.machine()]
# Do not label a cross-build as a native qualification.
assert os.environ['GOOS'] == system and os.environ['GOARCH'] == arch
host = subprocess.check_output(['go', 'env', 'GOHOSTOS', 'GOHOSTARCH'], cwd=root, text=True).splitlines()
assert host == [system, arch], 'Native Go toolchain architecture required'
version = os.environ['RELEASE_VERSION']
bundle = f'9l-{system}-{arch}'
destination = root / 'dist' / bundle
destination.mkdir(parents=True, exist_ok=True)
binary = destination / ('9l.exe' if system == 'windows' else '9l')
subprocess.run(['go', 'build', '-trimpath', f'-ldflags=-s -w -X main.version={version}', '-o', str(binary), './cmd/9l'], cwd=root, check=True)
assert subprocess.check_output([str(binary), '--version'], cwd=root, text=True).strip() == f'9l {version} (Go runner)'
subprocess.run([str(binary), '--help'], cwd=root, stdout=subprocess.DEVNULL, check=True)
subprocess.run([str(binary), 'plan', 'testdata/sdk/tests/checkout.spec.ts', '--format', 'json'], cwd=root, stdout=subprocess.DEVNULL, check=True)
for name in ('LICENSE', 'NOTICE'):
    shutil.copyfile(root / name, destination / name)
if system == 'windows':
    with zipfile.ZipFile(root / f'{bundle}.zip', 'w', compression=zipfile.ZIP_DEFLATED) as archive:
        archive.write(destination, bundle + '/')
        for name in ('9l.exe', 'LICENSE', 'NOTICE'):
            archive.write(destination / name, f'{bundle}/{name}')
else:
    with tarfile.open(root / f'{bundle}.tar.gz', 'w:gz') as archive:
        archive.add(destination, arcname=bundle)
print(f'Native {system}/{arch} CLI built, exercised and packaged')
