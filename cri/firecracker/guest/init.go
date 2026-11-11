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
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strconv"
	"strings"
)

// DefaultPath is the PATH a container gets when its image sets none (the
// same default Docker and containerd use).
const DefaultPath = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"

// MergeEnv returns the image env overridden by the pod env, the precedence
// containerd uses: a pod value replaces the image value of the same name,
// keeping the image's order; new pod names follow in pod order. PATH is
// added if neither sets it.
func MergeEnv(image, pod []string) []string {
	var out []string
	idx := map[string]int{}
	set := func(kv string) {
		k := kv
		if i := strings.IndexByte(kv, '='); i >= 0 {
			k = kv[:i]
		}
		if k == "" {
			return
		}
		if i, ok := idx[k]; ok {
			out[i] = kv
			return
		}
		idx[k] = len(out)
		out = append(out, kv)
	}
	for _, kv := range image {
		set(kv)
	}
	for _, kv := range pod {
		set(kv)
	}
	if _, ok := idx["PATH"]; !ok {
		set("PATH=" + DefaultPath)
	}
	return out
}

// Getenv returns name's value in env (KEY=VALUE list), last one wins.
func Getenv(env []string, name string) (string, bool) {
	v, ok := "", false
	for _, kv := range env {
		if strings.HasPrefix(kv, name+"=") {
			v, ok = kv[len(name)+1:], true
		}
	}
	return v, ok
}

// Argv is the process to start: Entrypoint followed by Cmd, as for a
// container started without command/args overrides.
func Argv(c ImageConfig) ([]string, error) {
	argv := append(append([]string{}, c.Entrypoint...), c.Cmd...)
	if len(argv) == 0 || argv[0] == "" {
		return nil, errors.New("image has neither Entrypoint nor Cmd")
	}
	return argv, nil
}

// LookPath resolves argv0 like execvp does: a name containing a slash is
// used as is (relative to dir), otherwise each PATH entry is tried. exists
// reports whether a path is an executable regular file.
func LookPath(argv0, pathEnv, dir string, exists func(string) bool) (string, error) {
	if strings.Contains(argv0, "/") {
		p := argv0
		if !path.IsAbs(p) {
			p = path.Join(dir, p)
		}
		if exists(p) {
			return p, nil
		}
		return "", fmt.Errorf("%s: no such executable", argv0)
	}
	for _, d := range strings.Split(pathEnv, ":") {
		if d == "" {
			d = "."
		}
		if !path.IsAbs(d) {
			d = path.Join(dir, d)
		}
		if p := path.Join(d, argv0); exists(p) {
			return p, nil
		}
	}
	return "", fmt.Errorf("%s: not found in PATH=%s", argv0, pathEnv)
}

// IsExecutable is LookPath's exists for the real filesystem.
func IsExecutable(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.Mode().IsRegular() && st.Mode().Perm()&0o111 != 0
}

// User is the identity the app runs as.
type User struct {
	UID, GID int
	Groups   []int // supplementary groups
	Home     string
}

// passwdEntry is one /etc/passwd line.
type passwdEntry struct {
	name     string
	uid, gid int
	home     string
}

func readPasswd(r io.Reader) []passwdEntry {
	var out []passwdEntry
	if r == nil {
		return nil
	}
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		f := strings.Split(sc.Text(), ":")
		if len(f) < 6 || strings.HasPrefix(f[0], "#") {
			continue
		}
		uid, err1 := strconv.Atoi(f[2])
		gid, err2 := strconv.Atoi(f[3])
		if err1 != nil || err2 != nil {
			continue
		}
		out = append(out, passwdEntry{name: f[0], uid: uid, gid: gid, home: f[5]})
	}
	return out
}

type groupEntry struct {
	name    string
	gid     int
	members []string
}

