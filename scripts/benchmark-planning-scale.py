"""Measure planning only for 100/1,000/10,000 files using the measured binary."""
import json
import os
import re
import statistics
import subprocess
import time
from pathlib import Path

repo = Path(__file__).resolve().parent.parent
roots = []
for path in (repo / '.context').glob('efficiency-resources-*/results.json'):
    report = json.loads(path.read_text())
    if report.get('completed') and not report.get('excluded_from_final_report'):
        roots.append(path.parent)
assert roots, 'Complete native resource benchmark first'
root = sorted(roots)[-1]
report = json.loads((root / 'results.json').read_text())
binary = root / '9l'
safe_env = {k: os.environ[k] for k in ('PATH', 'HOME', 'TMPDIR', 'LANG', 'LC_ALL') if k in os.environ}
fixture = root / 'fixture' / 'planning'
fixture.mkdir()
results = {'source_commit': report['metadata']['source_commit'],
           'scope': 'CLI planning only; these generated files are not executed tests; build excluded; warm caches; 3 repetitions per size',
           'ram_method': 'macOS /usr/bin/time -l maximum resident set size; planning command has no test/browser children',
           'runs': []}
created = 0
for count in (100, 1000, 10000):
    for n in range(created, count):
        (fixture / f'plan-{n:05d}.spec.ts').write_text('// Planning fixture only: no execution claim.\n')
    created = count
    for sample in range(3):
        started = time.perf_counter()
        p = subprocess.run(['/usr/bin/time', '-l', str(binary), 'plan', str(fixture / '*.spec.ts'), '--format', 'json'],
                           cwd=repo, env=safe_env, capture_output=True, text=True, timeout=60)
        elapsed = time.perf_counter() - started
        assert p.returncode == 0, 'planning failed; diagnostics withheld'
        plan = json.loads(p.stdout)
        assert len(plan['jobs']) == count and len(plan['skipped']) == 0
        memory = re.search(r'(\d+)\s+maximum resident set size', p.stderr)
        assert memory, 'macOS time did not supply memory metric'
        results['runs'].append({'jobs': count, 'sample': sample + 1, 'seconds': elapsed,
                               'peak_go_rss_mib': int(memory.group(1)) / (1024 ** 2),
                               'all_jobs_planned': True})
    rows = [r for r in results['runs'] if r['jobs'] == count]
    print(json.dumps({'jobs': count, 'median_ms': statistics.median(r['seconds'] for r in rows) * 1000,
                      'peak_go_rss_mib': max(r['peak_go_rss_mib'] for r in rows)}), flush=True)
    (root / 'planning-results.json').write_text(json.dumps(results, indent=2))
print('RESULTS', root / 'planning-results.json')
