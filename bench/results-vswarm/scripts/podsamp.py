#!/usr/bin/env python3
"""podsamp.py SVC OUTPREFIX — 1 Hz sampler, runs until SIGTERM.
OUTPREFIX.samples.csv: epoch,pods,ready,terminating,firecracker_vms,host_busy_cores (busy cores over the last ~1 s, /proc/stat)
OUTPREFIX.pods.json : every pod of SVC ever seen -> creationTimestamp, Ready=True lastTransitionTime (1 s resolution, from the API),
                      first_seen_ready_epoch (this sampler, ±1 s), deletionTimestamp, node.
Read-only: kubectl get, /proc/stat, pgrep."""
import json, signal, subprocess, sys, time
svc, out = sys.argv[1], sys.argv[2]
stop = False
def _s(*_):
    global stop; stop = True
signal.signal(signal.SIGTERM, _s); signal.signal(signal.SIGINT, _s)
def stat():
    v = [int(x) for x in open('/proc/stat').readline().split()[1:]]
    return v[0] + v[1] + v[2] + v[5] + v[6] + v[7], v[3] + v[4]
pods = {}
f = open(out + '.samples.csv', 'w'); f.write('epoch,pods,ready,terminating,firecracker_vms,host_busy_cores\n')
import os; ncpu = os.cpu_count()
b0, i0 = stat()
while not stop:
    t = time.time()
    try:
        j = json.loads(subprocess.run(['kubectl', 'get', 'pods', '-l', f'serving.knative.dev/service={svc}', '-o', 'json'],
                                      capture_output=True, text=True, timeout=5).stdout)
        items = j['items']; n = len(items); rd = term = 0
        for p in items:
            nm = p['metadata']['name']; d = pods.setdefault(nm, {'creation': p['metadata'].get('creationTimestamp'), 'first_seen_epoch': t})
            if p['metadata'].get('deletionTimestamp'): term += 1; d['deletion'] = p['metadata']['deletionTimestamp']
            for c in (p.get('status', {}).get('conditions') or []):
                if c['type'] == 'Ready' and c['status'] == 'True':
                    rd += 1; d['ready_api'] = c.get('lastTransitionTime'); d.setdefault('first_seen_ready_epoch', t)
    except Exception:
        n = rd = term = -1
    try: vms = len(subprocess.run(['pgrep', '-x', 'firecracker'], capture_output=True, text=True).stdout.split())
    except Exception: vms = -1
    b1, i1 = stat(); db, di = b1 - b0, i1 - i0; b0, i0 = b1, i1
    f.write(f'{t:.3f},{n},{rd},{term},{vms},{(db / (db + di) * ncpu) if db + di else 0:.2f}\n'); f.flush()
    time.sleep(max(0.0, 1.0 - (time.time() - t)))
json.dump(pods, open(out + '.pods.json', 'w'), indent=1)
