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

package guest

import (
	"reflect"
	"strings"
	"testing"
)

func TestMergeEnv(t *testing.T) {
	got := MergeEnv(
		[]string{"PATH=/img/bin", "PORT=8000", "A=img"},
		[]string{"PORT=9000", "K_SERVICE=svc", "A=pod", "EMPTY="},
	)
	want := []string{"PATH=/img/bin", "PORT=9000", "A=pod", "K_SERVICE=svc", "EMPTY="}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	def := MergeEnv(nil, []string{"X=1"})
	if !reflect.DeepEqual(def, []string{"X=1", "PATH=" + DefaultPath}) {
		t.Fatalf("default PATH: %v", def)
	}
	if v, ok := Getenv(def, "X"); !ok || v != "1" {
		t.Fatalf("Getenv X = %q %v", v, ok)
	}
	if _, ok := Getenv(def, "NOPE"); ok {
		t.Fatal("Getenv of unset var")
	}
}

func TestArgv(t *testing.T) {
	for _, tc := range []struct {
		ep, cmd, want []string
	}{
		{nil, []string{"/bin/sh", "-c", "httpd -p $PORT & wait"}, []string{"/bin/sh", "-c", "httpd -p $PORT & wait"}},
		{[]string{"/app"}, []string{"--x"}, []string{"/app", "--x"}},
		{[]string{"/app"}, nil, []string{"/app"}},
	} {
		got, err := Argv(ImageConfig{Entrypoint: tc.ep, Cmd: tc.cmd})
		if err != nil || !reflect.DeepEqual(got, tc.want) {
			t.Errorf("Argv(%v,%v) = %v, %v", tc.ep, tc.cmd, got, err)
		}
	}
	if _, err := Argv(ImageConfig{}); err == nil {
		t.Error("empty Argv accepted")
	}
}

func TestLookPath(t *testing.T) {
	files := map[string]bool{"/usr/bin/python3": true, "/bin/sh": true, "/srv/run.sh": true}
	exists := func(p string) bool { return files[p] }
	for _, tc := range []struct{ argv0, path, dir, want string }{
		{"python3", "/usr/local/bin:/usr/bin", "/", "/usr/bin/python3"},
		{"/bin/sh", "", "/", "/bin/sh"},
		{"./run.sh", "/bin", "/srv", "/srv/run.sh"},
		{"run.sh", "/bin::", "/srv", "/srv/run.sh"}, // empty PATH entry = workdir
	} {
		got, err := LookPath(tc.argv0, tc.path, tc.dir, exists)
		if err != nil || got != tc.want {
			t.Errorf("LookPath(%q) = %q, %v; want %q", tc.argv0, got, err, tc.want)
		}
	}
	if _, err := LookPath("nope", "/bin", "/", exists); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("missing binary: %v", err)
	}
}

const passwd = `root:x:0:0:root:/root:/bin/sh
# comment
nobody:x:65534:65534:nobody:/nonexistent:/usr/sbin/nologin
app:x:1000:1001:app:/home/app:/bin/sh
broken line
`
const group = `root:x:0:
app:x:1001:
docker:x:999:app,other
video:x:44:app
`

func TestResolveUser(t *testing.T) {
	for _, tc := range []struct {
		spec string
		want User
	}{
		{"", User{UID: 0, GID: 0, Home: "/root"}},
		{"root", User{UID: 0, GID: 0, Home: "/root"}},
		{"app", User{UID: 1000, GID: 1001, Groups: []int{999, 44}, Home: "/home/app"}},
		{"1000", User{UID: 1000, GID: 1001, Groups: []int{999, 44}, Home: "/home/app"}},
		{"app:docker", User{UID: 1000, GID: 999, Home: "/home/app"}},
		{"1000:5", User{UID: 1000, GID: 5, Home: "/home/app"}},
		{"4242", User{UID: 4242, GID: 0, Home: "/"}}, // unknown numeric uid is allowed
		{"65534:65534", User{UID: 65534, GID: 65534, Home: "/nonexistent"}},
	} {
		got, err := ResolveUser(tc.spec, strings.NewReader(passwd), strings.NewReader(group))
		if err != nil || !reflect.DeepEqual(got, tc.want) {
			t.Errorf("ResolveUser(%q) = %+v, %v; want %+v", tc.spec, got, err, tc.want)
		}
	}
	for _, bad := range []string{"ghost", "app:nogroup", "-1", "1:-1"} {
		if _, err := ResolveUser(bad, strings.NewReader(passwd), strings.NewReader(group)); err == nil {
			t.Errorf("ResolveUser(%q) succeeded", bad)
		}
	}
	// Images without /etc/passwd (scratch, distroless/static) still run as root or a numeric uid.
	if u, err := ResolveUser("", nil, nil); err != nil || u.UID != 0 || u.Home != "/" {
		t.Errorf("no passwd: %+v %v", u, err)
	}
	if u, err := ResolveUser("65532:65532", nil, nil); err != nil || u.UID != 65532 || u.GID != 65532 {
		t.Errorf("no passwd numeric: %+v %v", u, err)
	}
}

func TestParseCmdline(t *testing.T) {
	n, err := ParseCmdline("console=ttyS0 ro init=/.fcinit/init fcweb.ip=10.200.0.6/30 fcweb.gw=10.200.0.5\n")
	if err != nil || n != (Net{CIDR: "10.200.0.6/30", Gateway: "10.200.0.5"}) {
		t.Fatalf("got %+v, %v", n, err)
	}
	if _, err := ParseCmdline("console=ttyS0 fcweb.ip=1.2.3.4/30"); err == nil {
		t.Fatal("missing gateway accepted")
	}
}

func TestHostsFile(t *testing.T) {
	h := HostsFile("pod-1", "10.200.0.6")
	if !strings.Contains(h, "127.0.0.1\tlocalhost\n") || !strings.Contains(h, "10.200.0.6\tpod-1\n") {
		t.Fatalf("hosts:\n%s", h)
	}
}
