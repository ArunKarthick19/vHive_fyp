#!/usr/bin/env python3
"""window.py FILE T0 [T1]: print the lines of a daemon log/pane capture whose own timestamp is in [T0, T1] (epoch s).
Handles logrus text (time="...") and logrus TTY (DEBU[2026-...]) formats, like stock_breakdown.py. Lines without a
wall-clock timestamp are skipped."""
import re, sys
from datetime import datetime
TS = re.compile(r'^(?:[A-Z]{4}\[([^\]]+)\]|time="([^"]+)")')
def epoch(s):
    m = re.match(r'(\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d)(\.\d+)?(.*)$', s)
    if not m: return None
    return datetime.fromisoformat(m.group(1) + (m.group(2) or '.0')[:7] + m.group(3)).timestamp()
t0 = float(sys.argv[2]); t1 = float(sys.argv[3]) if len(sys.argv) > 3 else 1e12
for l in open(sys.argv[1], errors='replace'):
    m = TS.match(l)
    t = epoch(m.group(1) or m.group(2)) if m else None
    if t is not None and t0 <= t <= t1:
        sys.stdout.write(l)
