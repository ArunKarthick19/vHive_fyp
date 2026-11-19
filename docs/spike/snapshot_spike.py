#!/usr/bin/env python3
"""snapshot_spike.py — feasibility spike: boot vs snapshot restore of a firecracker-vm://image VM, outside Kubernetes.

Run as root (it creates a tap and runs firecracker, like vhive does):
  sudo python3 -I snapshot_spike.py <name> <rootfs.ext4> <guest_port> <out_dir> [N]

Slot 30 only (never 1-16, which the daemon owns): tap fcweb30, host 10.200.0.121/30, guest 10.200.0.122,
MAC 06:00:0a:c8:00:7a, run dir /run/vhive-fcweb/30. The VM config mirrors what the daemon writes for an image VM
(cri/firecracker/firecracker.go fcwebConfigJSON + a leftover /run/vhive-fcweb/1/vm-config.json): same kernel, boot args,
read-only rootfs, config drive (/dev/vdb, pod env JSON zero-padded to 4 KiB), writable 4 KiB status drive (/dev/vdc),
1 vCPU / 512 MiB, logger at Warning.

Timing (same method on both sides, like vhive's launchToHTTP200 with probe=tcp): t0 just before the firecracker process
is spawned; t1 = first successful TCP connect to guest:port, polling with a 20 ms dial timeout every 5 ms.
  boot    = spawn `firecracker --api-sock S --config-file C`               -> TCP accept
  restore = spawn `firecracker --api-sock S`, wait for S, PUT /snapshot/load (resume_vm) -> TCP accept
"""
import http.client, json, os, signal, socket, statistics as st, subprocess, sys, time, hashlib

FC = "/usr/local/bin/firecracker"
KERNEL = "/var/lib/firecracker-containerd/runtime/hello-vmlinux.bin"
BOOT_ARGS = ("console=ttyS0 quiet acpi=off random.trust_cpu=on reboot=k panic=1 pci=off nomodules ro "
             "i8042.noaux i8042.nomux i8042.nopnp i8042.dumbkbd init=/.fcinit/init")
SLOT, TAP, HOST_IP, GUEST_IP, MAC = 30, "fcweb30", "10.200.0.121", "10.200.0.122", "06:00:0a:c8:00:7a"
RUN = "/run/vhive-fcweb/30"
SOCK, CFG, CONSOLE = RUN + "/fc.sock", RUN + "/vm-config.json", RUN + "/console.log"
GUESTCFG, STATUS, FCLOG = RUN + "/guest-config.bin", RUN + "/exit-status.bin", RUN + "/firecracker.log"

name, rootfs, port, out = sys.argv[1], sys.argv[2], int(sys.argv[3]), sys.argv[4]
# SPIKE_DRIVES=3 (default): exactly the daemon's image-VM drives (rootfs, config, status).
# SPIKE_DRIVES=2: variant without the status drive; the vHive Firecracker fork's snapshot load asserts 2 drives
# (persist.rs:328) and needs `container_snapshot_path`, which is set to the 2nd drive's own path (the config drive).
DRIVES = int(os.environ.get("SPIKE_DRIVES", "3"))
N = int(sys.argv[5]) if len(sys.argv) > 5 else 10
os.makedirs(out, exist_ok=True)
SNAP, MEM = os.path.join(out, "snap_file"), os.path.join(out, "mem_file")
results = {"name": name, "drives": None, "rootfs": rootfs, "port": port, "boot_ms": [], "restore": {}, "notes": [], "failures": []}


def log(msg):
    line = f"{time.strftime('%T')} [{name}] {msg}"
    print(line, flush=True)
    with open(os.path.join(out, "spike.log"), "a") as f:
        f.write(line + "\n")


def sh(*cmd):
    return subprocess.run(cmd, capture_output=True, text=True)


class UnixHTTP(http.client.HTTPConnection):
    def __init__(self, path):
        super().__init__("localhost", timeout=60)
        self.path = path

    def connect(self):
        self.sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        self.sock.connect(self.path)


def api(method, path, body=None):
    c = UnixHTTP(SOCK)
    c.request(method, path, body=json.dumps(body) if body is not None else None,
              headers={"Content-Type": "application/json", "Accept": "application/json"})
    r = c.getresponse()
    data = r.read().decode(errors="replace")
    c.close()
    return r.status, data


