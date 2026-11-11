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

// fcinit is PID 1 of a firecracker-vm://image VM. It does for one container
// image what a container runtime would: make the read-only rootfs writable
// with a tmpfs overlay, mount /proc /sys /dev, configure eth0 from the
// kernel cmdline, apply the image's env/workdir/user plus the pod env from
// the config drive, start Entrypoint+Cmd, reap zombies, forward SIGTERM and
// power the VM off when the app exits.
//
// Build static: make -C cri/firecracker fcinit
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/vishvananda/netlink"

	"github.com/vhive-serverless/vhive/cri/firecracker/guest"
)

var console io.Writer = os.Stderr

func logf(format string, args ...any) {
	up, _ := os.ReadFile("/proc/uptime")
	t := strings.Fields(string(up) + " ?")[0]
	fmt.Fprintf(console, "fcinit[%ss]: "+format+"\n", append([]any{t}, args...)...)
}

// fail reports err and powers the VM off, so vhive sees the VM exit with
// this message in its console.log instead of a boot timeout.
func fail(err error) {
	logf("FATAL: %v", err)
	poweroff()
}

func poweroff() {
	syscall.Sync()
	// With reboot=k, Firecracker treats a guest reboot as "exit".
	_ = syscall.Reboot(syscall.LINUX_REBOOT_CMD_RESTART)
	for {
		time.Sleep(time.Hour)
	}
}

func mount(src, dst, fstype string, flags uintptr, data string) error {
	if err := os.MkdirAll(dst, 0o755); err != nil && !os.IsExist(err) {
		// MkdirAll fails on a read-only fs even if dst exists; check.
		if st, serr := os.Stat(dst); serr != nil || !st.IsDir() {
			return fmt.Errorf("mkdir %s: %w", dst, err)
		}
	}
	if err := syscall.Mount(src, dst, fstype, flags, data); err != nil {
		return fmt.Errorf("mount %s on %s: %w", fstype, dst, err)
	}
	return nil
}

// overlayRoot makes a writable copy-on-write view of the read-only rootfs
// (writes go to a tmpfs, i.e. VM memory) and chroots into it. If overlayfs
// is unavailable it falls back to the read-only root with tmpfs on /tmp
// and /run.
func overlayRoot() error {
	if err := syscall.Mount("tmpfs", guest.RWDir, "tmpfs", 0, "mode=0755"); err != nil {
		return fmt.Errorf("mount tmpfs on %s: %w", guest.RWDir, err)
	}
	upper, work := guest.RWDir+"/upper", guest.RWDir+"/work"
	for _, d := range []string{upper, work} {
		if err := os.Mkdir(d, 0o755); err != nil {
			return err
		}
	}
	opts := "lowerdir=/,upperdir=" + upper + ",workdir=" + work
	if err := syscall.Mount("overlay", guest.RootDir, "overlay", 0, opts); err != nil {
		logf("WARN: overlay root failed (%v); root stays read-only, tmpfs on /tmp and /run", err)
		for _, d := range []string{"/tmp", "/run"} {
			if err := syscall.Mount("tmpfs", d, "tmpfs", 0, "mode=1777"); err != nil {
				logf("WARN: tmpfs on %s: %v", d, err)
			}
		}
		return nil
	}
	if err := os.Chdir(guest.RootDir); err != nil {
		return err
	}
	if err := syscall.Chroot("."); err != nil {
		return fmt.Errorf("chroot %s: %w", guest.RootDir, err)
	}
	return os.Chdir("/")
}