func readGroup(r io.Reader) []groupEntry {
	var out []groupEntry
	if r == nil {
		return nil
	}
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		f := strings.Split(sc.Text(), ":")
		if len(f) < 4 || strings.HasPrefix(f[0], "#") {
			continue
		}
		gid, err := strconv.Atoi(f[2])
		if err != nil {
			continue
		}
		var members []string
		if f[3] != "" {
			members = strings.Split(f[3], ",")
		}
		out = append(out, groupEntry{name: f[0], gid: gid, members: members})
	}
	return out
}

// ResolveUser interprets an image's User field ("", "name", "uid",
// "name:group", "uid:gid", ...) against the image's /etc/passwd and
// /etc/group (either may be nil), the way runc does: a numeric uid not in
// passwd is allowed (gid then defaults to 0), a name not in passwd is an
// error, and supplementary groups are those listing the user by name.
func ResolveUser(spec string, passwd, group io.Reader) (User, error) {
	pw, gr := readPasswd(passwd), readGroup(group)
	u := User{Home: "/"}
	userPart, groupPart, hasGroup := strings.Cut(spec, ":")
	name := ""

	if userPart == "" {
		userPart = "0"
	}
	if n, err := strconv.Atoi(userPart); err == nil {
		if n < 0 {
			return u, fmt.Errorf("user %q: negative uid", spec)
		}
		u.UID = n
		for _, e := range pw {
			if e.uid == n {
				u.GID, u.Home, name = e.gid, e.home, e.name
				break
			}
		}
	} else {
		found := false
		for _, e := range pw {
			if e.name == userPart {
				u.UID, u.GID, u.Home, name, found = e.uid, e.gid, e.home, e.name, true
				break
			}
		}
		if !found {
			return u, fmt.Errorf("user %q: no such user in /etc/passwd", userPart)
		}
	}

	if hasGroup && groupPart != "" {
		if n, err := strconv.Atoi(groupPart); err == nil {
			if n < 0 {
				return u, fmt.Errorf("user %q: negative gid", spec)
			}
			u.GID = n
		} else {
			found := false
			for _, g := range gr {
				if g.name == groupPart {
					u.GID, found = g.gid, true
					break
				}
			}
			if !found {
				return u, fmt.Errorf("group %q: no such group in /etc/group", groupPart)
			}
		}
	} else if name != "" {
		// No explicit group: add the user's supplementary groups.
		for _, g := range gr {
			for _, m := range g.members {
				if m == name && g.gid != u.GID {
					u.Groups = append(u.Groups, g.gid)
				}
			}
		}
	}
	if u.Home == "" {
		u.Home = "/"
	}
	return u, nil
}

// Net is the guest network from the kernel cmdline (fcweb.ip=, fcweb.gw=),
// the values vhive derives from the VM's slot.
type Net struct {
	CIDR    string // e.g. 10.200.0.6/30
	Gateway string // e.g. 10.200.0.5
}

// ParseCmdline reads the network settings from /proc/cmdline contents.
func ParseCmdline(cmdline string) (Net, error) {
	var n Net
	for _, f := range strings.Fields(cmdline) {
		switch {
		case strings.HasPrefix(f, "fcweb.ip="):
			n.CIDR = strings.TrimPrefix(f, "fcweb.ip=")
		case strings.HasPrefix(f, "fcweb.gw="):
			n.Gateway = strings.TrimPrefix(f, "fcweb.gw=")
		}
	}
	if n.CIDR == "" || n.Gateway == "" {
		return n, fmt.Errorf("kernel cmdline lacks fcweb.ip= or fcweb.gw=: %q", cmdline)
	}
	return n, nil
}

// HostsFile is the /etc/hosts fcinit writes: localhost and the hostname on
// the guest IP, so lookups of the own name never wait for DNS (which the
// guest cannot reach).
func HostsFile(hostname, ip string) string {
	s := "127.0.0.1\tlocalhost\n::1\tlocalhost ip6-localhost ip6-loopback\n"
	if hostname != "" {
		s += ip + "\t" + hostname + "\n"
	}
	return s
}
