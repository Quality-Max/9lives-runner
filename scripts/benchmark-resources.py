"""Local, real-Chromium scaling/resource measurement; psutil 7.0.0 required.

Run on macOS with native arm64 Python and psutil==7.0.0; see docs/runner-efficiency.md.
No credentials, network app, database, build or install work is included in timings.
"""
import hashlib
import argparse
import json
import os
import platform
import resource
import signal
import subprocess
import time
from datetime import datetime, timezone
from pathlib import Path

import psutil

assert platform.system() == "Darwin", "This profiler is qualified on macOS only"
assert platform.machine() == "arm64", "Use native arm64 Python: psutil 7.0.0 x86_64 has macOS CPU-time bug #2411"

REPO = Path(__file__).resolve().parent.parent
parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--resume', type=Path)
args = parser.parse_args()
STAMP = datetime.now(timezone.utc).strftime("%Y%m%dT%H%M%SZ")
ROOT = args.resume.resolve() if args.resume else REPO / ".context" / f"efficiency-resources-{STAMP}"
ROOT.mkdir(exist_ok=bool(args.resume))
SAFE_ENV = {k: os.environ[k] for k in ("PATH", "HOME", "TMPDIR", "LANG", "LC_ALL", "PLAYWRIGHT_BROWSERS_PATH") if k in os.environ}
SAFE_ENV["GOARCH"] = "arm64"
BIN = ROOT / "9l"
INTERVAL = 0.1


def checked(cmd, cwd=REPO):
    result = subprocess.run(cmd, cwd=cwd, env=SAFE_ENV, capture_output=True, text=True, timeout=120)
    if result.returncode:
        raise RuntimeError(f"setup command failed: {cmd[0]} (exit {result.returncode}); output withheld")
    return result.stdout.strip()


checked(["go", "build", "-trimpath", "-ldflags=-s -w", "-o", str(BIN), "./cmd/9l"])
PW = REPO / "node_modules" / ".bin" / "playwright"
PROJECT = ROOT / "fixture"
PROJECT.mkdir(exist_ok=True)
if not (PROJECT / "node_modules").exists():
    (PROJECT / "node_modules").symlink_to(REPO / "node_modules", target_is_directory=True)
(PROJECT / "package.json").write_text(json.dumps({"private": True, "devDependencies": {"@playwright/test": "1.61.1"}}))
(PROJECT / "playwright.config.ts").write_text("""import {defineConfig} from '@playwright/test';
export default defineConfig({testDir: './tests', workers: 1, retries: 0, timeout: 15000,
  outputDir: './test-results/' + process.pid,
  use: {browserName: 'chromium', headless: true, trace: 'off', screenshot: 'off', video: 'off'}});
""")
TESTS = PROJECT / "tests"
TESTS.mkdir(exist_ok=True)


def spec_text(count):
    # Each case checks both rendered state and the actual in-page order mutation.
    body = "import {test, expect} from '@playwright/test';\n"
    for n in range(count):
        body += f"""
test('checkout {n}: exactly one order with selected SKU', async ({{page}}) => {{
  await page.setContent(`<button onclick="window.orders.push({{sku:'sku-{n}',quantity:1}});document.querySelector('[role=status]').textContent='Order confirmed'">Place order</button><p role="status">Cart ready</p><script>window.orders=[]</script>`);
  await page.getByRole('button', {{name: 'Place order'}}).click();
  await expect(page.getByRole('status')).toHaveText('Order confirmed');
  expect(await page.evaluate(() => (window as any).orders)).toEqual([{{sku:'sku-{n}',quantity:1}}]);
}});
"""
    return body


for n in range(100):
    (TESTS / f"checkout-{n:03d}.spec.ts").write_text(spec_text(5))
