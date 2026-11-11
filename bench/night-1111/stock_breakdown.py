#!/usr/bin/env python3
"""Phase C: split the stock (firecracker-containerd) cold start into sub-steps.

usage: stock_breakdown.py C_DIR

Inputs per cycle c (written by night.sh phase C):
  c<c>_meta.txt                   send/response epochs, pod name
  c<c>_firecracker_containerd.txt tmux capture of firecracker-containerd (debug level)
  c<c>_containerd.txt             tmux capture of containerd (info level, CRI calls)
  c<c>_vhive.txt                  slice of /tmp/vhive-logs/vhive_log.txt starting at the request
Outputs: breakdown.csv (one row per cycle, absent marks = empty) and breakdown.md.
Every timestamp comes from a log line; a step whose line is missing is reported as missing, never guessed.
"""
import csv, os, re, sys, statistics as st
from datetime import datetime

D = sys.argv[1]
TS = re.compile(r'^(?:[A-Z]{4}\[([^\]]+)\]|time="([^"]+)")')


def epoch(s):
    # 2026-11-09T01:57:38.006604680-07:00 -> float; keep microseconds for fromisoformat
    m = re.match(r'(\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d)(\.\d+)?(.*)$', s)
    if not m:               # e.g. logrus' relative 'WARN[0000]' lines: no wall-clock time, skip
        return None
    frac = (m.group(2) or '.0')[:7]
    return datetime.fromisoformat(m.group(1) + frac + m.group(3)).timestamp()


def lines(path):
    out = []
    if not os.path.exists(path):
        return out
    for l in open(path, errors='replace'):
        m = TS.match(l)
        t = epoch(m.group(1) or m.group(2)) if m else None
        if t is not None:
            out.append((t, l.rstrip('\n')))
    return out


def first(ls, pat, after=0.0):
    r = re.compile(pat)
    for t, l in ls:
        if t >= after and r.search(l):
            return t, l
    return None, None


# (column, description) in timeline order; each is an absolute epoch
STEPS = [
    ('sandbox_req',   'containerd: RunPodSandbox for the pod'),
    ('sandbox_ready', 'containerd: RunPodSandbox returns sandbox id'),
    ('fc_create_req', 'firecracker-containerd: create VM request (MachineCfg)'),
    ('vmm_started',   'firecracker-containerd: startInstance successful (VMM running, guest kernel booting)'),
    ('agent_ready',   'firecracker-containerd: successfully started the VM (in-guest agent answered)'),
    ('snap_prepare',  'firecracker-containerd: prepare snapshot for the container rootfs'),
    ('snap_device',   'firecracker-containerd: creating snapshot device (devmapper thin snapshot)'),
    ('drive_patched', 'firecracker-containerd: PatchGuestDrive successful (rootfs hot-plugged into VM)'),
    ('task_created',  'firecracker-containerd: successfully created task'),
    ('app_started',   'vhive log: guest stdout "httpd started"'),
    ('uc_created',    'containerd: CreateContainer user-container returns'),
    ('uc_started',    'containerd: StartContainer (user-container) returns successfully'),
    ('qp_started',    'containerd: StartContainer (queue-proxy) returns successfully'),
]
# (name, from, to, meaning)
DUR = [
    ('k8s: request -> sandbox ready',               'send', 'sandbox_ready', 'activator, autoscaler, scheduler, kubelet, pause sandbox'),
    ('pre-VM: sandbox -> create VM request',        'sandbox_ready', 'fc_create_req', 'kubelet CreateContainer -> vhive; vhive network setup + image lookup (cached, not logged separately)'),
    ('VM1 VMM launch',                              'fc_create_req', 'vmm_started', 'shim start, firecracker process, machine/boot/drive/NIC config'),
    ('VM2 guest boot + agent',                      'vmm_started', 'agent_ready', 'kernel + systemd + firecracker-containerd agent'),
    ('VM3 container create (vhive)',                'agent_ready', 'snap_prepare', 'vhive NewContainer: container metadata, snapshot request'),
    ('VM4 rootfs snapshot (unpack equivalent)',     'snap_prepare', 'drive_patched', 'devmapper thin snapshot of the cached image + hot-plug into the VM'),
    ('VM5 task create',                             'drive_patched', 'task_created', 'agent mounts rootfs and creates the container task in the guest'),
    ('VM6 task start -> app up',                    'task_created', 'app_started', 'task.Wait/Start, busybox httpd prints "httpd started"'),
    ('VM stage total (create VM -> app up)',        'fc_create_req', 'app_started', 'the "~2.66 s" stage'),
    ('post-VM: app up -> response',                 'app_started', 'response', 'placeholder UC + queue-proxy start, readiness, first proxied request'),
    ('end-to-end',                                  'send', 'response', 'client view'),
]


