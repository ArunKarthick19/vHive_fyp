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

// Package rootfs turns an OCI/Docker image into a bootable, read-only ext4
// root filesystem for a Firecracker VM, without root and without mounting
// anything: the flattened layers are unpacked into a staging directory,
// packed with `mkfs.ext4 -d`, and owners/modes from the image are restored
// with one `debugfs -w` script. The result is cached by image digest (and
// the fcinit binary's hash).
package rootfs

import (
	"archive/tar"
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/mutate"

	"github.com/vhive-serverless/vhive/cri/firecracker/guest"
)

// Source fetches images. The default is a registry (see Remote); tests use
// an in-memory one.
type Source interface {
	// Digest returns the digest the reference names (index or manifest),
	// without downloading layers. For a ref@sha256:... it needs no network.
	Digest(ctx context.Context, ref string) (string, error)
	// Image returns the linux/amd64 image of ref.
	Image(ctx context.Context, ref string) (v1.Image, error)
}

// Builder builds and caches rootfs disks.
type Builder struct {
	CacheDir string // where <digest>-<init hash>.ext4 files live
	InitPath string // the fcinit binary copied into every rootfs
	Source   Source

	Logf func(format string, args ...any) // optional progress log
}

// Result describes a cached rootfs.
type Result struct {
	Path   string // the ext4 file
	Digest string // the image digest it was built from
	Built  bool   // true if this call built it (cache miss)
}

func (b *Builder) logf(format string, args ...any) {
	if b.Logf != nil {
		b.Logf(format, args...)
	}
}

// initHash is the first 12 hex digits of the fcinit binary's sha256, so a
// changed fcinit never runs with a disk built for an older one.
func (b *Builder) initHash() (string, error) {
	f, err := os.Open(b.InitPath)
	if err != nil {
		return "", fmt.Errorf("fcinit binary: %w", err)
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil))[:12], nil
}

// CachePath is the disk for image digest dgst (sha256:...) and fcinit hash.
func (b *Builder) cachePath(dgst, initHash string) string {
	return filepath.Join(b.CacheDir, strings.ReplaceAll(dgst, ":", "-")+"-"+initHash+".ext4")
}

// Lookup returns the cached disk of ref, or ok=false if it is not built.
func (b *Builder) Lookup(ctx context.Context, ref string) (res Result, ok bool, err error) {
	dgst, err := b.Source.Digest(ctx, ref)
	if err != nil {
		return res, false, fmt.Errorf("resolving %s: %w", ref, err)
	}
	ih, err := b.initHash()
	if err != nil {
		return res, false, err
	}
	p := b.cachePath(dgst, ih)
	if _, err := os.Stat(p); err == nil {
		return Result{Path: p, Digest: dgst}, true, nil
	}
	return Result{Path: p, Digest: dgst}, false, nil
}

// Ensure returns ref's disk, building it on a cache miss. Concurrent calls
// for the same image wait for one build.
func (b *Builder) Ensure(ctx context.Context, ref string) (Result, error) {
	res, ok, err := b.Lookup(ctx, ref)
	if err != nil || ok {
		return res, err
	}
	if err := os.MkdirAll(b.CacheDir, 0o755); err != nil {
		return res, err
	}
	unlock, err := lockFile(res.Path + ".lock")
	if err != nil {
		return res, err
	}
	defer unlock()
	if _, err := os.Stat(res.Path); err == nil { // built while we waited
		return res, nil
	}

	img, err := b.Source.Image(ctx, ref)
	if err != nil {
		return res, fmt.Errorf("fetching %s: %w", ref, err)
	}
	if err := b.build(ref, res.Digest, img, res.Path); err != nil {
		return res, err
	}
	res.Built = true
	return res, nil
}

func lockFile(p string) (func(), error) {
	f, err := os.OpenFile(p, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	matchDirOwner(p)
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, err
	}
	return func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN); f.Close() }, nil
}

// matchDirOwner gives p the owner of its directory when running as root, so
// a disk vhive (root) built in the user's cache stays manageable by the user.
func matchDirOwner(p string) {
	if os.Geteuid() != 0 {
		return
	}
	if st, err := os.Stat(filepath.Dir(p)); err == nil {
		if s, ok := st.Sys().(*syscall.Stat_t); ok {
			_ = os.Lchown(p, int(s.Uid), int(s.Gid))
		}
	}
}

