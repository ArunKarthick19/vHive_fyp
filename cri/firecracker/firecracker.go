// MIT License
//
// Copyright (c) 2023 Georgiy Lebedev, Plamen Petrov, Nathaniel Tornow and vHive team
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package firecracker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/vishvananda/netlink"
)

// Prototype: a standalone Firecracker VM (own kernel + Alpine rootfs running
// `python3 -m http.server 80`) launched directly, without firecracker-containerd.
// Port of ~/fc-vm/fcvm.sh; the rootfs is built by `fcvm.sh build`.
//
// Networking avoids vHive's ranges (172.16-18/16) and k8s's (192.168/16,
// 10.96/12): tap fcwebTap in the root netns with a /30.
const (
	fcwebBin     = "/usr/local/bin/firecracker"
	fcwebKernel  = "/var/lib/firecracker-containerd/runtime/hello-vmlinux.bin"
	fcwebRunDir  = "/run/vhive-fcweb"
	fcwebSock    = fcwebRunDir + "/fc.sock"
	fcwebTap     = "fcweb0"
	fcwebHostIP  = "10.200.0.1"
	fcwebGuestIP = "10.200.0.2"
	fcwebMask    = 30
	fcwebMAC     = "06:00:0a:c8:00:02"

	// acpi=off: Firecracker 1.13 describes devices via ACPI, which this 5.10
	// kernel cannot parse, so it would never find the root disk.
	// random.trust_cpu=on: python reads urandom right after boot.
	fcwebBootArgs = "console=ttyS0 quiet acpi=off random.trust_cpu=on reboot=k panic=1 pci=off nomodules ro " +
		"i8042.noaux i8042.nomux i8042.nopnp i8042.dumbkbd init=/sbin/fcinit"

	fcwebBootTimeout  = 30 * time.Second
	fcwebPollInterval = 5 * time.Millisecond
	fcwebStopTimeout  = 2 * time.Second
)

// firecrackerVM is a single Firecracker VM shared by every pod of a function.
// It is booted when the first pod needs it and torn down (process, tap and API
// socket) when the last pod using it is removed.
type firecrackerVM struct {
	// Serializes boot and teardown so a pod being removed cannot tear the VM
	// down while a new pod is booting it.
	sync.Mutex

	port   string
	refs   int
	cmd    *exec.Cmd
	exited chan struct{} // closed once cmd has exited and been reaped
	cfg    fcwebConfig   // size/image the running VM was booted with
}

func newFirecrackerVM(port string) *firecrackerVM {
	return &firecrackerVM{port: port}
}

func (vm *firecrackerVM) logger() *log.Entry {
	return log.WithField("firecrackerVM", fcwebTap)
}

// acquire boots the VM if it is not running, waits until its guest port answers
// HTTP and returns the guest IP. Each successful acquire must be paired with a
// release. On failure nothing is left behind.
func (vm *firecrackerVM) acquire(ctx context.Context) (string, error) {
	return vm.acquireWith(ctx, defaultFcwebConfig())
}

// acquireWith is acquire with an explicit VM size/image (per-function config).
func (vm *firecrackerVM) acquireWith(ctx context.Context, cfg fcwebConfig) (string, error) {
	vm.Lock()
	defer vm.Unlock()

	if vm.running() {
		if cfg != vm.cfg {
			return "", fmt.Errorf("fcweb VM is running with %+v but this pod wants %+v; it will boot once the current VM is released", vm.cfg, cfg)
		}
		vm.refs++
		vm.logger().WithField("refs", vm.refs).Info("Firecracker VM already running")
		return fcwebGuestIP, nil
	}
	if vm.refs > 0 {
		vm.logger().WithField("refs", vm.refs).Warn("Firecracker VM exited while in use, rebooting")
	}

	// Leftovers of a previous vHive run, or of a VM that died under us.
	start := time.Now()
	if err := vm.cleanup(); err != nil {
		return "", fmt.Errorf("cleaning up stale Firecracker VM state: %w", err)
	}

	vm.cfg = cfg
	if err := vm.boot(ctx); err != nil {
		if cerr := vm.cleanup(); cerr != nil {
			vm.logger().WithError(cerr).Error("failed to clean up after failed boot")
		}
		return "", err
	}

	vm.refs++
	vm.logger().WithFields(log.Fields{"ip": fcwebGuestIP, "refs": vm.refs, "took": time.Since(start)}).Info("Firecracker VM reachable")
	return fcwebGuestIP, nil
}

