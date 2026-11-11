#!/usr/bin/env python3
"""Phase E: summarise phases B, C, D into summary.csv and REPORT.md.

usage: analyze.py OUT_ROOT E_DIR
Cycle 1 of every cold-start run is a warm-up and is excluded from the statistics.
Only the Python standard library is used.
"""
import csv, os, re, sys, statistics as st

ROOT, E = sys.argv[1], sys.argv[2]
B, C, D, A = (os.path.join(ROOT, x) for x in ('B_cold', 'C_stock_breakdown', 'D_scaleout', 'A_preflight'))
MODES = ('stock', 'shared', 'perpod')
LABEL = {'stock': 'stock (fcmicro-fn, firecracker-containerd uVM)',
         'shared': 'shared (fcweb-fn, one Firecracker VM)',
         'perpod': 'per-pod (fcpod-fn, one Firecracker VM per pod)'}
summary = []          # rows: section, mode, metric, n, mean, min, max
notes = []            # data problems found while reading


def num(x):
    try:
        v = float(x)
        return v if v == v else None
    except (TypeError, ValueError):
        return None


def read(path):
    if not os.path.exists(path):
        notes.append(f'missing file: {os.path.relpath(path, ROOT)}')
        return []
    with open(path, newline='') as f:
        return list(csv.DictReader(f))


def add(section, mode, metric, vals):
    v = [x for x in vals if x is not None]
    if v:
        summary.append([section, mode, metric, len(v), round(st.mean(v), 4), round(min(v), 4), round(max(v), 4)])
    else:
        summary.append([section, mode, metric, 0, '', '', ''])


# ------------------------------------------------------------------ B: cold starts
cold = {}
for m in MODES:
    if m == 'stock':
        main = read(f'{B}/stock/cold_cycles_microvm_http.csv')
        ph = read(f'{B}/stock/cold_cycles_microvm_http_phases.csv')
        byc = {}
        for r in ph:                        # first uVM row per cycle
            byc.setdefault(r['cycle'], r)
        def g(r, k): return num(byc.get(r['cycle'], {}).get(k))
        rows = []
        for r in main:
            s, cr, hs, rsp = g(r, 'send_epoch'), g(r, 'create_vm_req_epoch'), g(r, 'httpd_started_epoch'), g(r, 'response_epoch')
            rows.append(dict(cycle=int(r['cycle']), http=r['http_code'], e2e=num(r['end_to_end_s']),
                             pre=(cr - s) if s and cr else None, vm=(hs - cr) if cr and hs else None,
                             post=(rsp - hs) if hs and rsp else None, total=(rsp - s) if s and rsp else None,
                             pod_gone=num(r['pod_gone_after_s']), stop=num(r.get('vhive_stop_s (from vhive log)')),
                             vms=num(byc.get(r['cycle'], {}).get('uvms_at_peak'))))
    else:
        main = read(f'{B}/{m}/cold_cycles_fc.csv')
        ph = {r['cycle']: r for r in read(f'{B}/{m}/cold_cycles_fc_phases.csv')}
        rows = []
        for r in main:
            p = ph.get(r['cycle'], {})
            s, a, v, rsp = (num(p.get(k)) for k in ('send_epoch', 'acquire_start_epoch', 'vm_reachable_epoch', 'response_epoch'))
            rows.append(dict(cycle=int(r['cycle']), http=r['http_code'], e2e=num(r['end_to_end_s']),
                             pre=(a - s) if s and a else None, vm=(v - a) if a and v else None,
                             post=(rsp - v) if v and rsp else None, total=(rsp - s) if s and rsp else None,
                             pod_gone=num(r['pod_gone_after_s']), stop=num(r.get('vhive_stop_s (from vhive log)')), vms=1))
    cold[m] = rows
    used = [r for r in rows if r['cycle'] != 1]
    bad = [r['cycle'] for r in used if r['http'] != '200']
    if bad:
        notes.append(f'{m}: non-200 responses in cycles {bad} (kept in the table; check B_cold/{m}/stdout.txt)')
    for k, name in (('pre', 'before VM stage'), ('vm', 'VM stage'), ('post', 'after VM stage'),
                    ('total', 'total (script clock)'), ('e2e', 'end-to-end (curl)'),
                    ('pod_gone', 'idle -> pod gone'), ('stop', 'vhive VM stop call')):
        add('cold', m, name, [r[k] for r in used])

