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
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/vishvananda/netlink"
)

func TestSharedFcwebSpecValues(t *testing.T) {
	require.Equal(t, fcwebSpec{
		Tap:     "fcweb0",
		HostIP:  "10.200.0.1",
		GuestIP: "10.200.0.2",
		MAC:     "06:00:0a:c8:00:02",
		Dir:     "/run/vhive-fcweb",
		Sock:    "/run/vhive-fcweb/fc.sock",
	}, sharedFcwebSpec())
}

// testdata/shared-vm-config.golden.json was produced by writeConfig before
// fcwebSpec existed (commit 3bccc41), with FCWEB_* unset.
func TestSharedConfigJSONMatchesGolden(t *testing.T) {
	t.Setenv("FCWEB_VCPUS", "")
	t.Setenv("FCWEB_MEM_MIB", "")
	t.Setenv("FCWEB_ROOTFS", "")

	want, err := os.ReadFile("testdata/shared-vm-config.golden.json")
	require.NoError(t, err)
	got, err := fcwebConfigJSON(sharedFcwebSpec(), defaultFcwebConfig())
	require.NoError(t, err)
	require.Equal(t, string(want), string(got))
}

func TestCmdlineUsesSock(t *testing.T) {
	cmdline := func(args ...string) []byte { return []byte(strings.Join(args, "\x00") + "\x00") }
	const (
		shared = "/run/vhive-fcweb/fc.sock"
		slot1  = "/run/vhive-fcweb/1/fc.sock"
		slot10 = "/run/vhive-fcweb/10/fc.sock"
	)
	fc := "/usr/local/bin/firecracker"
	cfg := []string{"--config-file", "/run/vhive-fcweb/vm-config.json"}

	for _, tc := range []struct {
		name    string
		cmdline []byte
		sock    string
		want    bool
	}{
		{"shared", cmdline(append([]string{fc, "--api-sock", shared}, cfg...)...), shared, true},
		{"slot 1", cmdline(append([]string{fc, "--api-sock", slot1}, cfg...)...), slot1, true},
		{"slot 1 is not slot 10", cmdline(append([]string{fc, "--api-sock", slot1}, cfg...)...), slot10, false},
		{"slot 10 is not slot 1", cmdline(append([]string{fc, "--api-sock", slot10}, cfg...)...), slot1, false},
		{"slot 1 is not shared", cmdline(append([]string{fc, "--api-sock", slot1}, cfg...)...), shared, false},
		{"shared is not slot 1", cmdline(append([]string{fc, "--api-sock", shared}, cfg...)...), slot1, false},
		{"socket after other flags", cmdline(fc, "--config-file", "x.json", "--api-sock", slot1), slot1, true},
		{"not firecracker", cmdline("/bin/sleep", "--api-sock", slot1), slot1, false},
		{"socket only as config value", cmdline(fc, "--config-file", slot1, "--api-sock", "other"), slot1, false},
		{"flag without value", cmdline(fc, "--config-file", "--api-sock"), slot1, false},
		{"exited process", []byte{}, slot1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, cmdlineUsesSock(tc.cmdline, tc.sock))
		})
	}
}

func TestFcwebTapSetupShared(t *testing.T) {
	setup, err := newFcwebTapSetup(sharedFcwebSpec())
	require.NoError(t, err)

	require.Equal(t, "fcweb0", setup.Tap.Name)
	require.Equal(t, netlink.TUNTAP_MODE_TAP, setup.Tap.Mode)

	require.Equal(t, "10.200.0.1", setup.Addr.IP.String())
	ones, bits := setup.Addr.Mask.Size()
	require.Equal(t, 30, ones)
	require.Equal(t, 32, bits)
	require.Equal(t, "10.200.0.1/30", setup.Addr.IPNet.String())

	require.Equal(t, "10.200.0.2", setup.Neigh.IP.String())
	require.Equal(t, "06:00:0a:c8:00:02", setup.Neigh.HardwareAddr.String())
	require.Equal(t, netlink.NUD_PERMANENT, setup.Neigh.State)
	require.Equal(t, netlink.FAMILY_V4, setup.Neigh.Family)
	require.Zero(t, setup.Neigh.LinkIndex, "set only once the tap exists")
}

func TestFcwebTapSetupRejectsBadSpec(t *testing.T) {
	for name, mutate := range map[string]func(*fcwebSpec){
		"host IP":  func(s *fcwebSpec) { s.HostIP = "10.200.0" },
		"guest IP": func(s *fcwebSpec) { s.GuestIP = "nope" },
		"MAC":      func(s *fcwebSpec) { s.MAC = "06:00" },
	} {
		t.Run(name, func(t *testing.T) {
			spec := sharedFcwebSpec()
			mutate(&spec)
			_, err := newFcwebTapSetup(spec)
			require.Error(t, err)
		})
	}
}