// release tears the VM down once no pod uses it, and fails if the process, the
// tap or the API socket is still there afterwards.
func (vm *firecrackerVM) release(ctx context.Context) error {
	vm.Lock()
	defer vm.Unlock()

	if vm.refs > 0 {
		vm.refs--
	}
	if vm.refs > 0 {
		vm.logger().WithField("refs", vm.refs).Info("Firecracker VM still in use, not stopping")
		return nil
	}

	start := time.Now()
	if err := vm.cleanup(); err != nil {
		vm.logger().WithError(err).Error("Firecracker VM teardown incomplete")
		return err
	}

	vm.logger().WithField("took", time.Since(start)).Info("Firecracker VM stopped")
	return nil
}

func (vm *firecrackerVM) running() bool {
	if vm.cmd == nil {
		return false
	}
	select {
	case <-vm.exited:
		return false
	default:
		return true
	}
}

func (vm *firecrackerVM) boot(ctx context.Context) error {
	for _, f := range []string{fcwebBin, fcwebKernel, vm.cfg.Rootfs} {
		if _, err := os.Stat(f); err != nil {
			return fmt.Errorf("firecracker VM: %w", err)
		}
	}
	if err := os.MkdirAll(fcwebRunDir, 0o755); err != nil {
		return err
	}
	cfgPath, err := vm.writeConfig()
	if err != nil {
		return err
	}

	t0 := time.Now()
	if err := createFcwebTap(); err != nil {
		return err
	}

	console, err := os.Create(filepath.Join(fcwebRunDir, "console.log"))
	if err != nil {
		return err
	}
	defer console.Close()

	// Not CommandContext: the VM must outlive the CRI request that boots it.
	// Own session so a signal to vHive's terminal does not reach the VM.
	cmd := exec.Command(fcwebBin, "--api-sock", fcwebSock, "--config-file", cfgPath)
	cmd.Stdout = console
	cmd.Stderr = console
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting firecracker: %w", err)
	}
	exited := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(exited)
	}()
	vm.cmd, vm.exited = cmd, exited
	t1 := time.Now()

	if err := vm.waitReachable(ctx); err != nil {
		return err
	}
	vm.logger().WithFields(log.Fields{
		"pid":             cmd.Process.Pid,
		"tapAndLaunch":    t1.Sub(t0),
		"launchToHTTP200": time.Since(t1),
	}).Info("Firecracker VM booted")
	return nil
}

func (vm *firecrackerVM) writeConfig() (string, error) {
	logPath := filepath.Join(fcwebRunDir, "firecracker.log")
	// Firecracker does not create its log file.
	if err := os.WriteFile(logPath, nil, 0o644); err != nil {
		return "", err
	}

	cfg := map[string]any{
		"boot-source": map[string]any{
			"kernel_image_path": fcwebKernel,
			"boot_args":         fmt.Sprintf("%s fcweb.ip=%s/%d fcweb.gw=%s", fcwebBootArgs, fcwebGuestIP, fcwebMask, fcwebHostIP),
		},
		// Read-only, so one image can back several VMs.
		"drives": []map[string]any{
			{"drive_id": "rootfs", "path_on_host": vm.cfg.Rootfs, "is_root_device": true, "is_read_only": true},
		},
		"network-interfaces": []map[string]any{
			{"iface_id": "eth0", "guest_mac": fcwebMAC, "host_dev_name": fcwebTap},
		},
		"machine-config": map[string]any{"vcpu_count": vm.cfg.VCPUs, "mem_size_mib": vm.cfg.MemMiB},
		"logger":         map[string]any{"log_path": logPath, "level": "Warning"},
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return "", err
	}
	cfgPath := filepath.Join(fcwebRunDir, "vm-config.json")
	return cfgPath, os.WriteFile(cfgPath, data, 0o644)
}