# ------------------------------------------------------------------ C: stock breakdown
crow = read(f'{C}/breakdown.csv')
cused = [r for r in crow if r['cycle'] != '1'] or crow
step_cols = [k for k in (crow[0].keys() if crow else []) if re.match(r'^(k8s|pre-VM|VM\d|VM stage|post-VM|end-to-end)', k)]
for k in step_cols:
    add('stock_breakdown', 'stock', k, [num(r[k]) for r in cused])

# ------------------------------------------------------------------ D: scale-out
drows = read(f'{D}/scale.csv')
for r in drows:
    for k in ('rps', 'p50_ms', 'p99_ms', 'errors', 'max_pods', 'end_pods'):
        add('scale', r['mode'], f'c{r["conc"]} {k}', [num(r[k])])

with open(f'{E}/summary.csv', 'w', newline='') as f:
    w = csv.writer(f)
    w.writerow(['section', 'mode', 'metric', 'n', 'mean', 'min', 'max'])
    w.writerows(summary)

# ------------------------------------------------------------------ REPORT.md
def fmt(x, d=3):
    return '–' if x in ('', None) else f'{x:.{d}f}'

def cell(section, mode, metric, d=3):
    for s in summary:
        if s[0] == section and s[1] == mode and s[2] == metric:
            return f'{fmt(s[4], d)} ({fmt(s[5], d)}–{fmt(s[6], d)})' if s[3] else '–'
    return '–'

out = ['# Overnight benchmark report (night-1111)', '']
env = open(f'{A}/fcweb_env.txt').read().split() if os.path.exists(f'{A}/fcweb_env.txt') else []
ver = open(f'{A}/versions.txt').read().splitlines() if os.path.exists(f'{A}/versions.txt') else []
rev = next((l for l in ver if re.fullmatch(r'[0-9a-f]{40}', l)), 'unknown')
out += [f'vhive rev `{rev}`, daemon env {" ".join(env) or "unknown"}. '
        'Values are **mean (min–max)**; cycle 1 of every cold-start run is a warm-up and is excluded.', '']

out += ['## B. Cold start (seconds)', '',
        '| metric | ' + ' | '.join(MODES) + ' |', '|---|' + '---|' * len(MODES)]
ns = {m: len([r for r in cold.get(m, []) if r['cycle'] != 1]) for m in MODES}
out.append('| n (cycles used) | ' + ' | '.join(str(ns[m]) for m in MODES) + ' |')
for name in ('before VM stage', 'VM stage', 'after VM stage', 'total (script clock)', 'end-to-end (curl)',
             'idle -> pod gone', 'vhive VM stop call'):
    out.append(f'| {name} | ' + ' | '.join(cell('cold', m, name) for m in MODES) + ' |')
out += ['', 'VM stage = stock: firecracker-containerd `create VM request` → guest prints `httpd started`; '
        'shared/per-pod: vhive acquire start → `Firecracker VM reachable` (guest app answered HTTP 200).', '']

out += ['## C. Stock VM stage, step by step (seconds)', '']
if crow:
    out += [f'n = {len(cused)} dedicated stock cold starts (cycle 1 excluded). Details and the log lines used: '
            '`C_stock_breakdown/breakdown.md`.', '', '| step | n | mean | min | max |', '|---|---|---|---|---|']
    for s in summary:
        if s[0] == 'stock_breakdown':
            out.append(f'| {s[2]} | {s[3]} | {fmt(s[4])} | {fmt(s[5])} | {fmt(s[6])} |')
else:
    out.append('No data (phase C did not produce breakdown.csv).')
out.append('')

