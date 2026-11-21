#!/usr/bin/env python3
"""plot.py DIR TITLE — from DIR/sweep.csv and DIR/vary-{stock,mine}/seconds.csv write
DIR/rps_vs_conc.png (rps and p99 vs concurrency, stock vs mine) and DIR/vary.png (time-varying run)."""
import csv, os, sys
import matplotlib
matplotlib.use('Agg')
import matplotlib.pyplot as plt

d, title = sys.argv[1], sys.argv[2]
C = {'stock': '#2a6fbb', 'mine': '#d9661f'}
rows = list(csv.DictReader(open(os.path.join(d, 'sweep.csv'))))
fig, ax = plt.subplots(1, 2, figsize=(11, 4))
for m in ('stock', 'mine'):
    r = [x for x in rows if x['mode'] == m and x['rps']]
    cs = [int(x['conc']) for x in r]
    ax[0].plot(cs, [float(x['rps']) for x in r], 'o-', color=C[m], label=m)
    ax[1].plot(cs, [float(x['p99_ms']) if x['p99_ms'] else float('nan') for x in r], 'o-', color=C[m], label=m + ' p99')
    ax[1].plot(cs, [float(x['p50_ms']) if x['p50_ms'] else float('nan') for x in r], 's--', color=C[m], alpha=.6, label=m + ' p50')
for a_, yl in zip(ax, ('requests/s (ok only, 30 s measured)', 'latency ms')):
    a_.set_xscale('log', base=2); a_.set_xticks([1, 2, 4, 8, 16]); a_.set_xticklabels(['1', '2', '4', '8', '16'])
    a_.set_xlabel('concurrency (closed-loop workers)'); a_.set_ylabel(yl); a_.grid(alpha=.3); a_.legend(frameon=False)
ax[1].set_yscale('log')
fig.suptitle(f'{title}: throughput and latency vs concurrency (1 pod max, 1 vCPU / 512 MiB)')
fig.tight_layout(); fig.savefig(os.path.join(d, 'rps_vs_conc.png'), dpi=120); plt.close(fig)

have = [m for m in ('stock', 'mine') if os.path.exists(os.path.join(d, f'vary-{m}', 'seconds.csv'))]
if have:
    fig, ax = plt.subplots(3, 1, figsize=(11, 8), sharex=True)
    for m in have:
        s = list(csv.DictReader(open(os.path.join(d, f'vary-{m}', 'seconds.csv'))))
        t = [int(x['sec']) for x in s]
        if m == have[0]:
            ax[0].step(t, [float(x['target_rps']) for x in s], where='post', color='grey', lw=1, label='target rps')
        ax[0].plot(t, [int(x['ok']) for x in s], color=C[m], label=f'{m} ok/s')
        ax[0].plot(t, [int(x['errors']) if int(x['errors']) else float('nan') for x in s], 'x', color=C[m], label=f'{m} errors/s')
        ax[1].plot(t, [float(x['p50_ms']) if x['p50_ms'] else float('nan') for x in s], color=C[m], label=f'{m} p50 (by send second)')
        ax[2].step(t, [int(x['pods']) if x['pods'] not in ('', '-1') else float('nan') for x in s], where='post', color=C[m], label=f'{m} pods')
        ax[2].step(t, [int(x['firecracker_vms']) if x['firecracker_vms'] not in ('', '-1') else float('nan') for x in s], where='post', color=C[m], ls=':', label=f'{m} firecracker VMs')
    ax[0].set_ylabel('requests per second'); ax[1].set_ylabel('latency ms'); ax[1].set_yscale('log'); ax[2].set_ylabel('count')
    ax[2].set_xlabel('seconds since first scheduled request (each run starts from zero pods)')
    for a_ in ax: a_.grid(alpha=.3); a_.legend(frameon=False, fontsize=8, ncol=2)
    fig.suptitle(f'{title}: open-loop time-varying load')
    fig.tight_layout(); fig.savefig(os.path.join(d, 'vary.png'), dpi=120); plt.close(fig)
print('plots written in', d)