func createFcwebTap() error {
	tap := &netlink.Tuntap{LinkAttrs: netlink.LinkAttrs{Name: fcwebTap}, Mode: netlink.TUNTAP_MODE_TAP}
	if err := netlink.LinkAdd(tap); err != nil {
		return fmt.Errorf("creating tap %s: %w", fcwebTap, err)
	}
	link, err := netlink.LinkByName(fcwebTap)
	if err != nil {
		return fmt.Errorf("finding tap %s: %w", fcwebTap, err)
	}

	addr, err := netlink.ParseAddr(fmt.Sprintf("%s/%d", fcwebHostIP, fcwebMask))
	if err != nil {
		return err
	}
	if err := netlink.AddrAdd(link, addr); err != nil {
		return fmt.Errorf("adding %s to %s: %w", addr, fcwebTap, err)
	}
	if err := netlink.LinkSetUp(link); err != nil {
		return fmt.Errorf("bringing up %s: %w", fcwebTap, err)
	}

	// Static ARP for the guest (its MAC is fixed): no ARP round trip before the
	// first probe, and no FAILED entry if we probe before the guest NIC is up.
	mac, err := net.ParseMAC(fcwebMAC)
	if err != nil {
		return err
	}
	neigh := &netlink.Neigh{
		LinkIndex:    link.Attrs().Index,
		Family:       netlink.FAMILY_V4,
		State:        netlink.NUD_PERMANENT,
		IP:           net.ParseIP(fcwebGuestIP),
		HardwareAddr: mac,
	}
	if err := netlink.NeighSet(neigh); err != nil {
		return fmt.Errorf("adding static ARP entry on %s: %w", fcwebTap, err)
	}
	return nil
}

// waitReachable polls the guest port until it answers HTTP, the process exits
// or the boot timeout expires.
func (vm *firecrackerVM) waitReachable(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, fcwebBootTimeout)
	defer cancel()

	// A SYN sent before the guest NIC is up is lost; a short dial timeout
	// retries it after a few ms instead of waiting out a TCP retransmit.
	dialer := &net.Dialer{Timeout: 20 * time.Millisecond}
	client := &http.Client{
		Timeout:   time.Second,
		Transport: &http.Transport{DialContext: dialer.DialContext, DisableKeepAlives: true},
	}
	url := "http://" + fcwebGuestIP + ":" + vm.port + "/"
	for {
		if resp, err := client.Get(url); err == nil {
			resp.Body.Close()
			return nil
		}

		select {
		case <-vm.exited:
			return fmt.Errorf("firecracker exited during boot: %s", consoleTail())
		case <-ctx.Done():
			return fmt.Errorf("Firecracker VM not reachable at %s: %w: %s", url, ctx.Err(), consoleTail())
		case <-time.After(fcwebPollInterval):
		}
	}
}

func consoleTail() string {
	data, _ := os.ReadFile(filepath.Join(fcwebRunDir, "console.log"))
	if len(data) > 2048 {
		data = data[len(data)-2048:]
	}
	return string(bytes.TrimSpace(data))
}

// cleanup stops our firecracker process and any stale one bound to our API
// socket, deletes the tap and the socket, then verifies that none of them is
// left. It is idempotent.
func (vm *firecrackerVM) cleanup() error {
	if vm.cmd != nil {
		stopProcess(vm.cmd.Process.Pid, func() bool { return !vm.running() })
		vm.cmd, vm.exited = nil, nil
	}
	// Firecrackers started by an earlier vHive run are not our children:
	// liveness is judged by their command line still being in /proc (an exited
	// process, even unreaped, has an empty one).
	for _, pid := range fcwebPids() {
		stopProcess(pid, func() bool { return !isFcwebPid(pid) })
	}

	if link, err := netlink.LinkByName(fcwebTap); err == nil {
		if err := netlink.LinkDel(link); err != nil {
			vm.logger().WithError(err).Error("failed to delete tap")
		}
	}
	if err := os.Remove(fcwebSock); err != nil && !errors.Is(err, os.ErrNotExist) {
		vm.logger().WithError(err).Error("failed to remove API socket")
	}

	var errs []error
	if pids := fcwebPids(); len(pids) > 0 {
		errs = append(errs, fmt.Errorf("firecracker still running: pids %v", pids))
	}
	if _, err := netlink.LinkByName(fcwebTap); err == nil {
		errs = append(errs, fmt.Errorf("tap %s still present", fcwebTap))
	}
	if _, err := os.Stat(fcwebSock); !errors.Is(err, os.ErrNotExist) {
		errs = append(errs, fmt.Errorf("API socket %s still present", fcwebSock))
	}
	return errors.Join(errs...)
}