def setup_slot():
    os.makedirs(RUN, exist_ok=True)
    if sh("ip", "link", "show", TAP).returncode != 0:
        for cmd in (["ip", "tuntap", "add", TAP, "mode", "tap"], ["ip", "addr", "add", f"{HOST_IP}/30", "dev", TAP],
                    ["ip", "link", "set", TAP, "up"],
                    ["ip", "neigh", "replace", GUEST_IP, "lladdr", MAC, "dev", TAP, "nud", "permanent"]):
            r = sh(*cmd)
            if r.returncode != 0:
                raise SystemExit(f"{cmd}: {r.stderr}")
    env = [f"GUEST_PORT={port}", "GUEST_IMAGE=firecracker-vm://image", "FC_MEM_MIB=512"]
    data = json.dumps({"env": env, "hostname": f"spike30-{name}"}).encode()
    with open(GUESTCFG, "wb") as f:
        f.write(data + b"\0" * ((len(data) // 4096 + 1) * 4096 - len(data)))
    os.chmod(GUESTCFG, 0o600)
    cfg = {
        "boot-source": {"kernel_image_path": KERNEL, "boot_args": f"{BOOT_ARGS} fcweb.ip={GUEST_IP}/30 fcweb.gw={HOST_IP}"},
        "drives": [
            {"drive_id": "rootfs", "path_on_host": rootfs, "is_root_device": True, "is_read_only": True},
            {"drive_id": "config", "path_on_host": GUESTCFG, "is_root_device": False, "is_read_only": True},
            {"drive_id": "status", "path_on_host": STATUS, "is_root_device": False, "is_read_only": False},
        ][:DRIVES],
        "network-interfaces": [{"iface_id": "eth0", "guest_mac": MAC, "host_dev_name": TAP}],
        "machine-config": {"vcpu_count": 1, "mem_size_mib": 512},
        "logger": {"log_path": FCLOG, "level": "Warning"},
    }
    with open(CFG, "w") as f:
        json.dump(cfg, f, indent=2)


def fresh_files():
    for p in (SOCK,):
        if os.path.exists(p):
            os.remove(p)
    open(FCLOG, "w").close()
    with open(STATUS, "wb") as f:
        f.write(b"\0" * 4096)
    os.chmod(STATUS, 0o600)


def wait_tcp(deadline_s=30.0):
    end = time.perf_counter() + deadline_s
    while time.perf_counter() < end:
        s = socket.socket()
        s.settimeout(0.02)
        try:
            s.connect((GUEST_IP, port))
            s.close()
            return time.perf_counter()
        except OSError:
            s.close()
            time.sleep(0.005)
    return None


def spawn(args, console_mode="w"):
    con = open(CONSOLE, console_mode)
    t0 = time.perf_counter()
    p = subprocess.Popen([FC] + args, stdout=con, stderr=con, start_new_session=True)
    con.close()
    return p, t0


def kill(p):
    if p and p.poll() is None:
        os.killpg(p.pid, signal.SIGKILL)
        p.wait(10)
    if os.path.exists(SOCK):
        os.remove(SOCK)


def http_get():
    c = http.client.HTTPConnection(GUEST_IP, port, timeout=10)
    c.request("GET", "/")
    r = c.getresponse()
    body = r.read()
    hdrs = dict(r.getheaders())
    c.close()
    return r.status, body, hdrs


def wait_sock(p, deadline_s=5.0):
    end = time.perf_counter() + deadline_s
    while time.perf_counter() < end:
        if os.path.exists(SOCK):
            try:
                s = socket.socket(socket.AF_UNIX)
                s.connect(SOCK)
                s.close()
                return True
            except OSError:
                pass
        if p.poll() is not None:
            return False
        time.sleep(0.001)
    return False


def boot_once(i):
    fresh_files()
    p, t0 = spawn(["--api-sock", SOCK, "--config-file", CFG])
    t1 = wait_tcp()
    ms = None if t1 is None else (t1 - t0) * 1000
    if ms is None:
        results["failures"].append(f"boot {i}: no TCP accept in 30 s; console tail: {open(CONSOLE).read()[-400:]!r}")
    return p, ms


def restore_once(i, variant):
    if os.path.exists(SOCK):
        os.remove(SOCK)
    p, t0 = spawn(["--api-sock", SOCK])
    if not wait_sock(p):
        results["failures"].append(f"restore[{variant}] {i}: API socket never came up; console: {open(CONSOLE).read()[-400:]!r}")
        kill(p)
        return None, None, None
    t_sock = time.perf_counter()
    if variant == "mem_backend_File":
        body = {"snapshot_path": SNAP, "mem_backend": {"backend_type": "File", "backend_path": MEM}, "resume_vm": True}
    else:  # legacy field
        body = {"snapshot_path": SNAP, "mem_file_path": MEM, "resume_vm": True}
    body["container_snapshot_path"] = GUESTCFG  # required by the vHive fork; the 2nd drive's own path
    try:
        code, resp = api("PUT", "/snapshot/load", body)
    except Exception as e:  # noqa: BLE001 — e.g. Firecracker panics and drops the connection
        code, resp = 599, f"{type(e).__name__}: {e}; console: {open(CONSOLE).read()[-500:]!r}"
    t_load = time.perf_counter()
    if code >= 300:
        results["failures"].append(f"restore[{variant}] {i}: load HTTP {code}: {resp}")
        kill(p)
        return None, None, None
    t1 = wait_tcp()
    if t1 is None:
        results["failures"].append(f"restore[{variant}] {i}: loaded but no TCP accept in 30 s")
        return p, None, None
    return p, (t1 - t0) * 1000, {"spawn_to_sock_ms": (t_sock - t0) * 1000, "load_call_ms": (t_load - t_sock) * 1000,
                                 "load_to_tcp_ms": (t1 - t_load) * 1000}


def summary(v):
    v = [x for x in v if x is not None]
    if not v:
        return "n=0"
    return f"n={len(v)} median={st.median(v):.1f} min={min(v):.1f} max={max(v):.1f} mean={st.mean(v):.1f}"


def main():
    results["drives"] = DRIVES
    setup_slot()
    log(f"slot {SLOT} ready: tap {TAP} {HOST_IP}/30, guest {GUEST_IP}:{port}, rootfs {rootfs}")
    # 1. boots
    ref_body = None
    for i in range(1, N + 1):
        p, ms = boot_once(i)
        if ms is not None and ref_body is None:
            time.sleep(0.3)
            code, body, hdrs = http_get()
            ref_body = body
            results["boot_http"] = {"status": code, "bytes": len(body), "sha16": hashlib.sha256(body).hexdigest()[:16],
                                    "date_hdr": hdrs.get("Date"), "host_time": time.strftime("%a, %d %b %Y %H:%M:%S GMT", time.gmtime())}
        results["boot_ms"].append(ms)
        log(f"boot {i}: {ms if ms is None else round(ms, 1)} ms")
        kill(p)
    log("boot " + summary(results["boot_ms"]))

    # 2. one boot, wait until the app answers, pause, Full snapshot
    p, ms = boot_once(0)
    time.sleep(1.0)
    code, body, hdrs = http_get()
    log(f"snapshot source VM booted in {ms:.1f} ms; GET / -> {code}, {len(body)} B")
    t = time.perf_counter()
    c1 = api("PATCH", "/vm", {"state": "Paused"})
    t_pause = time.perf_counter()
    c2 = api("PUT", "/snapshot/create", {"snapshot_type": "Full", "snapshot_path": SNAP, "mem_file_path": MEM})
    t_snap = time.perf_counter()
    results["snapshot"] = {"pause": c1, "create": c2, "pause_ms": (t_pause - t) * 1000, "create_ms": (t_snap - t_pause) * 1000,
                           "snap_file_bytes": os.path.getsize(SNAP) if os.path.exists(SNAP) else None,
                           "mem_file_bytes": os.path.getsize(MEM) if os.path.exists(MEM) else None,
                           "mem_file_du": sh("du", "-h", MEM).stdout.split("\t")[0] if os.path.exists(MEM) else None,
                           "created_at": time.strftime("%T")}
    log(f"pause {c1[0]} {(t_pause - t) * 1000:.0f} ms; create {c2[0]} {(t_snap - t_pause) * 1000:.0f} ms {c2[1][:200]}; sizes {results['snapshot']}")
    kill(p)
    if c2[0] >= 300:
        results["failures"].append(f"snapshot create failed: {c2}")
        return

    # 3./4. restores, two file-backed ways of passing the memory file
    for variant in ("mem_backend_File", "mem_file_path_legacy"):
        times, parts = [], []
        for i in range(1, N + 1):
            p, ms, part = restore_once(i, variant)
            times.append(ms)
            parts.append(part)
            log(f"restore[{variant}] {i}: {ms if ms is None else round(ms, 1)} ms {part or ''}")
            if i == 1 and ms is not None:
                time.sleep(0.3)
                try:
                    code, body, hdrs = http_get()
                    results.setdefault("restore_http", {})[variant] = {
                        "status": code, "bytes": len(body), "sha16": hashlib.sha256(body).hexdigest()[:16],
                        "same_body_as_boot": body == ref_body, "date_hdr": hdrs.get("Date"),
                        "host_time": time.strftime("%a, %d %b %Y %H:%M:%S GMT", time.gmtime())}
                except Exception as e:  # noqa: BLE001 — recorded, not hidden
                    results["failures"].append(f"restore[{variant}] verify GET: {type(e).__name__}: {e}")
                # a second request a few seconds later: does the VM keep serving?
                time.sleep(3)
                try:
                    code2, body2, _ = http_get()
                    results["restore_http"][variant]["after_3s"] = {"status": code2, "same_body": body2 == ref_body}
                except Exception as e:  # noqa: BLE001
                    results["failures"].append(f"restore[{variant}] GET after 3 s: {type(e).__name__}: {e}")
                with open(os.path.join(out, f"console.restore-{variant}.log"), "w") as f:
                    f.write(open(CONSOLE).read())
            kill(p)
        results["restore"][variant] = {"ms": times, "parts": parts}
        log(f"restore[{variant}] " + summary(times))
    # an unsupported backend type, to record what the API accepts (Uffd needs a page-fault handler process; not tested)
    p, _ = spawn(["--api-sock", SOCK])
    wait_sock(p)
    try:
        results["notes"].append({"probe_Uffd_without_handler": api("PUT", "/snapshot/load", {
            "snapshot_path": SNAP, "mem_backend": {"backend_type": "Uffd", "backend_path": RUN + "/no-handler.sock"},
            "container_snapshot_path": GUESTCFG, "resume_vm": True})})
    except Exception as e:  # noqa: BLE001
        results["notes"].append({"probe_Uffd_without_handler": f"{type(e).__name__}: {e}"})
    kill(p)


try:
    main()
finally:
    with open(os.path.join(out, "results.json"), "w") as f:
        json.dump(results, f, indent=1, default=str)
    log("wrote results.json")
