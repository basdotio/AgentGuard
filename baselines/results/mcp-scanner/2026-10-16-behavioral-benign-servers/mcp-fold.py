#!/usr/bin/env python3 -I
"""mcp-fold.py <work list> <run dir> <out dir>: fold Cisco mcp-scanner `behavioral` raw JSON into
verdicts.jsonl (threshold high) and a per-population summary. One tool-level record list per sample;
a sample with no tool found is no-verdict (the tool read nothing it could judge)."""
import sys, json, os, collections
lst, run, out = sys.argv[1:4]
os.makedirs(out, exist_ok=True)
LADDER = {'HIGH': 3, 'MEDIUM': 2, 'LOW': 1, 'SAFE': 0, 'UNKNOWN': 0}
pins = json.load(open('../agent-artifact-corpus/manifest/pins/mcp-servers.json'))['pins']
phase = {('ben-mcp-' + p['name']): p['phase'] for p in pins if p['surface'] == 'mcp'}
rows = [json.loads(l) for l in open(lst)]
verdicts, ledger = [], []
per = collections.defaultdict(lambda: collections.Counter())
for r in rows:
    s = r['sample']; pop = phase.get(s, 'official')
    raw = f"{run}/raw/{s}.json"
    per[pop]['n'] += 1
    if not os.path.exists(raw):
        ledger.append({'sample': s, 'outcome': 'errored', 'detail': open(raw + '.err').read()[-300:] if os.path.exists(raw + '.err') else 'no output'})
        per[pop]['errored'] += 1
        continue
    try:
        tools = json.load(open(raw))
    except Exception as e:
        ledger.append({'sample': s, 'outcome': 'errored', 'detail': f'unparseable: {e}'}); per[pop]['errored'] += 1; continue
    real = [t for t in tools if t.get('tool_name') != 'No MCP functions found']
    if not real:
        ledger.append({'sample': s, 'outcome': 'no-verdict', 'reason': 'no-load-path', 'detail': 'no @mcp.tool() found by the behavioral analyzer'})
        per[pop]['no_tools'] += 1
        continue
    worst = 0; unsafe = 0; sevs = []
    for t in real:
        f = (t.get('findings') or {}).get('behavioral_analyzer')
        sev = (f.get('severity') if isinstance(f, dict) else f) or 'SAFE'
        sev = str(sev).upper()
        sevs.append(sev)
        worst = max(worst, LADDER.get(sev, 0))
        unsafe += 0 if t.get('is_safe', True) else 1
    verdict = 'malicious' if worst >= LADDER['HIGH'] else 'benign'
    verdicts.append({'sample': s, 'verdict': verdict, 'severity': {3: 'high', 2: 'medium', 1: 'low', 0: 'none'}[worst]})
    ledger.append({'sample': s, 'outcome': 'scored', 'verdict': verdict, 'tools': len(real), 'unsafe_tools': unsafe, 'severities': collections.Counter(sevs)})
    per[pop]['scored'] += 1
    per[pop]['high'] += worst >= 3
    per[pop]['any_unsafe'] += unsafe > 0
    per[pop]['medium_or_worse'] += worst >= 2
with open(f'{out}/verdicts.jsonl', 'w') as f:
    for v in verdicts: f.write(json.dumps(v) + '\n')
with open(f'{out}/ledger.jsonl', 'w') as f:
    for l in ledger: f.write(json.dumps(l, default=dict) + '\n')
lines = [f"{'population':<16} {'n':>4} {'scored':>6} {'no_tools':>8} {'errored':>7} {'HIGH':>5} {'>=MED':>5} {'any unsafe':>10}"]
tot = collections.Counter()
for pop in ('official', 'servers', 'servers-recent'):
    c = per[pop]; tot.update(c)
    lines.append(f"{pop:<16} {c['n']:>4} {c['scored']:>6} {c['no_tools']:>8} {c['errored']:>7} {c['high']:>5} {c['medium_or_worse']:>5} {c['any_unsafe']:>10}")
c = tot
lines.append(f"{'all':<16} {c['n']:>4} {c['scored']:>6} {c['no_tools']:>8} {c['errored']:>7} {c['high']:>5} {c['medium_or_worse']:>5} {c['any_unsafe']:>10}")
open(f'{out}/summary.txt', 'w').write("\n".join(lines) + "\n")
print("\n".join(lines))