def cycle(c):
    meta = dict(kv.split('=', 1) for kv in open(f'{D}/c{c}_meta.txt').read().split())
    send, resp, pod = float(meta['send_epoch']), float(meta['response_epoch']), meta.get('pod', '')
    fc = lines(f'{D}/c{c}_firecracker_containerd.txt')
    ct = lines(f'{D}/c{c}_containerd.txt')
    vh = lines(f'{D}/c{c}_vhive.txt')
    r = {'cycle': c, 'http': meta.get('http'), 'send': send, 'response': resp, 'pod': pod, 'vmID': ''}

    t, l = first(fc, r'create VM request: VMID:"[^"]+" MachineCfg', send - 1)
    if l:
        vm = re.search(r'VMID:"([^"]+)"', l).group(1)
        r['vmID'], r['fc_create_req'] = vm, t
        r['vmm_started'] = first(fc, r'startInstance successful', t)[0]
        r['agent_ready'] = first(fc, r'successfully started the VM.*vmID=' + re.escape(vm), t)[0]
        r['snap_prepare'] = first(fc, r'prepare snapshot.*key=vm' + re.escape(vm), t)[0]
        if r['snap_prepare']:
            r['snap_device'] = first(fc, r'creating snapshot device', r['snap_prepare'])[0]
            r['drive_patched'] = first(fc, r'PatchGuestDrive successful', r['snap_prepare'])[0]
        r['task_created'] = first(fc, r'successfully created task.*vmID=' + re.escape(vm), t)[0]
        r['app_started'] = first(vh, r'httpd started.*vmID=' + re.escape(vm))[0] or first(vh, r'httpd started')[0]

    if pod:
        t, l = first(ct, r'RunPodSandbox for name:"' + re.escape(pod) + '"', send - 1)
        r['sandbox_req'] = t
        t2, l2 = first(ct, r'RunPodSandbox for name:"' + re.escape(pod) + '"' + r'.*returns sandbox id', send - 1)
        r['sandbox_ready'] = t2
        if l2:
            sid = re.search(r'returns sandbox id "([0-9a-f]+)"', l2).group(1)
            ids = {}
            for name in ('user-container', 'queue-proxy'):
                tt, ll = first(ct, rf'CreateContainer within sandbox "{sid}" for name:"{name}" returns container id', t2)
                if ll:
                    ids[name] = re.search(r'returns container id "([0-9a-f]+)"', ll).group(1)
                    if name == 'user-container':
                        r['uc_created'] = tt
            if 'user-container' in ids:
                r['uc_started'] = first(ct, rf'StartContainer for "{ids["user-container"]}" returns successfully', t2)[0]
            if 'queue-proxy' in ids:
                r['qp_started'] = first(ct, rf'StartContainer for "{ids["queue-proxy"]}" returns successfully', t2)[0]
    return r


cs = sorted(int(m.group(1)) for f in os.listdir(D) if (m := re.match(r'c(\d+)_meta\.txt$', f)))
rows = [cycle(c) for c in cs]
cols = ['cycle', 'http', 'pod', 'vmID', 'send'] + [s for s, _ in STEPS] + ['response'] + [n for n, *_ in DUR]
for r in rows:
    for n, a, b, _ in DUR:
        r[n] = round(r[b] - r[a], 4) if r.get(a) and r.get(b) else None
with open(f'{D}/breakdown.csv', 'w', newline='') as f:
    w = csv.DictWriter(f, fieldnames=cols, extrasaction='ignore')
    w.writeheader()
    w.writerows(rows)

used = [r for r in rows if r['cycle'] != 1] or rows   # cycle 1 = warm-up
md = [f'# Stock cold start breakdown (phase C)\n',
      f'Cycles found: {len(rows)}; statistics use cycles {[r["cycle"] for r in used]} (cycle 1 is a warm-up).\n',
      '| step | n | mean s | min s | max s | what it covers |', '|---|---|---|---|---|---|']
for n, a, b, what in DUR:
    v = [r[n] for r in used if r[n] is not None]
    if v:
        md.append(f'| {n} | {len(v)} | {st.mean(v):.3f} | {min(v):.3f} | {max(v):.3f} | {what} |')
    else:
        md.append(f'| {n} | 0 | – | – | – | {what} — **log line(s) missing** |')
missing = sorted({s for r in used for s, _ in STEPS if not r.get(s)})
md.append('\n## Log lines used\n')
md += [f'- `{s}`: {d}' for s, d in STEPS]
md.append('\n## Not measurable from the logs\n')
md.append('- **Image pull**: vHive (devmapper snapshotter) calls getImage *before* CreateVM and logs it only at debug '
          'level; the daemon runs at info level, so there is no timestamp. The image is already cached on the node, so '
          'no network pull happens; whatever lookup time exists is inside "pre-VM".')
md.append('- **vHive\'s own StartVM sub-step timings** (`StartVM: Creating a new container`, `...task`, `...Starting the task`) '
          'are debug-level and absent; the firecracker-containerd lines above stand in for them.')
md.append('- Pane timestamps are the daemons\' own; pod creation time from the API has only 1 s resolution and is not used.')
if missing:
    md.append(f'\n**Missing in at least one cycle:** {", ".join(missing)} (those cells are empty in breakdown.csv).')
open(f'{D}/breakdown.md', 'w').write('\n'.join(md) + '\n')
print('\n'.join(md[:len(DUR) + 4]))
sys.exit(0 if rows and any(r.get('fc_create_req') for r in rows) else 1)
