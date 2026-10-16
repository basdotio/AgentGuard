#!/usr/bin/env python3 -I
"""mcp-run.py <work list.jsonl> <out dir> [parallel]: Cisco mcp-scanner `behavioral` (LLM) per sample tree.
Key read from the file, never printed; raw JSON per sample in <out>/raw/<sample>.json, stderr beside it."""
import sys, os, json, subprocess, datetime
from concurrent.futures import ThreadPoolExecutor
SB='<scratch>'
C='../agent-artifact-corpus'
lst, out = sys.argv[1:3]; par = int(sys.argv[3]) if len(sys.argv) > 3 else 3
os.makedirs(out + '/raw', exist_ok=True)
env = dict(os.environ, MCP_SCANNER_LLM_API_KEY=open(os.path.expanduser('~/.config/aguard/openai.key')).read().strip(),
           MCP_SCANNER_LLM_MODEL='gpt-4.1-mini', MCP_SCANNER_LLM_BASE_URL='https://api.openai.com/v1')
rows = [json.loads(l) for l in open(lst) if l.strip()]
print(f"=== {os.path.basename(out)} start {datetime.datetime.utcnow().isoformat()}Z n={len(rows)}", flush=True)
def one(r):
    s, d = r['sample'], os.path.join(C, r['path']); dst = f"{out}/raw/{s}.json"
    # FILE MODE: point the analyzer at the tree's single .py, the shape its own evals are in. Directory
    # mode on these same trees found no tool at all (the 2026-10-15 probe); file mode finds them.
    pys = sorted(f for f in os.listdir(d) if f.endswith('.py'))
    if len(pys) == 1: d = os.path.join(d, pys[0])
    if os.path.exists(dst): return s
    try:
        p = subprocess.run([f"{SB}/tools/mcp-venv/bin/mcp-scanner", '--log-level', 'error', 'behavioral', d, '--format', 'raw', '--output', dst],
                           cwd='/tmp', env=env, capture_output=True, text=True, timeout=900)
        open(dst + '.err', 'w').write(p.stderr[-4000:] + f"\nexit {p.returncode}\n")
    except subprocess.TimeoutExpired:
        open(dst + '.err', 'w').write('timeout 900s\n')
    return s
with ThreadPoolExecutor(par) as ex:
    for i, s in enumerate(ex.map(one, rows), 1):
        if i % 25 == 0: print(f"  {i}/{len(rows)}", flush=True)
print(f"=== {os.path.basename(out)} done {datetime.datetime.utcnow().isoformat()}Z · {len([f for f in os.listdir(out+'/raw') if f.endswith('.json')])} json", flush=True)