metadata = {
    "source_commit": checked(["git", "rev-parse", "HEAD"]),
    "tracked_worktree_clean": not checked(["git", "status", "--porcelain", "--untracked-files=no"]),
    "binary_sha256": hashlib.sha256(BIN.read_bytes()).hexdigest(),
    "machine": platform.machine(), "os": platform.platform(),
    "physical_cores": psutil.cpu_count(logical=False), "logical_cores": psutil.cpu_count(),
    "machine_ram_bytes": psutil.virtual_memory().total,
    "go": checked(["go", "env", "GOOS", "GOARCH", "GOVERSION"]).splitlines(),
    "node_version": checked(["node", "--version"]),
    "node_arch": checked(["node", "-p", "process.arch"]),
    "playwright_version": checked([str(PW), "--version"]),
    "psutil_version": psutil.__version__, "sample_interval_seconds": INTERVAL,
    "fixture_spec_sha256": hashlib.sha256(spec_text(5).encode()).hexdigest(),
    "cpu_units": "100% is one logical core; interval sample peaks, not instantaneous peaks",
    "ram_units": "summed RSS; shared pages can be counted more than once; sampled peaks can miss short transients",
    "cpu_total_method": "getrusage(RUSAGE_CHILDREN) delta; includes command and reaped descendants",
    "scope": "synthetic page.setContent checkout assertions in real Chromium; no app server, DB, providers, setup hooks or attachments; warm caches; build/install excluded; runs sequential on shared developer Mac",
}
results = {"metadata": metadata, "runs": [], "live_provider": {"measured": False, "reason": "No provider calls in this resource profile; credentials are not inspected"}}
if args.resume:
    results = json.loads((ROOT / 'results.json').read_text())
    assert results['metadata']['source_commit'] == metadata['source_commit']
    assert results['metadata']['binary_sha256'] == metadata['binary_sha256']
    results.setdefault('measurement_notes', []).append('Resumed prior measurement; unsuccessful case artifacts retained separately; successful evidence revalidated before inclusion.')


def save():
    (ROOT / "results.json").write_text(json.dumps(results, indent=2))