func mountSpecial() error {
	const nosuid, noexec, nodev = syscall.MS_NOSUID, syscall.MS_NOEXEC, syscall.MS_NODEV
	for _, m := range []struct {
		src, dst, fstype string
		flags            uintptr
		data             string
	}{
		{"proc", "/proc", "proc", nosuid | noexec | nodev, ""},
		{"sysfs", "/sys", "sysfs", nosuid | noexec | nodev, ""},
		{"devtmpfs", "/dev", "devtmpfs", nosuid, "mode=0755"},
		{"devpts", "/dev/pts", "devpts", nosuid | noexec, "newinstance,ptmxmode=0666,mode=0620"},
		{"shm", "/dev/shm", "tmpfs", nosuid | nodev, "mode=1777"},
	} {
		if err := mount(m.src, m.dst, m.fstype, m.flags, m.data); err != nil {
			return err
		}
	}
	for link, target := range map[string]string{
		"/dev/fd": "/proc/self/fd", "/dev/stdin": "/proc/self/fd/0",
		"/dev/stdout": "/proc/self/fd/1", "/dev/stderr": "/proc/self/fd/2",
	} {
		_ = os.Symlink(target, link)
	}
	return nil
}

func setupNet(n guest.Net) error {
	lo, err := netlink.LinkByName("lo")
	if err != nil {
		return err
	}
	if err := netlink.LinkSetUp(lo); err != nil {
		return fmt.Errorf("lo up: %w", err)
	}
	eth, err := netlink.LinkByName("eth0")
	if err != nil {
		return err
	}
	addr, err := netlink.ParseAddr(n.CIDR)
	if err != nil {
		return err
	}
	if err := netlink.AddrAdd(eth, addr); err != nil {
		return fmt.Errorf("eth0 addr %s: %w", n.CIDR, err)
	}
	if err := netlink.LinkSetUp(eth); err != nil {
		return fmt.Errorf("eth0 up: %w", err)
	}
	gw := net.ParseIP(n.Gateway)
	if gw == nil {
		return fmt.Errorf("bad gateway %q", n.Gateway)
	}
	if err := netlink.RouteAdd(&netlink.Route{LinkIndex: eth.Attrs().Index, Gw: gw}); err != nil {
		return fmt.Errorf("default route via %s: %w", n.Gateway, err)
	}
	return nil
}

func readImageConfig() (guest.ImageConfig, error) {
	var c guest.ImageConfig
	data, err := os.ReadFile(guest.ImageConfigPath)
	if err != nil {
		return c, err
	}
	return c, json.Unmarshal(data, &c)
}

func readPodConfig() guest.PodConfig {
	f, err := os.Open(guest.ConfigDrive)
	if err != nil {
		logf("WARN: no config drive (%v); starting with the image env only", err)
		return guest.PodConfig{}
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 1<<20))
	if err != nil {
		logf("WARN: reading config drive: %v", err)
		return guest.PodConfig{}
	}
	c, err := guest.DecodePodConfig(data)
	if err != nil {
		logf("WARN: %v", err)
	}
	return c
}

func openOrNil(p string) io.Reader {
	data, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	return bytes.NewReader(data)
}

