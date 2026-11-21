#!/usr/bin/env python3
"""vary.py — OPEN-LOOP time-varying load: requests are sent on a fixed schedule (rate steps), whether or not earlier
requests have finished (each request runs in its own worker from a large pool, so a slow cold start does not delay
the schedule). Records every request and a per-second table with the number of pods and firecracker VMs alive.

usage: vary.py --ip IP --host HOST --steps 1:30,5:30,20:30,5:30,1:30 --out DIR --svc KSVC
               (--http PATH | --grpc FN)   [--timeout S] [--workers N]
  --http PATH : HTTP/1.1 GET PATH, Host: HOST; each worker thread keeps one keep-alive connection.
  --grpc FN   : FN in fibgo|fibpy|aesgo|authgo; ONE shared gRPC channel (HTTP/2, :authority HOST), like a normal gRPC client.
Outputs in DIR: requests.csv (one row per scheduled request), seconds.csv (per second), summary.json.
Latency is from the scheduled send time (so client-side queueing, if any, is included) to the full response.
A request that is not finished within --timeout counts as an error (timeout); nothing is retried.
"""
import argparse, concurrent.futures as cf, http.client, json, os, subprocess, sys, threading, time

ap = argparse.ArgumentParser()
ap.add_argument('--ip', required=True); ap.add_argument('--host', required=True); ap.add_argument('--svc', required=True)
ap.add_argument('--steps', required=True); ap.add_argument('--out', required=True)
ap.add_argument('--http'); ap.add_argument('--grpc')
ap.add_argument('--timeout', type=float, default=60.0); ap.add_argument('--workers', type=int, default=400)
a = ap.parse_args()
os.makedirs(a.out, exist_ok=True)
steps = [(float(r), float(d)) for r, d in (s.split(':') for s in a.steps.split(','))]

# ---- request functions ---------------------------------------------------------------------------------------------
tl = threading.local()
if a.http:
    def do_req():
        c = getattr(tl, 'c', None)
        try:
            if c is None:
                c = tl.c = http.client.HTTPConnection(a.ip, 80, timeout=a.timeout)
            c.request('GET', a.http, headers={'Host': a.host})
            r = c.getresponse(); r.read()
            if r.will_close:
                c.close(); tl.c = None
            return 'ok' if r.status == 200 else f'http_{r.status}'
        except Exception as e:  # noqa: BLE001
            try: c and c.close()
            except Exception: pass
            tl.c = None
            return 'timeout' if 'timed out' in str(e) else 'err:' + type(e).__name__
else:
    import grpc
    sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), 'pb'))
    if a.grpc in ('fibgo', 'fibpy'):
        from fibonacci import fibonacci_pb2 as pb, fibonacci_pb2_grpc as pbg
        mk = lambda ch: (pbg.GreeterStub(ch).SayHello, pb.HelloRequest(name='10'))
    elif a.grpc == 'aesgo':
        from aes import aes_pb2 as pb, aes_pb2_grpc as pbg
        mk = lambda ch: (pbg.AesStub(ch).ShowEncryption, pb.PlainTextMessage(plaintext_message='world'))
    elif a.grpc == 'authgo':
        from auth import auth_pb2 as pb, auth_pb2_grpc as pbg
        mk = lambda ch: (pbg.GreeterStub(ch).SayHello, pb.HelloRequest(name='allow'))
    else:
        sys.exit('unknown --grpc fn')
    channel = grpc.insecure_channel(f'{a.ip}:80', options=[('grpc.default_authority', a.host)])
    call, msg = mk(channel)

    def do_req():
        try:
            call(msg, timeout=a.timeout)
            return 'ok'
        except grpc.RpcError as e:
            return 'grpc_' + e.code().name
        except Exception as e:  # noqa: BLE001
            return 'err:' + type(e).__name__

# ---- sampler: pods + VMs every second -------------------------------------------------------------------------------
samples = []
stop = threading.Event()
def sampler():
    while not stop.is_set():
        t = time.monotonic()
        try:
            out = subprocess.run(['kubectl', 'get', 'pods', '--no-headers', '-l', f'serving.knative.dev/service={a.svc}'],
                                 capture_output=True, text=True, timeout=5).stdout
            pods = len(out.strip().splitlines()) if out.strip() else 0
            ready = sum(1 for l in out.splitlines() if l.split()[1:2] and l.split()[1].split('/')[0] == l.split()[1].split('/')[1])
        except Exception:
            pods = ready = -1
        try:
            vms = len(subprocess.run(['pgrep', '-x', 'firecracker'], capture_output=True, text=True).stdout.split())
        except Exception:
            vms = -1
        samples.append((t, pods, ready, vms))
        stop.wait(max(0.0, 1.0 - (time.monotonic() - t)))

