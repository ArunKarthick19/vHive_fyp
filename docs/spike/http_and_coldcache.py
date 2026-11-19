#!/usr/bin/env python3
"""http_and_coldcache.py NAME ROOTFS PORT SNAPDIR [N] — supplement to snapshot_spike.py (2-drive variant, slot 30).
For boot and restore it measures spawn -> TCP accept AND spawn -> first complete HTTP 200 on GET /, in two conditions:
  warm: page cache as left by the previous run;  cold: `sync; echo 3 > /proc/sys/vm/drop_caches` right before each run
(drops the host page cache, so the rootfs (boot) or the snapshot memory file (restore) must be read from disk)."""
import http.client, json, os, signal, socket, statistics as st, subprocess, sys, time
FC = "/usr/local/bin/firecracker"; RUN = "/run/vhive-fcweb/30"; SOCK, CFG, CON = RUN + "/fc.sock", RUN + "/vm-config.json", RUN + "/console.log"
GUESTCFG, STATUS = RUN + "/guest-config.bin", RUN + "/exit-status.bin"
GUEST_IP = "10.200.0.122"
name, rootfs, port, snapdir = sys.argv[1], sys.argv[2], int(sys.argv[3]), sys.argv[4]
N = int(sys.argv[5]) if len(sys.argv) > 5 else 5
KERNEL = "/var/lib/firecracker-containerd/runtime/hello-vmlinux.bin"
ARGS = ("console=ttyS0 quiet acpi=off random.trust_cpu=on reboot=k panic=1 pci=off nomodules ro i8042.noaux i8042.nomux "
        "i8042.nopnp i8042.dumbkbd init=/.fcinit/init fcweb.ip=10.200.0.122/30 fcweb.gw=10.200.0.121")
json.dump({"boot-source": {"kernel_image_path": KERNEL, "boot_args": ARGS},
           "drives": [{"drive_id": "rootfs", "path_on_host": rootfs, "is_root_device": True, "is_read_only": True},
                      {"drive_id": "config", "path_on_host": GUESTCFG, "is_root_device": False, "is_read_only": True}],
           "network-interfaces": [{"iface_id": "eth0", "guest_mac": "06:00:0a:c8:00:7a", "host_dev_name": "fcweb30"}],
           "machine-config": {"vcpu_count": 1, "mem_size_mib": 512},
           "logger": {"log_path": RUN + "/firecracker.log", "level": "Warning"}}, open(CFG, "w"), indent=2)
env = [f"GUEST_PORT={port}", "GUEST_IMAGE=firecracker-vm://image", "FC_MEM_MIB=512"]
d = json.dumps({"env": env, "hostname": f"spike30-{name}"}).encode()
open(GUESTCFG, "wb").write(d + b"\0" * ((len(d) // 4096 + 1) * 4096 - len(d)))
class U(http.client.HTTPConnection):
    def __init__(s): super().__init__("localhost", timeout=30)
    def connect(s): s.sock = socket.socket(socket.AF_UNIX); s.sock.connect(SOCK)
def api(m, p, b):
    c = U(); c.request(m, p, body=json.dumps(b), headers={"Content-Type": "application/json"}); r = c.getresponse(); r.read(); c.close(); return r.status
def wait_tcp():
    end = time.perf_counter() + 30
    while time.perf_counter() < end:
        s = socket.socket(); s.settimeout(0.02)
        try: s.connect((GUEST_IP, port)); s.close(); return time.perf_counter()
        except OSError: s.close(); time.sleep(0.005)
def first_200():
    end = time.perf_counter() + 30
    while time.perf_counter() < end:
        try:
            c = http.client.HTTPConnection(GUEST_IP, port, timeout=5); c.request("GET", "/"); r = c.getresponse(); r.read(); c.close()
            if r.status == 200: return time.perf_counter()
        except OSError: time.sleep(0.005)
def drop(): subprocess.run(["sync"]); open("/proc/sys/vm/drop_caches", "w").write("3\n")
def run(kind, cold):
    if os.path.exists(SOCK): os.remove(SOCK)
    if cold: drop()
    con = open(CON, "w"); t0 = time.perf_counter()
    if kind == "boot":
        p = subprocess.Popen([FC, "--api-sock", SOCK, "--config-file", CFG], stdout=con, stderr=con, start_new_session=True)
    else:
        p = subprocess.Popen([FC, "--api-sock", SOCK], stdout=con, stderr=con, start_new_session=True)
        while not os.path.exists(SOCK): time.sleep(0.0005)
        time.sleep(0.001)
        code = api("PUT", "/snapshot/load", {"snapshot_path": snapdir + "/snap_file", "mem_backend": {"backend_type": "File",
                   "backend_path": snapdir + "/mem_file"}, "container_snapshot_path": GUESTCFG, "resume_vm": True})
        if code >= 300: print("load failed", code)
    con.close()
    t_tcp = wait_tcp(); t_http = first_200()
    os.killpg(p.pid, signal.SIGKILL); p.wait()
    return (None if t_tcp is None else (t_tcp - t0) * 1000), (None if t_http is None else (t_http - t0) * 1000)
res = {}
for kind in ("boot", "restore"):
    for cold in (False, True):
        v = [run(kind, cold) for _ in range(N)]
        key = f"{kind}_{'cold' if cold else 'warm'}"
        res[key] = v
        tcp = [a for a, b in v if a is not None]; htp = [b for a, b in v if b is not None]
        print(f"{name} {key:13s} n={len(v)} tcp median {st.median(tcp):7.1f} min {min(tcp):7.1f} max {max(tcp):7.1f} | "
              f"first HTTP 200 median {st.median(htp):7.1f} min {min(htp):7.1f} max {max(htp):7.1f} ms", flush=True)
json.dump(res, open(os.path.join(snapdir, "http_and_coldcache.json"), "w"), indent=1)