// ImageConfigOf extracts what fcinit needs from img's config.
func ImageConfigOf(ref, dgst string, img v1.Image) (guest.ImageConfig, error) {
	cf, err := img.ConfigFile()
	if err != nil {
		return guest.ImageConfig{}, fmt.Errorf("reading image config: %w", err)
	}
	c := cf.Config
	return guest.ImageConfig{
		Ref: ref, Digest: dgst,
		Entrypoint: c.Entrypoint, Cmd: c.Cmd, Env: c.Env,
		WorkingDir: c.WorkingDir, User: c.User,
	}, nil
}

func (b *Builder) build(ref, dgst string, img v1.Image, out string) error {
	icfg, err := ImageConfigOf(ref, dgst, img)
	if err != nil {
		return err
	}
	if len(icfg.Entrypoint)+len(icfg.Cmd) == 0 {
		return fmt.Errorf("image %s has neither Entrypoint nor Cmd", ref)
	}

	stage, err := os.MkdirTemp(b.CacheDir, ".stage-")
	if err != nil {
		return err
	}
	defer removeStage(stage)

	b.logf("rootfs: unpacking %s (%s)", ref, dgst)
	rc := mutate.Extract(img)
	ents, err := unpack(tar.NewReader(rc), stage)
	rc.Close()
	if err != nil {
		return fmt.Errorf("unpacking %s: %w", ref, err)
	}
	ents, err = addFcinit(stage, b.InitPath, icfg, ents)
	if err != nil {
		return err
	}

	tmp := out + ".tmp"
	_ = os.Remove(tmp)
	if err := mkfs(stage, tmp, ents); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := fixOwners(tmp, ents); err != nil {
		os.Remove(tmp)
		return err
	}
	matchDirOwner(tmp)
	if err := os.Rename(tmp, out); err != nil {
		os.Remove(tmp)
		return err
	}
	b.logf("rootfs: built %s (%d entries)", out, len(ents))
	return nil
}

// removeStage deletes a staging dir even if the image had read-only dirs.
// unpack never leaves a staged dir without owner rwx, so RemoveAll works.
func removeStage(dir string) { _ = os.RemoveAll(dir) }

// entry is the metadata of one path as the image wants it; the staged copy
// may differ (it is owned by whoever ran the build, with relaxed modes).
type entry struct {
	Path     string // absolute path in the guest, "/" for the root
	Mode     uint32 // st_mode: type bits | permission bits (incl. setuid/gid/sticky)
	UID, GID int
}

const (
	sIFREG = 0o100000
	sIFDIR = 0o040000
	sIFLNK = 0o120000
	sIFIFO = 0o010000
)

// safeTarget maps a tar entry name to a staging path, refusing names that
// escape the staging dir and parents that are symlinks (an image could
// otherwise make us write through a symlink to anywhere on the host).
func safeTarget(stage, name string) (string, string, error) {
	gp := path.Clean("/" + name)
	if gp == "/" {
		return stage, gp, nil
	}
	parts := strings.Split(gp[1:], "/")
	cur := stage
	for _, p := range parts[:len(parts)-1] {
		cur = filepath.Join(cur, p)
		st, err := os.Lstat(cur)
		if errors.Is(err, fs.ErrNotExist) {
			continue // created by MkdirAll below; no symlink can be in the way
		}
		if err != nil {
			return "", "", err
		}
		if !st.IsDir() {
			return "", "", fmt.Errorf("%s: parent %s is not a directory", gp, strings.TrimPrefix(cur, stage))
		}
	}
	return filepath.Join(stage, filepath.FromSlash(gp[1:])), gp, nil
}