def measure(engine, files, workers, label, expected_per_spec=5):
    if any(r['label'] == label or r['label'].startswith(label + '-remeasured-') for r in results['runs']):
        return
    case = ROOT / label
    if case.exists():
        label += '-remeasured-' + STAMP
        case = ROOT / label
    case.mkdir()
    if engine == "9l":
        cmd = [str(BIN), "run", *map(str, files), "--workers", str(workers), "--attempts", "1", "--format", "json", "--receipt-dir", str(case / "receipts"), "--timeout", "120s", "--deadline", "5m"]
    else:
        cmd = [str(PW), "test", *[str(f.relative_to(PROJECT)) for f in files], "--workers", str(workers), "--reporter=json"]
    known = {}
    latest_cpu = {}
    samples = []
    cpu_before = resource.getrusage(resource.RUSAGE_CHILDREN)
    started = time.perf_counter()
    wall_started = time.time()
    with (case / "execution.json").open("w") as stdout, (case / "stderr.log").open("w") as stderr:
        active = subprocess.Popen(cmd, cwd=PROJECT, env=SAFE_ENV, stdout=stdout, stderr=stderr, start_new_session=True)
        root_process = psutil.Process(active.pid)
        previous_elapsed = 0.0
        previous_cpu = 0.0
        root_previous_cpu = 0.0
        try:
            while active.poll() is None:
                try:
                    for process in [root_process, *root_process.children(recursive=True)]:
                        known[(process.pid, process.create_time())] = process
                except (psutil.NoSuchProcess, psutil.AccessDenied):
                    pass
                rss = 0
                root_rss = 0
                root_cpu = root_previous_cpu
                live = 0
                for identity, process in list(known.items()):
                    try:
                        with process.oneshot():
                            if process.create_time() != identity[1] or process.status() == psutil.STATUS_ZOMBIE:
                                continue
                            memory = process.memory_info().rss
                            cpu = process.cpu_times()
                            latest_cpu[identity] = cpu.user + cpu.system
                            rss += memory
                            live += 1
                            if process.pid == active.pid:
                                root_rss = memory
                                root_cpu = cpu.user + cpu.system
                    except (psutil.NoSuchProcess, psutil.AccessDenied):
                        pass
                elapsed = time.perf_counter() - started
                total_cpu = sum(latest_cpu.values())
                interval = elapsed - previous_elapsed
                samples.append({"elapsed_s": elapsed, "tree_rss_bytes": rss, "root_rss_bytes": root_rss,
                                "tree_cpu_pct": max(0, (total_cpu - previous_cpu) / interval * 100),
                                "root_cpu_pct": max(0, (root_cpu - root_previous_cpu) / interval * 100), "live_processes": live})
                previous_elapsed, previous_cpu, root_previous_cpu = elapsed, total_cpu, root_cpu
                if elapsed > 330:
                    raise TimeoutError("benchmark command exceeded deadline")
                time.sleep(INTERVAL)
            active.wait()
        finally:
            if active.poll() is None:
                os.killpg(active.pid, signal.SIGINT)
                try:
                    active.wait(timeout=10)
                except subprocess.TimeoutExpired:
                    pass
                # The leader can exit while an owned descendant ignores SIGINT, and
                # `known` misses children that lived between samples. Escalate the
                # whole owned session so a timed-out measurement cannot leak browsers.
                # The group ID stays reserved while any member is alive.
                try:
                    os.killpg(active.pid, signal.SIGKILL)
                except ProcessLookupError:
                    pass  # every owned process already exited
                if active.poll() is None:
                    active.wait(timeout=5)
    elapsed = time.perf_counter() - started
    wall_elapsed = time.time() - wall_started
    assert abs(wall_elapsed - elapsed) < 2, 'Host sleep/clock discontinuity: measurement excluded'
    cpu_after = resource.getrusage(resource.RUSAGE_CHILDREN)
    cpu_total = cpu_after.ru_utime + cpu_after.ru_stime - cpu_before.ru_utime - cpu_before.ru_stime
    report = json.loads((case / "execution.json").read_text())
    expected = len(files) * expected_per_spec
    if engine == "9l":
        receipts = report["receipts"]
        valid = (report["complete"] and report["passed"] == len(files)
                 and all(report[k] == 0 for k in ("failed", "canceled", "timedOut", "errors"))
                 and len(receipts) == len(files)
                 and sum(r["executedTests"] for r in receipts) == expected
                 and all(r["status"] == "passed" and r["executed"] and r["validated"] and r["skippedTests"] == 0 and r["attempt"] == 1 for r in receipts))
    else:
        stats = report["stats"]
        valid = stats["expected"] == expected and all(stats[k] == 0 for k in ("unexpected", "skipped", "flaky")) and not report.get("errors")
    assert active.returncode == 0 and valid, f"invalid execution evidence in {label}; see owned artifact"
    # Allow child exit notification/reaping; never touch processes from other workspaces.
    survivors = []
    deadline = time.monotonic() + 2
    while True:
        survivors = []
        for key, p in known.items():
            try:
                if p.pid != active.pid and p.is_running() and p.create_time() == key[1] and p.status() != psutil.STATUS_ZOMBIE:
                    survivors.append(p)
            except psutil.NoSuchProcess:
                pass
        if not survivors or time.monotonic() >= deadline:
            break
        time.sleep(0.05)
    assert not survivors, f"owned process survivors after {label}"
    record = {"label": label, "engine": engine, "specs": len(files), "tests": expected, "workers": workers,
              "seconds": elapsed, "wall_seconds": wall_elapsed, "tests_per_second": expected / elapsed,
              "cpu_seconds": cpu_total, "average_cpu_pct": cpu_total / elapsed * 100,
              "peak_tree_cpu_pct": max(s["tree_cpu_pct"] for s in samples[1:] or samples),
              "peak_root_cpu_pct": max(s["root_cpu_pct"] for s in samples[1:] or samples),
              "peak_tree_rss_mib": max(s["tree_rss_bytes"] for s in samples) / (1024**2),
              "peak_root_rss_mib": max(s["root_rss_bytes"] for s in samples) / (1024**2),
              "peak_processes": max(s["live_processes"] for s in samples),
              "all_expected_tests_passed": True, "owned_survivors": 0, "evidence": str(case.relative_to(ROOT))}
    (case / "samples.json").write_text(json.dumps(samples))
    results["runs"].append(record)
    save()
    print(json.dumps(record), flush=True)
    return record


save()
files = sorted(TESTS.glob("checkout-*.spec.ts"))
for engine in ("playwright", "9l"):
    measure(engine, files[:1], 1, f"warmup-{engine}")
for count in (10, 50, 100):
    for workers in (1, 4):
        for engine in ("playwright", "9l"):
            measure(engine, files[:count], workers, f"{engine}-{count}spec-{workers}workers-r1")
for engine in ("playwright", "9l"):
    measure(engine, files, 8, f"{engine}-100spec-8workers-r1")
for repetition in (2, 3):
    # Alternate order to reduce one-sided thermal/cache ordering bias.
    for engine in (("9l", "playwright") if repetition == 2 else ("playwright", "9l")):
        measure(engine, files, 4, f"{engine}-100spec-4workers-r{repetition}")
# Same 500 assertions/test cases in one spec distinguishes file fanout from test count.
concentrated = TESTS / "concentrated.spec.ts"
concentrated.write_text(spec_text(500))
for engine in ("playwright", "9l"):
    measure(engine, [concentrated], 1, f"{engine}-1spec-500tests", expected_per_spec=500)
results["completed"] = True
save()
print(f"RESULTS {ROOT / 'results.json'}", flush=True)