func main() {
	if os.Getpid() != 1 {
		fmt.Fprintln(os.Stderr, "fcinit must run as PID 1 of a Firecracker VM")
		os.Exit(2)
	}
	if err := overlayRoot(); err != nil {
		fail(err)
	}
	if err := mountSpecial(); err != nil {
		fail(err)
	}
	if c, err := os.OpenFile("/dev/console", os.O_WRONLY, 0); err == nil {
		console = c
	}
	logf("root ready")

	cmdline, _ := os.ReadFile("/proc/cmdline")
	n, err := guest.ParseCmdline(string(cmdline))
	if err != nil {
		fail(err)
	}
	if err := setupNet(n); err != nil {
		fail(err)
	}

	ic, err := readImageConfig()
	if err != nil {
		fail(fmt.Errorf("image config: %w", err))
	}
	pc := readPodConfig()

	hostname := pc.Hostname
	if hostname == "" {
		hostname = "fcvm"
	}
	_ = syscall.Sethostname([]byte(hostname))
	ip, _, _ := strings.Cut(n.CIDR, "/")
	_ = os.WriteFile("/etc/hosts", []byte(guest.HostsFile(hostname, ip)), 0o644)
	_ = os.WriteFile("/etc/hostname", []byte(hostname+"\n"), 0o644)

	user, err := guest.ResolveUser(ic.User, openOrNil("/etc/passwd"), openOrNil("/etc/group"))
	if err != nil {
		fail(err)
	}
	env := guest.MergeEnv(ic.Env, pc.Env)
	if _, ok := guest.Getenv(env, "HOSTNAME"); !ok {
		env = append(env, "HOSTNAME="+hostname)
	}
	if _, ok := guest.Getenv(env, "HOME"); !ok {
		env = append(env, "HOME="+user.Home)
	}
	dir := ic.WorkingDir
	if dir == "" {
		dir = "/"
	}
	if err := os.MkdirAll(dir, 0o755); err != nil { // like containerd, create a missing workdir
		fail(fmt.Errorf("workdir %s: %w", dir, err))
	}

	argv, err := guest.Argv(ic)
	if err != nil {
		fail(err)
	}
	pathEnv, _ := guest.Getenv(env, "PATH")
	exe, err := guest.LookPath(argv[0], pathEnv, dir, guest.IsExecutable)
	if err != nil {
		fail(err)
	}

	// stdout/stderr are a pipe that fcinit copies to the console, as a
	// container runtime does. The app cannot use /dev/console directly: images
	// like nginx re-open /dev/stderr (/proc/self/fd/2), which for a non-root
	// app fails on the root-owned 0600 console. The pipe is chowned to the app
	// so re-opening it works.
	outR, outW, err := os.Pipe()
	if err != nil {
		fail(err)
	}
	if err := syscall.Fchown(int(outW.Fd()), user.UID, user.GID); err != nil {
		logf("WARN: chown stdout pipe: %v", err)
	}
	copied := make(chan struct{})
	go func() {
		_, _ = io.Copy(console, outR)
		close(copied)
	}()
	devnull, err := os.Open("/dev/null")
	if err != nil {
		fail(err)
	}
	groups := make([]uint32, len(user.Groups))
	for i, g := range user.Groups {
		groups[i] = uint32(g)
	}

	// Forward termination signals before starting, so none is lost.
	sigs := make(chan os.Signal, 4)
	signal.Notify(sigs, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP, syscall.SIGQUIT)

	logf("ip %s gw %s; starting %q as %d:%d in %s", n.CIDR, n.Gateway, argv, user.UID, user.GID, dir)
	pid, err := syscall.ForkExec(exe, argv, &syscall.ProcAttr{
		Dir:   dir,
		Env:   env,
		Files: []uintptr{devnull.Fd(), outW.Fd(), outW.Fd()},
		Sys: &syscall.SysProcAttr{
			Setsid:     true,
			Credential: &syscall.Credential{Uid: uint32(user.UID), Gid: uint32(user.GID), Groups: groups},
		},
	})
	if err != nil {
		fail(fmt.Errorf("starting %s: %w", exe, err))
	}
	outW.Close() // only the app holds the write end now
	go func() {
		for s := range sigs {
			_ = syscall.Kill(pid, s.(syscall.Signal))
		}
	}()

	// PID 1 inherits every orphan: reap them all, and power off once the
	// app itself has exited.
	for {
		var ws syscall.WaitStatus
		p, err := syscall.Wait4(-1, &ws, 0, nil)
		if err == syscall.EINTR {
			continue
		}
		if err != nil {
			logf("wait4: %v", err)
			poweroff()
		}
		if p == pid {
			_ = syscall.Kill(-1, syscall.SIGKILL)
			// Let the last log lines reach the console (the pipe closes once
			// every process holding it is gone).
			select {
			case <-copied:
			case <-time.After(200 * time.Millisecond):
			}
			logf("app exited (%s); powering off", describe(ws))
			poweroff()
		}
	}
}

func describe(ws syscall.WaitStatus) string {
	if ws.Signaled() {
		return "signal " + ws.Signal().String()
	}
	return fmt.Sprintf("status %d", ws.ExitStatus())
}