// unpack writes a flattened image tar into stage and returns the image's
// metadata for every path. Device nodes are skipped (devtmpfs provides
// /dev in the guest).
func unpack(tr *tar.Reader, stage string) ([]entry, error) {
	byPath := map[string]entry{"/": {Path: "/", Mode: sIFDIR | 0o755}}
	type link struct {
		dst, path, target string
		perm              uint32
		uid, gid          int
	}
	var links []link
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		dst, gp, err := safeTarget(stage, h.Name)
		if err != nil {
			return nil, err
		}
		perm := uint32(h.Mode) & 0o7777
		e := entry{Path: gp, UID: h.Uid, GID: h.Gid}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return nil, err
		}
		switch h.Typeflag {
		case tar.TypeDir:
			if st, err := os.Lstat(dst); err == nil && !st.IsDir() {
				os.Remove(dst)
			}
			if err := os.MkdirAll(dst, 0o755); err != nil {
				return nil, err
			}
			if err := os.Chmod(dst, os.FileMode(perm&0o777|0o700)); err != nil {
				return nil, err
			}
			e.Mode = sIFDIR | perm
		case tar.TypeReg, tar.TypeRegA:
			if err := replace(dst); err != nil {
				return nil, err
			}
			f, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600)
			if err != nil {
				return nil, err
			}
			_, err = io.Copy(f, tr)
			if cerr := f.Close(); err == nil {
				err = cerr
			}
			if err != nil {
				return nil, err
			}
			if err := os.Chmod(dst, os.FileMode(perm&0o777|0o600)); err != nil {
				return nil, err
			}
			e.Mode = sIFREG | perm
		case tar.TypeSymlink:
			if err := replace(dst); err != nil {
				return nil, err
			}
			if err := os.Symlink(h.Linkname, dst); err != nil {
				return nil, err
			}
			e.Mode = sIFLNK | 0o777
		case tar.TypeLink:
			// mutate.Extract emits the top layer first, so the target may
			// come later in the stream: link once everything is written.
			links = append(links, link{dst: dst, path: gp, target: h.Linkname, perm: perm, uid: h.Uid, gid: h.Gid})
			continue
		case tar.TypeFifo:
			if err := replace(dst); err != nil {
				return nil, err
			}
			if err := syscall.Mkfifo(dst, 0o600); err != nil {
				return nil, err
			}
			e.Mode = sIFIFO | perm
		default: // char/block devices and anything exotic
			continue
		}
		byPath[gp] = e
	}
	for _, l := range links {
		src, sp, err := safeTarget(stage, l.target)
		if err != nil {
			return nil, err
		}
		if err := replace(l.dst); err != nil {
			return nil, err
		}
		if err := os.Link(src, l.dst); err != nil {
			return nil, fmt.Errorf("hardlink %s -> %s: %w", l.path, sp, err)
		}
		// A hardlink shares the inode, so its metadata is the target's.
		e, ok := byPath[sp]
		if !ok {
			e = entry{Mode: sIFREG | l.perm, UID: l.uid, GID: l.gid}
		}
		e.Path = l.path
		byPath[l.path] = e
	}
	ents := make([]entry, 0, len(byPath))
	for _, e := range byPath {
		ents = append(ents, e)
	}
	sort.Slice(ents, func(i, j int) bool { return ents[i].Path < ents[j].Path })
	return ents, nil
}

// replace removes a non-directory at dst so a later layer's entry can take
// its place (a directory would have been replaced by a whiteout first).
func replace(dst string) error {
	if st, err := os.Lstat(dst); err == nil {
		if st.IsDir() {
			return os.RemoveAll(dst)
		}
		return os.Remove(dst)
	}
	return nil
}

// addFcinit adds fcinit, the image config and the mount points fcinit needs.
func addFcinit(stage, initPath string, icfg guest.ImageConfig, ents []entry) ([]entry, error) {
	have := make(map[string]bool, len(ents))
	for _, e := range ents {
		have[e.Path] = true
	}
	add := func(e entry) {
		if !have[e.Path] {
			ents = append(ents, e)
			have[e.Path] = true
		}
	}
	for _, d := range []struct {
		p    string
		perm uint32
	}{{guest.Dir, 0o755}, {guest.RWDir, 0o755}, {guest.RootDir, 0o755},
		{"/proc", 0o555}, {"/sys", 0o555}, {"/dev", 0o755}, {"/tmp", 0o1777}, {"/run", 0o755}, {"/etc", 0o755}} {
		dst := filepath.Join(stage, d.p)
		if st, err := os.Lstat(dst); err == nil && !st.IsDir() {
			// e.g. /run -> /var/run in some images: keep the image's symlink.
			continue
		}
		if err := os.MkdirAll(dst, 0o755); err != nil {
			return nil, err
		}
		add(entry{Path: d.p, Mode: sIFDIR | d.perm})
	}

	data, err := os.ReadFile(initPath)
	if err != nil {
		return nil, fmt.Errorf("fcinit binary: %w", err)
	}
	if err := os.WriteFile(filepath.Join(stage, guest.InitPath), data, 0o755); err != nil {
		return nil, err
	}
	add(entry{Path: guest.InitPath, Mode: sIFREG | 0o755})

	js, err := json.MarshalIndent(icfg, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(stage, guest.ImageConfigPath), js, 0o644); err != nil {
		return nil, err
	}
	add(entry{Path: guest.ImageConfigPath, Mode: sIFREG | 0o644})
	sort.Slice(ents, func(i, j int) bool { return ents[i].Path < ents[j].Path })
	return ents, nil
}

