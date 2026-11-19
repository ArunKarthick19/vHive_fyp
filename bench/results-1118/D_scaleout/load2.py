#!/usr/bin/env python3
"""load2.py — closed-loop HTTP load: N worker threads, each sending GET requests back to back for DURATION seconds.

usage: load2.py HOST_IP PORT DURATION_S CONCURRENCY PATH HOST_HEADER [--new-conn] [--timeout S] [--json FILE]

Default: each worker keeps ONE persistent keep-alive connection (HTTP/1.1) and reconnects only after a failure or
when the server says it will close. --new-conn: a new TCP connection for every request, closed after the response
(the behaviour of the lost load.py); NOTHING else changes.

Every attempt is counted exactly once, as ok (HTTP 200) or as one error type:
  connect_timeout, read_timeout, refused, reset:ECONNRESET, reset:RemoteDisconnected, reset:EPIPE,
  http_<code> (any status other than 200), other:<exception class>
After a connection-level error the worker closes the connection, waits 10 ms (so a refusing endpoint cannot turn
into a busy loop of millions of errors) and reconnects; the failed request is NOT retried.
Latency percentiles are over the ok (HTTP 200) responses only, measured from just before the request is sent
(including connect time when a new connection is made) to the last body byte.

stdout: CSV header + one row:
  conc,rps,p50_ms,p99_ms,errors,ok,attempts,elapsed_s,conns_opened,mode,err_breakdown,client_cpu_s
rps = ok responses / elapsed seconds. err_breakdown = "type=count;..." (empty if none).
"""
import http.client, json, math, resource, socket, sys, threading, time, errno

args = sys.argv[1:]
new_conn = '--new-conn' in args
timeout = 10.0
jfile = None
pos = []
i = 0
while i < len(args):
    a = args[i]
    if a == '--new-conn': pass
    elif a == '--timeout': i += 1; timeout = float(args[i])
    elif a == '--json': i += 1; jfile = args[i]
    else: pos.append(a)
    i += 1
if len(pos) != 6:
    sys.exit(__doc__)
ip, port, dur, conc, path, host = pos[0], int(pos[1]), float(pos[2]), int(pos[3]), pos[4], pos[5]
headers = {'Host': host}
if new_conn:
    headers['Connection'] = 'close'

lock = threading.Lock()
lat_all, err_all = [], {}
conns_opened = [0]
start = time.monotonic()
deadline = start + dur


def classify(e, phase):
    if isinstance(e, (socket.timeout, TimeoutError)):
        return 'connect_timeout' if phase == 'connect' else 'read_timeout'
    if isinstance(e, ConnectionRefusedError):
        return 'refused'
    if isinstance(e, http.client.RemoteDisconnected):   # subclass of ConnectionResetError: check first
        return 'reset:RemoteDisconnected'
    if isinstance(e, ConnectionResetError):
        return 'reset:ECONNRESET'
    if isinstance(e, BrokenPipeError):
        return 'reset:EPIPE'
    if isinstance(e, OSError) and e.errno == errno.ECONNREFUSED:
        return 'refused'
    return 'other:' + type(e).__name__


def worker():
    lat, err, opened = [], {}, 0
    conn = None
    while True:
        t0 = time.monotonic()
        if t0 >= deadline:
            break
        phase = 'connect'
        try:
            if conn is None:
                conn = http.client.HTTPConnection(ip, port, timeout=timeout)
                conn.connect()
                opened += 1
            phase = 'request'
            conn.request('GET', path, headers=headers)
            r = conn.getresponse()
            r.read()
            t1 = time.monotonic()
            if r.status == 200:
                lat.append((t1 - t0) * 1000.0)
            else:
                k = f'http_{r.status}'
                err[k] = err.get(k, 0) + 1
            if new_conn or r.will_close:
                conn.close(); conn = None
        except Exception as e:  # noqa: BLE001 — every exception is classified, none is hidden
            k = classify(e, phase)
            err[k] = err.get(k, 0) + 1
            try:
                if conn is not None:
                    conn.close()
            except Exception:
                pass
            conn = None
            time.sleep(0.01)
    if conn is not None:
        conn.close()
    with lock:
        lat_all.extend(lat)
        for k, v in err.items():
            err_all[k] = err_all.get(k, 0) + v
        conns_opened[0] += opened


ts = [threading.Thread(target=worker, daemon=True) for _ in range(conc)]
for t in ts:
    t.start()
for t in ts:
    t.join()
elapsed = time.monotonic() - start


def pct(v, p):
    if not v:
        return ''
    v = sorted(v)
    k = min(len(v) - 1, max(0, math.ceil(p / 100.0 * len(v)) - 1))   # nearest-rank
    return f'{v[k]:.2f}'


ok = len(lat_all)
nerr = sum(err_all.values())
ru = resource.getrusage(resource.RUSAGE_SELF)
row = {
    'conc': conc, 'rps': f'{ok / elapsed:.1f}', 'p50_ms': pct(lat_all, 50), 'p99_ms': pct(lat_all, 99),
    'errors': nerr, 'ok': ok, 'attempts': ok + nerr, 'elapsed_s': f'{elapsed:.2f}', 'conns_opened': conns_opened[0],
    'mode': 'new-conn' if new_conn else 'keepalive',
    'err_breakdown': ';'.join(f'{k}={v}' for k, v in sorted(err_all.items())),
    'client_cpu_s': f'{ru.ru_utime + ru.ru_stime:.2f}',
}
print(','.join(row))
print(','.join(str(v) for v in row.values()))
if jfile:
    json.dump({**row, 'errors_by_type': err_all, 'timeout_s': timeout, 'path': path, 'host': host}, open(jfile, 'w'), indent=1)