out += ['## D. Scale-out, closed loop (one 30 s measurement per cell, after a 10 s warm-up)', '']
if drows:
    for k, title, d in (('rps', 'requests/s', 0), ('p50_ms', 'p50 latency (ms)', 2), ('p99_ms', 'p99 latency (ms)', 2),
                        ('errors', 'errors', 0), ('max_pods', 'max pods during measurement', 0)):
        out += [f'**{title}**', '', '| concurrency | ' + ' | '.join(MODES) + ' |', '|---|' + '---|' * len(MODES)]
        for c in sorted({int(r['conc']) for r in drows}):
            vals = []
            for m in MODES:
                r = next((x for x in drows if x['mode'] == m and int(x['conc']) == c), None)
                vals.append(fmt(num(r[k]), d) if r and num(r[k]) is not None else '–')
            out.append(f'| {c} | ' + ' | '.join(vals) + ' |')
        out.append('')
    if os.path.exists(f'{D}/yaml_differences.txt'):
        out += ['Remaining differences between the three services: see `D_scaleout/yaml_differences.txt`.', '']
else:
    out += ['No data (phase D did not produce scale.csv).', '']

# ------------------------------------------------------------------ caveats (automatic)
cav = [
    '**One trivial workload.** Every mode serves a tiny static page (busybox httpd for stock, python3 http.server '
    'for shared/per-pod). No real function code, no state, no CPU or I/O work; results say nothing about heavier functions.',
    '**Memory differs: 256 MiB vs 512 MiB.** shared and per-pod VMs get '
    + (next((e.split('=')[1] for e in env if e.startswith('FCWEB_MEM_MIB=')), '256')) +
    ' MiB (FCWEB_MEM_MIB / FC_MEM_MIB); stock uVMs get 512 MiB, hard-coded in vHive\'s ctriface. All use 1 vCPU.',
    '**Baked-in program vs real container image.** shared/per-pod boot a prepared rootfs.ext4 whose init starts the '
    'server directly (no agent, no image, no container). Stock boots a generic rootfs with systemd and the '
    'firecracker-containerd agent, then mounts the OCI image via a devmapper snapshot and starts it as a container. '
    'Part of the stock cost is the price of running an arbitrary image, which the other two modes do not support.',
    '**VM-stage end points differ.** shared/per-pod end at an HTTP 200 from the guest app; stock ends when the guest '
    'prints "httpd started" (just before httpd listens), so if anything the stock VM stage is slightly under-counted.',
    '**Small samples.** n ≈ 10 cold starts per mode, one 30 s run per scale-out cell, one node, one night. '
    'min–max ranges are shown instead of confidence intervals.',
    '**Image cache.** Images are already on the node; no cold start includes a network image pull.',
    '**Phase D modes share one VM differently.** shared = all pods of n11-shared use the single fcweb0 VM; '
    'per-pod = one VM per pod; stock = one uVM per pod. Pod count is not VM count for the shared mode.',
]
for m in MODES:
    if ns[m] and ns[m] < 10:
        cav.append(f'**{m}: only {ns[m]} usable cold cycles** (fewer than planned).')
vms = [r['vms'] for r in cold.get('stock', []) if r['cycle'] != 1 and r['vms']]
if vms and max(vms) > 1:
    cav.append(f'**Stock created more than one uVM per cold start** in some cycles (max {int(max(vms))}); '
               'parallel boots contend for CPU.')
for r in drows:
    if (num(r['errors']) or 0) > 0:
        cav.append(f'**Errors in scale-out:** {r["mode"]} at concurrency {r["conc"]}: {r["errors"]} failed requests '
                   '(load.py counts a request as failed after a 5 s timeout or connection error).')
    if (num(r['max_pods']) or 0) >= 8:
        cav.append(f'**max-scale reached:** {r["mode"]} at concurrency {r["conc"]} hit 8 pods; higher load cannot add capacity.')
if os.path.exists(f'{ROOT}/night.log'):
    for l in open(f'{ROOT}/night.log'):
        if re.search(r'=== phase \w end: (FAILED|TIMEOUT)', l) or 'skipped' in l:
            cav.append('**Phase problem:** `' + l.strip() + '`')
cav += [f'Data note: {n}' for n in notes]
out += ['## Caveats (generated automatically)', ''] + [f'{i}. {c}' for i, c in enumerate(cav, 1)] + ['']

open(f'{E}/REPORT.md', 'w').write('\n'.join(out))
print(f'wrote {E}/summary.csv ({len(summary)} rows) and {E}/REPORT.md ({len(cav)} caveats)')
