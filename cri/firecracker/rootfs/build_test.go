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

package rootfs

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/tarball"

	"github.com/vhive-serverless/vhive/cri/firecracker/guest"
)

type tarEntry struct {
	h    tar.Header
	body string
}

func layer(t *testing.T, ents ...tarEntry) v1.Layer {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, e := range ents {
		h := e.h
		h.Size = int64(len(e.body))
		if h.Typeflag == 0 {
			h.Typeflag = tar.TypeReg
		}
		if err := tw.WriteHeader(&h); err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(tw, e.body); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	data := buf.Bytes()
	l, err := tarball.LayerFromOpener(func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(data)), nil })
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func dir(p string, mode int64, uid int) tarEntry {
	return tarEntry{h: tar.Header{Name: p, Typeflag: tar.TypeDir, Mode: mode, Uid: uid, Gid: uid}}
}
func file(p string, mode int64, uid int, body string) tarEntry {
	return tarEntry{h: tar.Header{Name: p, Mode: mode, Uid: uid, Gid: uid}, body: body}
}

// testImage has two layers exercising owners, odd modes, symlinks, a
// hardlink to a lower-layer file, a device node and a whiteout.
func testImage(t *testing.T) v1.Image {
	t.Helper()
	l1 := layer(t,
		dir("etc/", 0o755, 0),
		file("etc/passwd", 0o644, 0, "root:x:0:0::/root:/bin/sh\napp:x:1000:1000::/home/app:/bin/sh\n"),
		dir("bin/", 0o755, 0),
		file("bin/app", 0o4755, 0, "#!/bin/sh\necho hi\n"),
		dir("data/", 0o700, 1000),
		file("data/secret", 0o000, 1000, "s3cret"),
		dir("ro/", 0o555, 0),
		file("ro/f", 0o444, 0, "x"),
		file("old", 0o644, 0, "to be deleted"),
		tarEntry{h: tar.Header{Name: "lib", Typeflag: tar.TypeSymlink, Linkname: "usr/lib"}},
		tarEntry{h: tar.Header{Name: "dev/null", Typeflag: tar.TypeChar, Mode: 0o666, Devmajor: 1, Devminor: 3}},
	)
	l2 := layer(t,
		file(".wh.old", 0o644, 0, ""),
		file("etc/hostname", 0o644, 0, "img\n"),
		tarEntry{h: tar.Header{Name: "bin/app2", Typeflag: tar.TypeLink, Linkname: "bin/app"}},
	)
	img, err := mutate.AppendLayers(empty.Image, l1, l2)
	if err != nil {
		t.Fatal(err)
	}
	cf, err := img.ConfigFile()
	if err != nil {
		t.Fatal(err)
	}
	cf = cf.DeepCopy()
	cf.Architecture, cf.OS = "amd64", "linux"
	cf.Config.Entrypoint = []string{"/bin/app"}
	cf.Config.Cmd = []string{"--flag", "a b"}
	cf.Config.Env = []string{"PATH=/bin", "PORT=8000"}
	cf.Config.WorkingDir = "/data"
	cf.Config.User = "1000:1000"
	img, err = mutate.ConfigFile(img, cf)
	if err != nil {
		t.Fatal(err)
	}
	return img
}

// memSource serves fixed images by reference.
type memSource map[string]v1.Image

func (m memSource) Digest(_ context.Context, ref string) (string, error) {
	img, ok := m[ref]
	if !ok {
		return "", fmt.Errorf("no image %s", ref)
	}
	d, err := img.Digest()
	return d.String(), err
}

func (m memSource) Image(_ context.Context, ref string) (v1.Image, error) {
	img, ok := m[ref]
	if !ok {
		return nil, fmt.Errorf("no image %s", ref)
	}
	return img, nil
}

func needTools(t *testing.T) {
	for _, tool := range []string{"mkfs.ext4", "debugfs"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not installed", tool)
		}
	}
}

func newBuilder(t *testing.T, src Source) *Builder {
	t.Helper()
	d := t.TempDir()
	initPath := filepath.Join(d, "fcinit")
	if err := os.WriteFile(initPath, []byte("fake fcinit v1"), 0o755); err != nil {
		t.Fatal(err)
	}
	return &Builder{CacheDir: filepath.Join(d, "cache"), InitPath: initPath, Source: src}
}

func debugfs(t *testing.T, img, req string) string {
	t.Helper()
	out, err := exec.Command("debugfs", "-R", req, img).CombinedOutput()
	if err != nil {
		t.Fatalf("debugfs %q: %v: %s", req, err, out)
	}
	return string(out)
}

