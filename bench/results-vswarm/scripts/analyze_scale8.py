#!/usr/bin/env python3
"""analyze_scale8.py SWEEPDIR OLD_R2_DIR — per-cell table (cells_full.csv) + per (fn,mode,c) summary (summary.md).
Per cell, from podsamp files: pods/ready at end of warm-up and end of measurement (last 1 Hz sample at or before that moment),
max pods, max per-second host busy cores in the measured window, Ready time of every pod relative to load start
(API lastTransitionTime, 1 s resolution; load start has sub-second resolution, so ±1 s)."""
import csv, datetime as dt, json, os, statistics as st, sys
d, old = sys.argv[1], sys.argv[2]
iso = lambda s: dt.datetime.fromisoformat(s.replace('Z', '+00:00')).timestamp()
rows = list(csv.DictReader(open(os.path.join(d, 'cells.csv'))))
full = []
for r in rows:
    if r['idle_ok'] != '1': full.append({**r}); continue
    pre = os.path.join(d, f"cyc{r['cycle']}", r['fn'], f"c{r['conc']}-{r['mode']}")
    S = [x for x in csv.DictReader(open(pre + '.samples.csv'))]
    P = json.load(open(pre + '.pods.json')) if os.path.exists(pre + '.pods.json') else {}
    t0, tw, tm = float(r['start_epoch']), float(r['warm_end_epoch']), float(r['meas_end_epoch'])
    at = lambda t: next((x for x in reversed(S) if float(x['epoch']) <= t), None)
    sw, sm = at(tw), at(tm)
    meas = [x for x in S if tw < float(x['epoch']) <= tm]
    allw = [x for x in S if t0 < float(x['epoch']) <= tm]
    ready = sorted(round(iso(p['ready_api']) - t0, 1) for p in P.values() if p.get('ready_api'))
    full.append({**r, 'pods_end_warm': sw['pods'] if sw else '', 'ready_end_warm': sw['ready'] if sw else '',
                 'pods_end_meas': sm['pods'] if sm else '', 'ready_end_meas': sm['ready'] if sm else '',
                 'pods_max': max((int(x['pods']) for x in allw), default=''),
                 'host_busy_max_meas': max((float(x['host_busy_cores']) for x in meas), default=''),
                 'host_busy_max_warm': max((float(x['host_busy_cores']) for x in allw if float(x['epoch']) <= tw), default=''),
                 'ready_s_after_start': ' '.join(f'{v:g}' for v in ready),
                 'ready_seen_s_after_start': ' '.join(f'{v:.1f}' for v in sorted(p['first_seen_ready_epoch'] - t0 for p in P.values() if p.get('first_seen_ready_epoch'))), 'n_pods_seen': len(P)})
keys = list(full[0].keys()) if full else []
for f in full:
    for k in f:
        if k not in keys: keys.append(k)
with open(os.path.join(d, 'cells_full.csv'), 'w') as f:
    w = csv.DictWriter(f, fieldnames=keys); w.writeheader(); [w.writerow(x) for x in full]

oldv = {}
for fn in ('aesgo', 'authgo'):
    p = os.path.join(old, fn, 'load', 'sweep.csv')
    if os.path.exists(p):
        for x in csv.DictReader(open(p)): oldv[(fn, x['mode'], x['conc'])] = x

def rng(v, fmt='{:.1f}'):
    v = [x for x in v if x != '' and x is not None]
    if not v: return '–'
    v = [float(x) for x in v]
    return fmt.format(min(v)) if min(v) == max(v) else f"{fmt.format(min(v))}–{fmt.format(max(v))}"
out = ['| fn | mode | c | n | RPS median (range) | p50 ms | p99 ms | errors | pods end warm-up | pods end meas. | max pods | Ready s after load start (per cycle) | host busy cores max (meas.) | old 1-pod RPS |',
       '|---|---|---|---|---|---|---|---|---|---|---|---|---|---|']
for fn in ('aesgo', 'authgo'):
    for c in ('1', '2', '4', '8', '16', '32'):
        for m in ('stock', 'mine'):
            g = [x for x in full if x['fn'] == fn and x['conc'] == c and x['mode'] == m]
            gv = [x for x in g if x.get('idle_ok') == '1' and x.get('rps')]
            if not g: continue
            bad = len(g) - len(gv)
            rp = [float(x['rps']) for x in gv]
            o = oldv.get((fn, m, c))
            out.append(f"| {fn} | {m} | {c} | {len(gv)}{f' (+{bad} not run)' if bad else ''} | "
                       f"{st.median(rp):.1f} ({rng(rp)}) | {rng([x['p50_ms'] for x in gv], '{:.2f}')} | {rng([x['p99_ms'] for x in gv], '{:.2f}')} | "
                       f"{rng([x['errors'] for x in gv], '{:.0f}')} | {rng([x['pods_end_warm'] for x in gv], '{:.0f}')} | {rng([x['pods_end_meas'] for x in gv], '{:.0f}')} | "
                       f"{rng([x['pods_max'] for x in gv], '{:.0f}')} | {' / '.join('[' + x['ready_s_after_start'] + ']' for x in gv)} | "
                       f"{rng([x['host_busy_max_meas'] for x in gv], '{:.1f}')} | {o['rps'] + ' (n=1)' if o else '–'} |")
open(os.path.join(d, 'summary.md'), 'w').write('\n'.join(out) + '\n')
print('\n'.join(out))
