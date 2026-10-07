"""Source-bound offline Tier 1 process benchmark; no provider or verification run.

Every Python sample starts in isolated mode and includes the pinned source-only
oracle's import and provenance checks. Build time is excluded. First invocations
and subsequent invocations are reported separately; neither means a cold OS
cache. This harness deliberately does not report a language speed ratio.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import platform
import random
import shutil
import statistics
import struct
import subprocess
import sys
import tempfile
import time
from datetime import UTC, datetime
from pathlib import Path
from types import ModuleType

REQUEST = {
    "version": 1,
    "framework": "playwright",
    "failureType": "locator_not_found",
    "errorMessage": "TimeoutError: waiting for locator('#save')",
    "failedSelector": "#save",
    "testCode": "await page.locator('#save').click();\n",
    "pageSnapshot": '<button id="save-new">Save</button>',
}


def sha256(data: bytes) -> str:
    return hashlib.sha256(data).hexdigest()


def file_digest(path: Path) -> str:
    return sha256(path.read_bytes())


def command(command: list[str], *, cwd: Path | None = None, env=None) -> bytes:
    result = subprocess.run(command, cwd=cwd, env=env, capture_output=True, timeout=120)
    if result.returncode:
        # Diagnostics cannot echo arbitrary subprocess output or configuration.
        raise RuntimeError(f"{Path(command[0]).name} exited {result.returncode}")
    return result.stdout


def git(repo: Path, *arguments: str) -> bytes:
    return command(["git", "-C", str(repo), *arguments])


def source_manifest(repo: Path) -> dict[str, str | None]:
    """Bind all Go files, including tests/untracked files, and module inputs."""
    paths = sorted(p for p in repo.rglob("*.go") if p.is_file() and ".git" not in p.relative_to(repo).parts)
    manifest = {p.relative_to(repo).as_posix(): file_digest(p) for p in paths}
    for name in ("go.mod", "go.sum"):
        path = repo / name
        manifest[name] = file_digest(path) if path.exists() else None
    return manifest


def patch_identity(repo: Path) -> dict:
    """Hash exact staged/unstaged binary patches and untracked file contents."""
    if Path(git(repo, "rev-parse", "--show-toplevel").decode().strip()).resolve() != repo:
        raise RuntimeError("Go repository must be its exact Git root")
    tracked = git(repo, "diff", "--binary", "--no-ext-diff", "--no-textconv", "HEAD", "--")
    index = git(repo, "diff", "--cached", "--binary", "--no-ext-diff", "--no-textconv", "HEAD", "--")
    unstaged = git(repo, "diff", "--binary", "--no-ext-diff", "--no-textconv", "--")
    untracked = {}
    for name in sorted(n for n in git(repo, "ls-files", "--others", "--exclude-standard", "-z").split(b"\0") if n):
        relative = os.fsdecode(name)
        path = repo / relative
        content = os.fsencode(os.readlink(path)) if path.is_symlink() else path.read_bytes()
        untracked[relative] = {"sha256": sha256(content), "mode": path.lstat().st_mode}
    status = git(repo, "status", "--porcelain=v1", "--untracked-files=all", "-z")
    identity = {
        "head": git(repo, "rev-parse", "HEAD").decode().strip(),
        "tracked_patch_sha256": sha256(tracked),
        "index_patch_sha256": sha256(index),
        "unstaged_patch_sha256": sha256(unstaged),
        "status_sha256": sha256(status),
        "untracked": untracked,
    }
    identity["exact_patch_and_untracked_sha256"] = sha256(json.dumps(identity, sort_keys=True).encode())
    return identity


def upstream_manifest(source: Path, oracle, version: str) -> dict:
    oracle.verify_checkout(source.parent, oracle.PINS[version])
    if source.name != "src":
        raise RuntimeError("Python source must be the checkout src directory")
    paths = git(source.parent, "ls-files", "-z", "--", "src").split(b"\0")
    manifest = {os.fsdecode(p): file_digest(source.parent / os.fsdecode(p)) for p in sorted(paths) if p}
    return {"path": str(source), "revision": oracle.PINS[version], "tracked_source_manifest": manifest}


def executable_architecture(path: Path) -> str:
    """Check the actual release executable, independently of compiler host."""
    data = path.read_bytes()[:64]
    if data[:4] in (b"\xcf\xfa\xed\xfe", b"\xfe\xed\xfa\xcf"):
        endian = "<" if data[:4] == b"\xcf\xfa\xed\xfe" else ">"
        return {0x0100000C: "arm64", 0x01000007: "amd64"}.get(struct.unpack(endian + "I", data[4:8])[0], "unknown")
    if data[:4] == b"\x7fELF":
        endian = "<" if data[5] == 1 else ">"
        return {183: "arm64", 62: "amd64"}.get(struct.unpack(endian + "H", data[18:20])[0], "unknown")
    raise RuntimeError("benchmark supports thin Mach-O or ELF release binaries")


def unchanged(before, after, label):
    if before != after:
        raise RuntimeError(f"{label} changed during benchmark; discard measurements")


def summarize(samples: list[dict]) -> dict:
    warm = [sample["elapsed_ms"] for sample in samples[1:]]
    return {
        "first_invocation_ms": samples[0]["elapsed_ms"],
        "subsequent_invocations": {
            "count": len(warm),
            "median_ms": statistics.median(warm),
            "min_ms": min(warm),
            "max_ms": max(warm),
        },
    }


def sample(command_line, payload, environment, repo):
    started = time.perf_counter()
    result = subprocess.run(
        command_line, input=json.dumps(payload), cwd=repo, env=environment, capture_output=True, text=True, timeout=30
    )
    elapsed = (time.perf_counter() - started) * 1000
    if result.returncode:
        raise RuntimeError(f"fixture command exited {result.returncode}; discard measurements")
    return {"elapsed_ms": elapsed, "raw_stdout": result.stdout, "response": json.loads(result.stdout)}


def validate_response(name, response, source, oracle):
    if name == "go_native":
        proposal = response
        if (
            proposal.get("decision") != "propose"
            or proposal.get("apply") is not False
            or proposal.get("requiresApproval") is not True
        ):
            raise RuntimeError("native fixture proposal governance diverged")
        if proposal.get("metadata", {}).get("provenance") != "unverified":
            raise RuntimeError("native fixture provenance diverged")
    else:
        version = name.removeprefix("python_")
        proof = response["provenance"]
        if proof["version"] != version or proof["revision"] != oracle.PINS[version] or proof["sourceOnly"] is not True:
            raise RuntimeError("Python oracle provenance diverged")
        if proof["packageVersion"] != {"0.1.3": "0.1.0", "0.2.1": "0.2.1"}[version] or len(proof["modules"]) < 4:
            raise RuntimeError("Python oracle runtime identity diverged")
        for name, module in proof["modules"].items():
            path = (source / module["path"]).resolve()
            if (
                not (name == "ninelives" or name.startswith("ninelives."))
                or not path.is_relative_to(source / "ninelives")
                or path.suffix != ".py"
            ):
                raise RuntimeError("Python oracle owned module identity diverged")
            if module["sha256"] != file_digest(path):
                raise RuntimeError("Python oracle imported source digest diverged")
        if len(response["results"]) != 1:
            raise RuntimeError("Python oracle fixture count diverged")
        proposal = response["results"][0]
        if proposal["success"] is not True:
            raise RuntimeError("Python fixture did not propose")
    code = proposal.get("proposedCode", proposal.get("code", ""))
    if code != "await page.locator('#save-new').click();\n" or proposal.get("confidence") != 0.85:
        raise RuntimeError("fixture code or confidence diverged")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--repo", type=Path, default=Path(__file__).resolve().parents[1])
    parser.add_argument("--python", default=sys.executable, help="native Python executable or architecture wrapper")
    parser.add_argument("--go", default=shutil.which("go"), help="Go compiler executable")
    parser.add_argument(
        "--upstream-0.1.3", type=Path, required=True, dest="upstream_013", help="exact pinned checkout src directory"
    )
    parser.add_argument(
        "--upstream-0.2.1", type=Path, required=True, dest="upstream_021", help="exact pinned checkout src directory"
    )
    parser.add_argument(
        "--output-prefix", type=Path, required=True, help="new output path prefix for .json and .md; never overwritten"
    )
    parser.add_argument("--samples", type=int, default=30, help="invocations per engine, including first; at least 2")
    parser.add_argument("--seed", type=int, default=2151, help="seed for interleaved engine ordering")
    parser.add_argument(
        "--target-arch",
        choices=("arm64", "amd64"),
        default={"arm64": "arm64", "aarch64": "arm64", "x86_64": "amd64"}.get(platform.machine()),
    )
    args = parser.parse_args()
    if args.samples < 2 or not args.go or not args.target_arch:
        parser.error("requires at least two samples, a Go compiler and a supported target architecture")
    repo = args.repo.resolve()
    helper = repo / "internal/contracttest/python_oracle.py"
    harness = repo / "scripts/benchmark_tier1.py"
    invocation = Path(__file__).resolve()
    sources = {"python_0.1.3": args.upstream_013.resolve(), "python_0.2.1": args.upstream_021.resolve()}
    output = args.output_prefix.resolve()
    outputs = [Path(str(output) + suffix) for suffix in (".json", ".md")]
    if any(p.exists() for p in outputs):
        raise RuntimeError("benchmark output already exists; choose a fresh prefix")
    # Only these non-sensitive process keys are inherited. Isolated mode ignores
    # Python environment hooks; source-only protection also rejects valid caches.
    environment = {key: os.environ[key] for key in ("PATH", "LANG", "LC_ALL", "HOME", "TMPDIR") if key in os.environ}
    runtime = json.loads(
        command(
            [
                args.python,
                "-I",
                "-c",
                "import json,platform,sys; print(json.dumps(dict(machine=platform.machine(),version=platform.python_version(),executable=sys.executable)))",
            ],
            env=environment,
        )
    )
    runtime_arch = {"arm64": "arm64", "aarch64": "arm64", "x86_64": "amd64"}.get(runtime["machine"])
    if runtime_arch != args.target_arch:
        raise RuntimeError("Python runtime must use the release binary target architecture")
    # Compile the helper itself from source. Do not import it via a bytecode cache
    # and do not install owned-package protection in this coordinator process.
    oracle = ModuleType("tier1_oracle")
    exec(compile(helper.read_bytes(), str(helper), "exec"), oracle.__dict__)
    baseline = {
        "go_sources": source_manifest(repo),
        "go_patch": patch_identity(repo),
        "python_sources": {
            name: upstream_manifest(source, oracle, name.removeprefix("python_")) for name, source in sources.items()
        },
        "durable_harness_sha256": file_digest(harness),
        "invoked_harness_sha256": file_digest(invocation),
        "oracle_helper_sha256": file_digest(helper),
    }

    def check_sources():
        current = {
            "go_sources": source_manifest(repo),
            "go_patch": patch_identity(repo),
            "python_sources": {
                name: upstream_manifest(source, oracle, name.removeprefix("python_")) for name, source in sources.items()
            },
            "durable_harness_sha256": file_digest(harness),
            "invoked_harness_sha256": file_digest(invocation),
            "oracle_helper_sha256": file_digest(helper),
        }
        unchanged(baseline, current, "source, harness, helper or exact patch identity")
        return current

    with tempfile.TemporaryDirectory(prefix="tier1-source-bound-") as temporary:
        binary = Path(temporary) / "9l"
        build_command = [args.go, "build", "-trimpath", "-ldflags=-s -w", "-o", str(binary), "./cmd/9l"]
        build_environment = dict(environment, GOARCH=args.target_arch, CGO_ENABLED="0", GOWORK="off", GOENV="off")
        command(build_command, cwd=repo, env=build_environment)
        check_sources()
        if executable_architecture(binary) != args.target_arch:
            raise RuntimeError("release executable architecture differs from requested target")
        binary_hash = file_digest(binary)
        commands = {
            name: [args.python, "-I", str(helper), str(source), name.removeprefix("python_")]
            for name, source in sources.items()
        }
        commands["go_native"] = [str(binary), "tier1", "--format", "json"]
        samples = {name: [] for name in commands}
        order = []
        rng = random.Random(args.seed)
        started_utc = datetime.now(UTC).isoformat()
        for round_number in range(args.samples):
            names = list(commands)
            rng.shuffle(names)
            order.append(names)
            for position, name in enumerate(names):
                check_sources()
                unchanged(binary_hash, file_digest(binary), "release binary")
                result = sample(commands[name], REQUEST if name == "go_native" else [REQUEST], environment, repo)
                validate_response(name, result["response"], sources.get(name), oracle)
                result.update(round=round_number, position=position)
                samples[name].append(result)
        after = check_sources()
        unchanged(binary_hash, file_digest(binary), "release binary")
        result = {
            "scope": "One moved-ID offline proposal per fresh process. Python includes isolated startup, imports, source-only compilation and pinned provenance bootstrap/checks; Go includes startup and native proposal. Build and coordinator identity checks excluded. No browser, provider, verification or language ratio.",
            "first_invocation_scope": "First recorded invocation per engine; OS/filesystem cache state is uncontrolled. Subsequent samples also use fresh processes.",
            "started_utc": started_utc,
            "completed_utc": datetime.now(UTC).isoformat(),
            "sample_count_per_engine": args.samples,
            "seed": args.seed,
            "order": order,
            "request": REQUEST,
            "platform": {
                "os": platform.system(),
                "coordinator_machine": platform.machine(),
                "python_runtime": runtime,
                "go_compiler": command([args.go, "version"], env=environment).decode().strip(),
                "target_arch": args.target_arch,
                "binary_arch": executable_architecture(binary),
            },
            "build": {
                "command": build_command,
                "GOARCH": args.target_arch,
                "CGO_ENABLED": "0",
                "GOWORK": "off",
                "GOENV": "off",
                "metadata": command([args.go, "version", "-m", str(binary)], env=environment).decode(),
            },
            "commands": commands,
            "samples": samples,
            "summary": {name: summarize(values) for name, values in samples.items()},
            "provenance_before": baseline,
            "provenance_after": after,
            "release_binary_sha256_before": binary_hash,
            "release_binary_sha256_after": file_digest(binary),
        }
    lines = [
        "# Offline Tier 1 process benchmark",
        "",
        result["scope"],
        "",
        result["first_invocation_scope"],
        "",
        f"{args.samples} invocations per engine; seeded interleaving ({args.seed}).",
        "",
        "| Engine | First ms | Subsequent median ms | Min ms | Max ms |",
        "|---|---:|---:|---:|---:|",
    ]
    for name, row in result["summary"].items():
        warm = row["subsequent_invocations"]
        lines.append(
            f"| {name} | {row['first_invocation_ms']:.3f} | {warm['median_ms']:.3f} | {warm['min_ms']:.3f} | {warm['max_ms']:.3f} |"
        )
    lines.extend(
        [
            "",
            "The adjacent JSON contains each raw fixture response, order, timing, actual imported-module digests, all Go source inputs, exact patch identity, compiler/target/binary architecture and matching pre/post provenance.",
        ]
    )
    output.parent.mkdir(parents=True, exist_ok=True)
    with outputs[0].open("x") as stream:
        stream.write(json.dumps(result, indent=2) + "\n")
    with outputs[1].open("x") as stream:
        stream.write("\n".join(lines) + "\n")


if __name__ == "__main__":
    main()
