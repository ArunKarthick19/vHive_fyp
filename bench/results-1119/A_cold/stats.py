#!/usr/bin/env python3
"""stats.py cold.csv [stock_service mine_service] -> markdown summary (cycles 2..N only; warm-up excluded).
Timing stats use every non-warm-up cycle that got an HTTP response (any code); non-200s and failures are listed separately."""
import csv, statistics as st, sys
rows = list(csv.DictReader(open(sys.argv[1])))
svcs = sys.argv[2:] or list(dict.fromkeys(r["service"] for r in rows))
def summ(s):
    rs = [r for r in rows if r["service"] == s and r["warmup"] == "0"]
    t = [float(r["time_total_s"]) for r in rs if r["time_total_s"] and r["curl_exit"] == "0"]
    bad = [r for r in rs if r["http_code"] != "200" or r["curl_exit"] != "0" or r["idle_ok"] != "1"]
    return rs, t, bad
out = ["| service | n (cycles 2+) | n timed | mean s | median s | min s | max s | stdev s | non-200/failed |", "|---|---|---|---|---|---|---|---|---|"]
res = {}
for s in svcs:
    rs, t, bad = summ(s)
    res[s] = t
    if t:
        out.append(f"| {s} | {len(rs)} | {len(t)} | {st.mean(t):.3f} | {st.median(t):.3f} | {min(t):.3f} | {max(t):.3f} | {st.stdev(t) if len(t)>1 else 0:.3f} | {len(bad)} |")
    else:
        out.append(f"| {s} | {len(rs)} | 0 | - | - | - | - | - | {len(bad)} |")
if len(svcs) == 2 and all(res[s] for s in svcs):
    a, b = res[svcs[0]], res[svcs[1]]
    out.append("")
    out.append(f"ratio {svcs[0]}/{svcs[1]}: mean {st.mean(a)/st.mean(b):.2f}x, median {st.median(a)/st.median(b):.2f}x")
out.append("")
out.append("all cycles (warm-up flagged):")
out.append("")
out.append("| service | cycle | warmup | HTTP | curl_exit | time_total s | idle_wait s | bytes | sha256/16 | vm boot (vhive log) | note |")
out.append("|---|---|---|---|---|---|---|---|---|---|---|")
for r in rows:
    out.append(f"| {r['service']} | {r['cycle']} | {r['warmup']} | {r['http_code']} | {r['curl_exit']} | {r['time_total_s']} | {r['idle_wait_s']} | {r['body_bytes']} | {r['body_sha256_16']} | {r['vm_boot_line']} | {r['note']} |")
print("\n".join(out))
