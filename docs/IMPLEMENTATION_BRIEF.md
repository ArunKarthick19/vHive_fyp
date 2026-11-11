You are implementing the next stage of my FYP, unattended, overnight. I'm asleep and nobody can answer questions, so make reasonable decisions, record them, and keep going.

FIRST ACTION: save this whole message verbatim to ~/vhive/docs/IMPLEMENTATION_BRIEF.md (create docs/ if needed) and create ~/vhive/docs/STATUS.md. If your context is ever compacted or you feel lost, re-read both files before continuing. Update STATUS.md after every step (done / in progress / next / blockers / decisions).

BACKGROUND
- Repo ~/vhive (fork of vHive), branch per-pod-vm. The stack runs on this single node: Knative + Istio, containerd and firecracker-containerd (tmux sessions "containerd" and "firecracker"), and the vHive daemon (tmux session "vhive", started with: cd ~/vhive && sudo FCWEB_VCPUS=1 FCWEB_MEM_MIB=256 ./vhive-perpod 2>&1 | tee -a /tmp/vhive-logs/vhive_log.txt).
- What exists: cri/firecracker/ has my shared-VM mode (GUEST_IMAGE=firecracker-vm://webvm, slot 0) and per-pod mode (firecracker-vm://pod, slots 1-16, VM torn down on StopPodSandbox, FC_* sizing). In both, the VM boots a PREBUILT disk (~/fc-vm/rootfs.ext4: Alpine + python http.server on :80) with kernel ~/fc-vm/hello-vmlinux.bin. The Knative pod's queue-proxy forwards traffic to the VM's guest IP.
- The limitation: the VM runs ONE baked-in program, so it cannot run an arbitrary function. Stock vHive runs a real container image.

GOAL (the whole job)
Make the per-pod VM path run a REAL function from a container image, so one Knative service image X can run either the stock way or in my Firecracker VM, with the SAME image X, and answer the same. First target: the image used by ~/fc-vm/fcmicro-fn.yaml (read it). Success = two ksvcs with the identical image, one stock and one with the GUEST_IMAGE marker, both answer HTTP with the same response and scale to zero cleanly.

DESIGN: write ~/vhive/docs/DESIGN.md first (under 2 pages, include rejected alternatives) and commit it before coding. Cover:
1. Image -> rootfs builder: from an OCI image ref produce an ext4 root filesystem, in user space if possible (export layers, mkfs.ext4 -d). Cache by image digest. Provide a pre-build command so first-use build time never pollutes cold-start benchmarks.
2. A tiny static init (PID 1) for the guest: mounts /proc /sys /dev, configures the network, applies env/workdir/user from the image config, execs Entrypoint+Cmd, reaps zombies. Evaluate how to deliver config: baked into the rootfs, kernel cmdline, or Firecracker MMDS. Network values come from the slot (guest IP 10.200.0.(4n+2), host 10.200.0.(4n+1)).
3. Writable-ness: prefer one read-only shared rootfs per image plus tmpfs for writable dirs, instead of copying a disk per VM; justify the choice.
4. Integration into cri/firecracker per-pod mode: the image ref comes from the CRI request (or an annotation/env you define), the app's port is reachable by queue-proxy, container env vars are passed in. The existing modes (shared webvm, per-pod with the baked-in disk) must keep working: regression tests.
5. docs/LIMITS.md: what images WON'T work (features missing from the minimal kernel, multi-process images, etc.).

RULES
- Work on a new branch real-image-vm off per-pod-vm. Small commits, one logical step each, tests with each. Push the branch after each commit (git push -u origin real-image-vm); if a push fails, note it and continue. Never push to or modify main, firecracker-guest-prototype or per-pod-vm.
- Do NOT run benchmarks or load tests. I benchmark tomorrow. Functional checks only (a few requests).
- Unit tests must run without root (go test -race ./cri/firecracker/...). For end-to-end tests you MAY use sudo, but ONLY for: (a) replacing the vHive daemon: stop the process in tmux session "vhive" and restart it with the newly built binary using the same command line; keep the last known-good binary as ~/vhive/vhive-perpod.good and never delete it; if a build breaks, restore it; (b) removing stale fcweb<N> tap devices and /run/vhive-fcweb/<N> directories that belong to MY VMs. Never touch the containerd or firecracker tmux sessions, the Kubernetes/Istio/Knative installation, system packages (no apt upgrade), kernel modules, or anything outside ~/vhive, ~/fc-vm and /run/vhive-fcweb. No rm -rf outside those. Nothing that needs a reboot.
- Never print or store secrets (the GitHub token) in files or logs.
- If the stack breaks and you cannot restore it within 20 minutes: restore vhive-perpod.good, record exactly what happened in STATUS.md, and stop.
- Time-box: if one step takes more than about 90 minutes, write down what blocks it, pick the simplest workable alternative, and move on. Stop at 6:00 AM node time and finish the docs.

DELIVERABLES (priority order)
1. docs/DESIGN.md (committed first).
2. Builder + init + integration with unit tests on branch real-image-vm.
3. End-to-end demo: ~/fc-vm/real/stock.yaml and ~/fc-vm/real/vm.yaml using the identical image, both Ready and answering, plus ~/fc-vm/real/check.sh that applies both, curls each, compares responses, and deletes them.
4. ~/fc-vm/real/BENCH.md: exactly how to run my existing harness (~/fc-vm/night-1111/night.sh and its README; read them, don't modify them, copy to ~/fc-vm/night-1112/ if changes are needed) against the new pair, plus prerequisites like the pre-build command.
5. Final docs/STATUS.md: what works, what doesn't, known bugs, decisions, how to reproduce, and what I should check first in the morning.
Explain everything in plain language in the docs. I'm a student and want to understand every decision.