var (
	modeRE  = regexp.MustCompile(`Mode:\s+(\d+)`)
	ownerRE = regexp.MustCompile(`User:\s+(\d+)\s+Group:\s+(\d+)`)
)

// stat returns "mode uid gid" of p inside the ext4 image.
func stat(t *testing.T, img, p string) string {
	t.Helper()
	out := debugfs(t, img, "stat "+p)
	m, o := modeRE.FindStringSubmatch(out), ownerRE.FindStringSubmatch(out)
	if m == nil || o == nil {
		return "missing"
	}
	return m[1] + " " + o[1] + " " + o[2]
}

func TestEnsureBuildsAndCaches(t *testing.T) {
	needTools(t)
	src := memSource{"example/app:1": testImage(t)}
	b := newBuilder(t, src)
	ctx := context.Background()

	res, err := b.Ensure(ctx, "example/app:1")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Built {
		t.Fatal("first Ensure should build")
	}
	if !strings.HasPrefix(filepath.Base(res.Path), "sha256-") || !strings.HasSuffix(res.Path, ".ext4") {
		t.Fatalf("unexpected cache path %s", res.Path)
	}

	// Owners and modes come from the image, not from who ran the build.
	for p, want := range map[string]string{
		"/":                   "0755 0 0",
		"/bin/app":            "04755 0 0",
		"/data":               "0700 1000 1000",
		"/data/secret":        "0000 1000 1000",
		"/ro":                 "0555 0 0",
		"/etc/passwd":         "0644 0 0",
		guest.InitPath:        "0755 0 0",
		guest.ImageConfigPath: "0644 0 0",
		"/proc":               "0555 0 0",
		"/tmp":                "01777 0 0",
		guest.RootDir:         "0755 0 0",
		"/old":                "missing", // whiteout
		"/dev/null":           "missing", // devices are skipped
	} {
		if got := stat(t, res.Path, p); got != want {
			t.Errorf("%s: got %q, want %q", p, got, want)
		}
	}
	if out := debugfs(t, res.Path, "stat /lib"); !strings.Contains(out, "Type: symlink") || !strings.Contains(out, "usr/lib") {
		t.Errorf("/lib is not a symlink to usr/lib:\n%s", out)
	}
	// The hardlink shares the inode of its lower-layer target.
	inode := func(p string) string {
		return strings.Fields(debugfs(t, res.Path, "stat "+p))[1]
	}
	if inode("/bin/app") != inode("/bin/app2") {
		t.Error("/bin/app2 is not a hardlink of /bin/app")
	}
	if got := debugfs(t, res.Path, "cat "+guest.InitPath); !strings.Contains(got, "fake fcinit v1") {
		t.Errorf("fcinit not copied: %q", got)
	}

	var icfg guest.ImageConfig
	js := debugfs(t, res.Path, "cat "+guest.ImageConfigPath)
	if err := json.Unmarshal([]byte(js[strings.Index(js, "{"):]), &icfg); err != nil {
		t.Fatalf("image.json: %v: %s", err, js)
	}
	if strings.Join(icfg.Entrypoint, "|") != "/bin/app" || strings.Join(icfg.Cmd, "|") != "--flag|a b" ||
		icfg.User != "1000:1000" || icfg.WorkingDir != "/data" || strings.Join(icfg.Env, "|") != "PATH=/bin|PORT=8000" {
		t.Errorf("image config not carried over: %+v", icfg)
	}
	if icfg.Digest != res.Digest {
		t.Errorf("digest %s, want %s", icfg.Digest, res.Digest)
	}

	// Cache hit: same file, not rebuilt.
	again, err := b.Ensure(ctx, "example/app:1")
	if err != nil {
		t.Fatal(err)
	}
	if again.Built || again.Path != res.Path {
		t.Fatalf("second Ensure: %+v, want cache hit of %s", again, res.Path)
	}
	if _, ok, err := b.Lookup(ctx, "example/app:1"); err != nil || !ok {
		t.Fatalf("Lookup after build: ok=%v err=%v", ok, err)
	}

	// A different fcinit is a different disk.
	if err := os.WriteFile(b.InitPath, []byte("fake fcinit v2"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := b.Lookup(ctx, "example/app:1"); ok {
		t.Fatal("a new fcinit must not reuse the old disk")
	}

	// No staging dirs or temp files left behind.
	left, _ := filepath.Glob(filepath.Join(b.CacheDir, ".*"))
	if len(left) != 0 {
		t.Errorf("leftovers in cache dir: %v", left)
	}
}

func TestEnsureRejectsImageWithoutCommand(t *testing.T) {
	needTools(t)
	img, err := mutate.AppendLayers(empty.Image, layer(t, file("x", 0o644, 0, "x")))
	if err != nil {
		t.Fatal(err)
	}
	b := newBuilder(t, memSource{"nocmd": img})
	if _, err := b.Ensure(context.Background(), "nocmd"); err == nil || !strings.Contains(err.Error(), "neither Entrypoint nor Cmd") {
		t.Fatalf("got %v", err)
	}
}

func TestUnpackRefusesWritesThroughSymlinks(t *testing.T) {
	outside := t.TempDir()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	_ = tw.WriteHeader(&tar.Header{Name: "evil", Typeflag: tar.TypeSymlink, Linkname: outside})
	_ = tw.WriteHeader(&tar.Header{Name: "evil/pwned", Typeflag: tar.TypeReg, Mode: 0o644, Size: 1})
	_, _ = tw.Write([]byte("x"))
	_ = tw.Close()

	_, err := unpack(tar.NewReader(&buf), t.TempDir())
	if err == nil {
		t.Fatal("unpack followed a symlink out of the staging dir")
	}
	if _, err := os.Stat(filepath.Join(outside, "pwned")); err == nil {
		t.Fatal("file written outside the staging dir")
	}
}

func TestSafeTargetCleansDotDot(t *testing.T) {
	stage := t.TempDir()
	dst, gp, err := safeTarget(stage, "../../etc/../x/y")
	if err != nil {
		t.Fatal(err)
	}
	if gp != "/x/y" || dst != filepath.Join(stage, "x", "y") {
		t.Fatalf("got %s %s", dst, gp)
	}
}

func TestFixOwnersScript(t *testing.T) {
	script, skipped := fixOwnersScript([]entry{
		{Path: "/a b", Mode: sIFREG | 0o4755, UID: 1, GID: 2},
		{Path: "/l", Mode: sIFLNK | 0o777},
		{Path: "/bad\"name", Mode: sIFREG | 0o644},
	})
	want := "sif \"/a b\" uid 1\nsif \"/a b\" gid 2\nsif \"/a b\" mode 0104755\n" +
		"sif \"/l\" uid 0\nsif \"/l\" gid 0\n"
	if script != want {
		t.Errorf("script:\n%s\nwant:\n%s", script, want)
	}
	if len(skipped) != 1 || skipped[0] != "/bad\"name" {
		t.Errorf("skipped %v", skipped)
	}
}

// TestRemoteSource pulls through a real (in-process) registry: a tag costs a
// HEAD request, a digest reference resolves locally, and both name the
// digest containerd reports in RepoDigests.
func TestRemoteSource(t *testing.T) {
	srv := httptest.NewServer(registry.New(registry.Logger(log.New(io.Discard, "", 0))))
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "http://")

	img := testImage(t)
	tag, err := name.ParseReference(host + "/test/app:v1")
	if err != nil {
		t.Fatal(err)
	}
	if err := remote.Write(tag, img); err != nil {
		t.Fatal(err)
	}
	want, _ := img.Digest()

	ctx := context.Background()
	r := Remote{}
	got, err := r.Digest(ctx, tag.String())
	if err != nil || got != want.String() {
		t.Fatalf("Digest(tag) = %s, %v; want %s", got, err, want)
	}
	byDigest := host + "/test/app@" + want.String()
	srv.Close() // a digest reference must not need the registry
	if got, err := r.Digest(ctx, byDigest); err != nil || got != want.String() {
		t.Fatalf("Digest(digest ref) = %s, %v", got, err)
	}
}

func TestRemoteImagePull(t *testing.T) {
	needTools(t)
	srv := httptest.NewServer(registry.New(registry.Logger(log.New(io.Discard, "", 0))))
	defer srv.Close()
	ref := strings.TrimPrefix(srv.URL, "http://") + "/test/app:v1"
	tag, _ := name.ParseReference(ref)
	if err := remote.Write(tag, testImage(t)); err != nil {
		t.Fatal(err)
	}
	b := newBuilder(t, Remote{})
	res, err := b.Ensure(context.Background(), ref)
	if err != nil {
		t.Fatal(err)
	}
	if got := stat(t, res.Path, "/bin/app"); got != "04755 0 0" {
		t.Errorf("/bin/app: %s", got)
	}
}
