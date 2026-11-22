#!/usr/bin/env python3
"""teardown.py DIR (= out/r2/<fn>/cold) — VM stop request -> VM process exit, per cycle, both modes.
stock: firecracker-containerd pane lines (fccd-pane.raw, deduplicated):  'stop VM: VMID:"<id>"'  ->  next 'firecracker exited: ...'
       cross-check: pidwatch.log 'exit' of the firecracker process whose args contain --id <id>.
mine:  vhive log line 'per-pod Firecracker VM stopped ... took=<d>' : stop request = line time - took (vhive's own timer, started just before
       SIGTERM; no separate log line exists); process exit = pidwatch.log 'exit' of the process with /run/vhive-fcweb/<slot>/fc.sock
       (20 ms poll); line time = end of vhive's cleanup (process gone + tap/socket removed)."""
import re, sys, datetime as dt, os
d = sys.argv[1]
def iso(s):  # 2026-11-21T03:30:03.086965710-07:00 -> epoch
    m = re.match(r'(\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d)(?:\.(\d+))?([+-]\d\d:\d\d)', s)
    t = dt.datetime.fromisoformat(m.group(1) + m.group(3)).timestamp()
    return t + (float('0.' + m.group(2)) if m.group(2) else 0)
def dur(s):
    m = re.match(r'([\d.]+)(ms|s|µs|us)$', s); v = float(m.group(1)); return v / 1000 if m.group(2) == 'ms' else v / 1e6 if m.group(2) in ('µs', 'us') else v
t0 = float(open(f'{d}/pidwatch.log').readline().split()[0])
pw = [l.split(None, 4) for l in open(f'{d}/pidwatch.log') if ' exit ' in l]   # epoch iso exit pid=.. rest
pwapp = [l.split(None, 4) for l in open(f'{d}/pidwatch.log') if ' appear ' in l]
lines = sorted({l.strip() for l in open(f'{d}/fccd-pane.raw') if re.match(r'^[A-Z]{4}\[\d{4}-', l)}, key=lambda l: iso(l[5:40]))
lines = [l for l in lines if iso(l[5:40]) >= t0]
fmt = lambda e: dt.datetime.fromtimestamp(e).strftime('%H:%M:%S.%f')[:-3]
print('| mode | VM | stop request (log) | process exit (log) | request→exit s | pidwatch exit | request→pidwatch exit s | other |')
print('|---|---|---|---|---|---|---|---|')
for i, l in enumerate(lines):
    m = re.search(r'stop VM: VMID:"([^"]+)"', l)
    if not m: continue
    vid, ts = m.group(1), iso(l[5:40])
    ex = next((x for x in lines[i + 1:] if 'firecracker exited' in x), None)
    te = iso(ex[5:40]) if ex else None
    p = next((x for x in pw if f'--id {vid}' in x[4]), None)
    tp = float(p[0]) if p else None
    print(f"| stock | {vid} | {fmt(ts)} | {fmt(te) if te else 'NOT FOUND'} | {te - ts:.3f} | {fmt(tp) if tp else 'n/a'} | "
          f"{(tp - ts) if tp else float('nan'):.3f} | exit line: {ex.split(']',1)[1].split('  ')[0].strip() if ex else ''} |" if te else
          f"| stock | {vid} | {fmt(ts)} | NOT FOUND | - | {fmt(tp) if tp else 'n/a'} | - | |")
for l in open(f'{d}/../vhive-cold.log', errors='replace'):
    if 'per-pod Firecracker VM stopped' not in l: continue
    ts = iso(re.search(r'time="([^"]+)"', l).group(1)); took = dur(re.search(r'took=(\S+)', l).group(1)); slot = re.search(r'slot=(\d+)', l).group(1)
    st = ts - took
    p = min((x for x in pw if f'/run/vhive-fcweb/{slot}/fc.sock' in x[4] and abs(float(x[0]) - ts) < 30), key=lambda x: abs(float(x[0]) - ts), default=None)
    tp = float(p[0]) if p else None
    print(f"| mine | slot {slot} {re.search(r'vmID=(\S+)', l).group(1)} | {fmt(st)} (= line − took) | {fmt(ts)} (cleanup done, took={took:.3f}) | "
          f"{took:.3f} | {fmt(tp) if tp else 'n/a'} | {(tp - st) if tp else float('nan'):.3f} | |")