// stopProcess sends SIGTERM, then SIGKILL if the process has not exited within
// fcwebStopTimeout, and waits up to fcwebStopTimeout again.
func stopProcess(pid int, gone func() bool) {
	for _, sig := range []syscall.Signal{syscall.SIGTERM, syscall.SIGKILL} {
		if gone() {
			return
		}
		_ = syscall.Kill(pid, sig)
		for deadline := time.Now().Add(fcwebStopTimeout); time.Now().Before(deadline) && !gone(); {
			time.Sleep(10 * time.Millisecond)
		}
	}
}

// fcwebPids returns every firecracker process bound to our API socket. vHive's
// own microVMs use a different (relative) socket path and are never matched.
func fcwebPids() []int {
	entries, _ := os.ReadDir("/proc")
	var pids []int
	for _, e := range entries {
		if pid, err := strconv.Atoi(e.Name()); err == nil && isFcwebPid(pid) {
			pids = append(pids, pid)
		}
	}
	return pids
}

func isFcwebPid(pid int) bool {
	cmdline, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/cmdline")
	if err != nil {
		return false
	}
	args := bytes.Split(bytes.TrimRight(cmdline, "\x00"), []byte{0})
	if len(args) < 3 || filepath.Base(string(args[0])) != "firecracker" {
		return false
	}
	for i := 1; i+1 < len(args); i++ {
		if string(args[i]) == "--api-sock" && string(args[i+1]) == fcwebSock {
			return true
		}
	}
	return false
}

// envInt reads an int from an env var, falling back to def when it is
// unset, not a number, or outside [lo, hi].
func envInt(name string, def, lo, hi int) int {
	v, err := strconv.Atoi(os.Getenv(name))
	if err != nil || v < lo || v > hi {
		return def
	}
	return v
}

// Defaults are 1 vCPU / 256 MiB. Override with FCWEB_VCPUS / FCWEB_MEM_MIB
// in the vhive process environment; read each time a fresh VM boots.
func fcwebVCPUs() int  { return envInt("FCWEB_VCPUS", 1, 1, 4) }
func fcwebMemMiB() int { return envInt("FCWEB_MEM_MIB", 256, 128, 1024) }

// Guest disk image. Default is the plain http.server image; override with FCWEB_ROOTFS.
func fcwebRootfs() string {
	if p := os.Getenv("FCWEB_ROOTFS"); p != "" {
		return p
	}
	return "/users/Arun1903/fc-vm/rootfs.ext4"
}

// fcwebConfig is the size and disk image of the fcweb VM. vhive's FCWEB_* env
// gives the defaults; a function overrides them with FC_VCPUS, FC_MEM_MIB and
// FC_ROOTFS in its Knative Service env.
type fcwebConfig struct {
	VCPUs  int
	MemMiB int
	Rootfs string
}

func defaultFcwebConfig() fcwebConfig {
	return fcwebConfig{VCPUs: fcwebVCPUs(), MemMiB: fcwebMemMiB(), Rootfs: fcwebRootfs()}
}

// fcwebConfigFromEnv applies FC_* overrides. get returns "" for unset keys.
// Bad values are an error rather than silently falling back to the default.
func fcwebConfigFromEnv(get func(string) string) (fcwebConfig, error) {
	cfg := defaultFcwebConfig()
	if v := get("FC_VCPUS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 4 {
			return cfg, fmt.Errorf("FC_VCPUS=%q: want an integer 1-4", v)
		}
		cfg.VCPUs = n
	}
	if v := get("FC_MEM_MIB"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 128 || n > 1024 {
			return cfg, fmt.Errorf("FC_MEM_MIB=%q: want an integer 128-1024", v)
		}
		cfg.MemMiB = n
	}
	if v := get("FC_ROOTFS"); v != "" {
		if _, err := os.Stat(v); err != nil {
			return cfg, fmt.Errorf("FC_ROOTFS=%q: %w", v, err)
		}
		cfg.Rootfs = v
	}
	return cfg, nil
}
