#!/usr/bin/env python3
"""probe_container_path.py SNAPDIR ROOTFS — what does the vHive fork's required `container_snapshot_path` do on load?
Loads the snapshot twice in slot 30 (tap/run dir already set up by snapshot_spike.py): once with a non-existent
path (to see the error), once with the rootfs path, then GET /vm/config to see which drive path it changed."""
import http.client, json, os, signal, socket, subprocess, sys, time
SOCK, CON = "/run/vhive-fcweb/30/fc.sock", "/run/vhive-fcweb/30/console.log"
snap, rootfs = sys.argv[1], sys.argv[2]
class U(http.client.HTTPConnection):
    def __init__(s): super().__init__("localhost", timeout=30)
    def connect(s): s.sock = socket.socket(socket.AF_UNIX); s.sock.connect(SOCK)
def api(m, p, b=None):
    c = U(); c.request(m, p, body=json.dumps(b) if b is not None else None, headers={"Content-Type": "application/json"})
    r = c.getresponse(); d = r.read().decode(); c.close(); return r.status, d
for label, csp in (("rootfs", rootfs), ("nonexistent", "/nonexistent/container-snapshot-marker")):
    if os.path.exists(SOCK): os.remove(SOCK)
    p = subprocess.Popen(["/usr/local/bin/firecracker", "--api-sock", SOCK], stdout=open(CON, "w"), stderr=subprocess.STDOUT, start_new_session=True)
    for _ in range(500):
        if os.path.exists(SOCK): break
        time.sleep(0.002)
    time.sleep(0.02)
    code, resp = api("PUT", "/snapshot/load", {"snapshot_path": snap + "/snap_file", "mem_backend": {"backend_type": "File", "backend_path": snap + "/mem_file"},
                                                "container_snapshot_path": csp, "resume_vm": True})
    print(f"== {label}: load -> {code} {resp[:400]}")
    if code < 300:
        c2, cfg = api("GET", "/vm/config")
        try:
            print("   drives after load:", [(d["drive_id"], d["path_on_host"], d["is_read_only"]) for d in json.loads(cfg)["drives"]])
        except Exception as e:
            print("   GET /vm/config:", c2, cfg[:300], e)
    os.killpg(p.pid, signal.SIGKILL); p.wait()
    print("   console tail:", open(CON).read()[-300:].replace("\n", " | "))
