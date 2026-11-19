# DRAWBACKS_PLAN: fixing known drawbacks of `firecracker-vm://image`

Branch `drawbacks-1119` (from `real-image-vm`). Progress log: `~/fc-vm/DRAWBACKS-LOG.md`.

## 1. Crash detection (implemented on this branch)

### How it works today
- A Knative pod has two containers. For `firecracker-vm://image`, vhive's CRI service (`cri/firecracker/service.go`) does two things in
  `CreateContainer(user-container)`. It asks containerd to create the normal **placeholder** user container (it runs the same image *on the host*
  and is never sent traffic), and it boots the pod's own Firecracker VM (`podvm.go`, `firecracker.go`). queue-proxy is then pointed at the VM.
- Inside the VM, `fcinit` (PID 1) starts the app. When the app exits, fcinit logs `app exited (status N)` on the serial console and reboots.
  With `reboot=k` the Firecracker process then exits.
- **Nobody watches for that exit.** `firecrackerVM.boot` only closes an internal `exited` channel. The kubelet asks containerd about the
  *placeholder* (`ContainerStatus` is forwarded unchanged by `cri/proxy.go`), and the placeholder is still running. So the pod stays `Running`,
  queue-proxy's probe starts failing, and nothing restarts the app.

### What changes
1. **fcinit records the exit (guest, `cmd/fcinit`, `guest/`).** Each image VM gets a third virtio disk, a 4 KiB writable **status drive** (`/dev/vdc`,
   host file `/run/vhive-fcweb/<slot>/exit-status.bin`, mode 0600, zeroed at boot). When the app exits, fcinit writes
   `{"exitCode":3,"reason":"Exited"}` (a signal gives `128+signo`, as for containers) to it, fsyncs it, logs it on the console and powers off.
   A failure of fcinit itself writes `reason: InitError`. The format is a pure function in package `guest` (encode/decode/wait-status mapping).
   *Why a drive and not a console line:* the app's output goes to the same console, so an app could print a fake line. The drive is written only by fcinit.
2. **vhive notices the exit (`podvm.go`).** For VMs booted with the status drive (image VMs only), a goroutine waits on the Firecracker
   process's exit channel. If the exit was not caused by vhive's own teardown (StopPodSandbox or a failed create), it reads the status drive and the console tail
   and calls the service.
3. **vhive tells Kubernetes (`service.go`, `cri/proxy.go`).** The service remembers which user container belongs to the sandbox, records
   the exit for that container ID, and **stops the placeholder** through containerd (`StopContainer`, timeout 0). containerd then really reports the container as EXITED
   (so `ListContainers`/PLEG see it). `ContainerStatus` is patched for *that container ID only*: `exitCode` = the app's code, `reason` = `Error` (or
   `Completed` for 0, `VMExited` if the drive was empty, e.g. kernel panic), `message` = a short console tail. kubelet then applies the pod's restartPolicy
   (`Always` for Knative): it creates a new user container in the same sandbox. vhive's existing code boots a fresh VM in the same slot for it, so `RESTARTS` goes up
   and `kubectl describe` shows `Last State: Terminated, Exit Code: 3`.
4. **What does not change:** `firecracker-vm://webvm`, `firecracker-vm://pod` and the stock path never get a status drive, no watcher and no patched status.
   Their Firecracker configs stay byte-identical (golden test). `cri/proxy.go` patches only through an optional interface that only image-VM container IDs hit.

### How it is tested
- **Unit (no root):** guest exit-status encode/decode and wait-status → code mapping; VM config JSON with the status drive (and unchanged golden files for
  the baked modes); podVMs watcher with a fake VM (exit → one report; teardown → no report; a newer boot is not confused with an old exit; baked mode → no watcher);
  the service records the exit, stops the right placeholder, and patches only EXITED statuses of tracked IDs.
- **End to end:** a test image `dev.local/fcexit:1` (crccheck plus a guarded cmd): inside the VM (`/.fcinit` exists) it serves for 20 s, then `exit 3`;
  the host placeholder (no `/.fcinit`) serves forever, so it cannot fake the result. The same service is run with the **old** binary (expect: stays Running,
  0 restarts) and the **new** binary (expect: restarts, Exit Code 3 in describe). Before every push: `go test -race ./cri/firecracker/...`, `check-1118.sh` with
  `VM_YAML=vm-512.yaml`, and `regress.sh`.
