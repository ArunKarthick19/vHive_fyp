#!/usr/bin/env python3
"""pidwatch.py OUTFILE — every 20 ms list processes whose comm is 'firecracker'; log each appearance and exit:
<epoch> <iso time> appear|exit pid=<p> ppid=<pp> parent=<parent comm> args=<cmdline>   (exit time resolution ~20 ms)."""
import os, sys, time, datetime
out = open(sys.argv[1], 'a', buffering=1)
def comm(p):
    try: return open(f'/proc/{p}/comm').read().strip()
    except OSError: return ''
def info(p):
    try:
        st = open(f'/proc/{p}/stat').read(); pp = st.rsplit(')', 1)[1].split()[1]
        args = open(f'/proc/{p}/cmdline', 'rb').read().replace(b'\0', b' ').decode(errors='replace').strip()
        return f'ppid={pp} parent={comm(pp)} args={args}'
    except OSError: return 'gone-before-read'
def ts(): t = time.time(); return f'{t:.3f} {datetime.datetime.fromtimestamp(t).isoformat(timespec="milliseconds")}'
seen = {}
out.write(f'{ts()} start\n')
while True:
    now = {p for p in os.listdir('/proc') if p.isdigit() and comm(p) == 'firecracker'}
    for p in now - seen.keys(): seen[p] = info(p); out.write(f'{ts()} appear pid={p} {seen[p]}\n')
    for p in set(seen) - now: out.write(f'{ts()} exit pid={p} {seen.pop(p)}\n')
    time.sleep(0.02)