// diskSize estimates the ext4 size for stage: file data rounded up to 4 KiB
// blocks, one block per other inode, plus 25% and 16 MiB of headroom for
// metadata. The disk is read-only in the guest, so little slack is needed.
// Hardlinks (busybox images have hundreds) are counted once.
func diskSize(stage string) (bytes int64, inodes int, err error) {
	seen := map[uint64]bool{}
	err = filepath.WalkDir(stage, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if st, ok := info.Sys().(*syscall.Stat_t); ok && st.Nlink > 1 && !info.IsDir() {
			if seen[st.Ino] {
				return nil
			}
			seen[st.Ino] = true
		}
		inodes++
		if info.Mode().IsRegular() {
			bytes += (info.Size() + 4095) / 4096 * 4096
		} else {
			bytes += 4096
		}
		return nil
	})
	bytes = bytes*5/4 + 16<<20
	bytes = (bytes + 1<<20 - 1) / (1 << 20) * (1 << 20)
	return bytes, inodes, err
}

func mkfs(stage, out string, ents []entry) error {
	size, inodes, err := diskSize(stage)
	if err != nil {
		return err
	}
	if err := os.WriteFile(out, nil, 0o644); err != nil {
		return err
	}
	if err := os.Truncate(out, size); err != nil {
		return err
	}
	// No journal: the disk is only ever mounted read-only.
	cmd := exec.Command("mkfs.ext4", "-q", "-F", "-b", "4096", "-O", "^has_journal",
		"-N", fmt.Sprint(inodes+1024), "-E", "root_owner=0:0", "-d", stage, out)
	if o, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("mkfs.ext4: %w: %s", err, strings.TrimSpace(string(o)))
	}
	return nil
}

// debugfsQuote quotes p for debugfs's command parser.
func debugfsQuote(p string) (string, bool) {
	if strings.ContainsAny(p, "\"\n\\") {
		return "", false
	}
	return `"` + p + `"`, true
}

// fixOwnersScript is the debugfs script restoring the image's owners and
// modes. Pure, so it can be unit tested.
func fixOwnersScript(ents []entry) (string, []string) {
	var sb strings.Builder
	var skipped []string
	for _, e := range ents {
		q, ok := debugfsQuote(e.Path)
		if !ok {
			skipped = append(skipped, e.Path)
			continue
		}
		fmt.Fprintf(&sb, "sif %s uid %d\nsif %s gid %d\n", q, e.UID, q, e.GID)
		if e.Mode&0o170000 != sIFLNK {
			fmt.Fprintf(&sb, "sif %s mode 0%o\n", q, e.Mode)
		}
	}
	return sb.String(), skipped
}

func fixOwners(img string, ents []entry) error {
	script, _ := fixOwnersScript(ents)
	f, err := os.CreateTemp(filepath.Dir(img), ".debugfs-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	w := bufio.NewWriter(f)
	if _, err := w.WriteString(script); err != nil {
		f.Close()
		return err
	}
	if err := w.Flush(); err != nil {
		f.Close()
		return err
	}
	f.Close()
	out, err := exec.Command("debugfs", "-w", "-f", f.Name(), img).CombinedOutput()
	if err != nil {
		return fmt.Errorf("debugfs: %w: %s", err, tail(out))
	}
	// debugfs exits 0 even when a command fails; its errors start with the
	// command name, e.g. "sif: File not found by ext2_lookup".
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "sif:") || strings.Contains(line, "not found") {
			return fmt.Errorf("debugfs: %s", line)
		}
	}
	return nil
}

func tail(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 1000 {
		s = s[len(s)-1000:]
	}
	return s
}