# ---- run -----------------------------------------------------------------------------------------------------------
rows = []
lock = threading.Lock()
def run_one(i, step, rate, sched):
    res = do_req()
    done = time.monotonic()
    with lock:
        rows.append((i, step, rate, sched - T0, done - T0, (done - sched) * 1000.0, res))

st = threading.Thread(target=sampler, daemon=True); st.start()
time.sleep(1.2)
pool = cf.ThreadPoolExecutor(max_workers=a.workers)
T0 = time.monotonic()
wall0 = time.time()
late = 0
i = 0
t = T0
for si, (rate, dur) in enumerate(steps):
    n = int(round(rate * dur))
    for k in range(n):
        sched = t + k / rate
        now = time.monotonic()
        if sched > now:
            time.sleep(sched - now)
        elif now - sched > 0.05:
            late += 1
        pool.submit(run_one, i, si, rate, sched)
        i += 1
    t += dur
pool.shutdown(wait=True)
T_end = time.monotonic()
time.sleep(1.2)
stop.set(); st.join(timeout=3)

rows.sort()
with open(os.path.join(a.out, 'requests.csv'), 'w') as f:
    f.write('i,step,rate,sched_s,done_s,latency_ms,result\n')
    for r in rows:
        f.write(f'{r[0]},{r[1]},{r[2]},{r[3]:.4f},{r[4]:.4f},{r[5]:.2f},{r[6]}\n')

def pct(v, p):
    if not v: return ''
    v = sorted(v); return f'{v[min(len(v) - 1, max(0, -(-len(v) * p // 100) - 1))]:.2f}'

nsec = int(T_end - T0) + 1
with open(os.path.join(a.out, 'seconds.csv'), 'w') as f:
    f.write('sec,target_rps,sent,ok,errors,p50_ms,max_ms,pods,pods_ready,firecracker_vms\n')
    edges = []; acc = 0
    for rate, dur in steps: edges.append((acc, acc + dur, rate)); acc += dur
    for s in range(nsec):
        sent = [r for r in rows if s <= r[3] < s + 1]
        okl = [r[5] for r in sent if r[6] == 'ok']
        tgt = next((e[2] for e in edges if e[0] <= s < e[1]), 0)
        smp = [x for x in samples if s <= x[0] - T0 < s + 1]
        p, pr, v = (smp[0][1], smp[0][2], smp[0][3]) if smp else ('', '', '')
        f.write(f'{s},{tgt},{len(sent)},{len(okl)},{len(sent) - len(okl)},{pct(okl, 50)},{f'{max(okl):.2f}' if okl else ''},{p},{pr},{v}\n')

errs = {}
for r in rows:
    if r[6] != 'ok': errs[r[6]] = errs.get(r[6], 0) + 1
per_step = []
for si, (rate, dur) in enumerate(steps):
    sr = [r for r in rows if r[1] == si]; ok = [r[5] for r in sr if r[6] == 'ok']
    per_step.append({'step': si, 'rate': rate, 'dur_s': dur, 'sent': len(sr), 'ok': len(ok), 'p50_ms': pct(ok, 50), 'p99_ms': pct(ok, 99),
                     'max_ms': f'{max(ok):.2f}' if ok else ''})
summ = {'svc': a.svc, 'host': a.host, 'mode': 'http' if a.http else 'grpc', 'steps': a.steps, 'start_epoch': wall0,
        'sent': len(rows), 'ok': sum(1 for r in rows if r[6] == 'ok'), 'errors_by_type': errs, 'late_sends_over_50ms': late,
        'per_step': per_step, 'max_pods': max((x[1] for x in samples), default=-1), 'max_vms': max((x[3] for x in samples), default=-1),
        'first_ok_latency_ms': next((f'{r[5]:.2f}' for r in rows if r[6] == 'ok'), ''),
        'client_cpu_s': round(sum(os.times()[:2]), 2), 'elapsed_s': round(T_end - T0, 2)}
json.dump(summ, open(os.path.join(a.out, 'summary.json'), 'w'), indent=1)
print(json.dumps(summ))
