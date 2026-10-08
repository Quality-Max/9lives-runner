"""Validate the four CLI archives before any release upload."""

import hashlib
from pathlib import Path
import sys
import tarfile


def check(directory: Path) -> None:
    root = Path(__file__).resolve().parent.parent
    expected = sorted(f"9l-{os}-{arch}.tar.gz" for os in ("darwin", "linux") for arch in ("amd64", "arm64"))
    assert sorted(p.name for p in directory.iterdir()) == expected, "Unexpected or missing release artifacts"
    checksums = []
    for name in expected:
        archive = directory / name
        bundle = name.removesuffix(".tar.gz")
        with tarfile.open(archive, "r:gz") as tar:
            members = tar.getmembers()
            assert len(members) == 4, "Unexpected archive member count"
            by_name = {member.name.rstrip("/"): member for member in members}
            assert set(by_name) == {bundle, *(f"{bundle}/{file}" for file in ("9l", "LICENSE", "NOTICE"))}, "Unexpected archive layout"
            assert by_name[bundle].isdir(), "Missing bundle directory"
            for file in ("9l", "LICENSE", "NOTICE"):
                member = by_name[f"{bundle}/{file}"]
                assert member.isfile() and member.size > 0, "Invalid archive file"
                if file == "9l":
                    assert member.mode & 0o111, "Executable permission missing"
                else:
                    assert tar.extractfile(member).read() == (root / file).read_bytes(), "Legal file mismatch"
        checksums.append(f"{hashlib.sha256(archive.read_bytes()).hexdigest()}  {name}\n")
    (directory / "SHA256SUMS").write_text("".join(checksums))
    print("Four CLI archives, executable permissions, legal files and SHA-256 checksums verified")


if __name__ == "__main__":
    try:
        check(Path(sys.argv[1]))
    except (AssertionError, OSError, ValueError, tarfile.TarError, IndexError):
        sys.exit("CLI release archive validation failed")
