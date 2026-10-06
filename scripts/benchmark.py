"""Reproduce local CLI process costs without browsers, models, or providers."""

import argparse
import json
import platform
import statistics
import subprocess
import tempfile
import time
from pathlib import Path


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("repo", type=Path, nargs="?", default=Path.cwd())
    parser.add_argument("--samples", type=int, default=30)
    parser.add_argument("--run-samples", type=int, default=5)
    args = parser.parse_args()
    assert args.samples > 0 and args.run_samples > 0
    repo = args.repo.resolve()
    subprocess.run(
        ["npm", "ci", "--prefix", "testdata/playwright", "--ignore-scripts", "--no-audit", "--no-fund"],
        cwd=repo,
        check=True,
        capture_output=True,
    )
    toolchain = subprocess.check_output(["go", "env", "GOOS", "GOARCH", "GOVERSION"], cwd=repo, text=True).splitlines()
    with tempfile.TemporaryDirectory(prefix="9l-bench-") as temporary:
        root = Path(temporary)
        binary = root / "9l"
        subprocess.run(
            ["go", "build", "-trimpath", "-ldflags=-s -w", "-o", str(binary), "./cmd/9l"],
            cwd=repo,
            check=True,
            capture_output=True,
        )

        def call(*arguments):
            started = time.perf_counter()
            result = subprocess.run(
                [str(binary), *arguments], cwd=repo, check=True, capture_output=True, text=True, timeout=30
            )
            return (time.perf_counter() - started) * 1000, result.stdout

        startup_first, _ = call("version")
        startup_warm = [call("version")[0] for _ in range(args.samples)]
        plan_args = ("plan", "testdata/playwright/tests/pass.spec.ts", "--format", "json")
        plan_first, plan_raw = call(*plan_args)
        assert len(json.loads(plan_raw)["jobs"]) == 1
        planning_warm = [call(*plan_args)[0] for _ in range(args.samples)]
        final_times, first_evidence = [], []
        for sample in range(args.run_samples):
            receipts = root / f"receipts-{sample}"
            started = time.perf_counter()
            active = subprocess.Popen(
                [
                    str(binary),
                    "run",
                    "testdata/playwright/tests/pass.spec.ts",
                    "--format",
                    "json",
                    "--receipt-dir",
                    str(receipts),
                ],
                cwd=repo,
                stdout=subprocess.PIPE,
                stderr=subprocess.PIPE,
                text=True,
            )
            evidence_time = None
            try:
                while active.poll() is None:
                    if evidence_time is None and any(receipts.glob("*/*/*/stdout.log")):
                        evidence_time = (time.perf_counter() - started) * 1000
                    if time.perf_counter() - started > 30:
                        raise TimeoutError("local fixture exceeded 30 seconds")
                    time.sleep(0.001)
                stdout, _ = active.communicate(timeout=5)
            finally:
                if active.poll() is None:
                    active.terminate()
                    try:
                        active.communicate(timeout=5)
                    except subprocess.TimeoutExpired:
                        active.kill()
                        active.communicate(timeout=5)
            finished = (time.perf_counter() - started) * 1000
            assert active.returncode == 0 and json.loads(stdout)["complete"]
            assert any(receipts.glob("*/*/*/stdout.log"))
            final_times.append(finished)
            first_evidence.append(evidence_time if evidence_time is not None else finished)

        receipts = root / "cancel"
        active = subprocess.Popen(
            [
                str(binary),
                "run",
                "testdata/playwright/tests/slow.spec.ts",
                "--format",
                "json",
                "--receipt-dir",
                str(receipts),
                "--timeout",
                "60s",
            ],
            cwd=repo,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
        )
        deadline = time.monotonic() + 15
        try:
            run_id = None
            while time.monotonic() < deadline:
                for events in receipts.glob("*/events.jsonl"):
                    if '"attempt_started"' in events.read_text():
                        run_id = events.parent.name
                        break
                if run_id:
                    break
                time.sleep(0.005)
            assert run_id, "active attempt was not persisted"
            started = time.perf_counter()
            call("cancel", run_id, "--receipt-dir", str(receipts))
            stdout, _ = active.communicate(timeout=10)
            cancel_ms = (time.perf_counter() - started) * 1000
            summary = json.loads(stdout)
            assert active.returncode != 0 and not summary["complete"]
            assert any(r["status"] == "canceled" for r in summary["receipts"])
        finally:
            if active.poll() is None:
                active.terminate()
                try:
                    active.communicate(timeout=5)
                except subprocess.TimeoutExpired:
                    active.kill()
                    active.communicate(timeout=5)
        print(
            json.dumps(
                {
                    "platform": {"os": platform.system(), "machine": platform.machine(), "go": toolchain},
                    "samples": args.samples,
                    "run_samples": args.run_samples,
                    "startup_first_process_ms": startup_first,
                    "startup_warm_median_ms": statistics.median(startup_warm),
                    "plan_first_process_ms": plan_first,
                    "plan_warm_median_ms": statistics.median(planning_warm),
                    "fixture_first_run_ms": final_times[0],
                    "fixture_warm_median_ms": statistics.median(final_times[1:] or final_times),
                    "first_persisted_output_first_ms": first_evidence[0],
                    "first_persisted_output_warm_median_ms": statistics.median(first_evidence[1:] or first_evidence),
                    "cancel_ms": cancel_ms,
                    "measurement": "first process after build; no OS cache purge; 1ms evidence polling; Playwright browser-free assertion; build/install excluded; no model or provider work",
                },
                indent=2,
            )
        )


if __name__ == "__main__":
    main()
