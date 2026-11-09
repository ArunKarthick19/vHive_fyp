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
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	criapi "k8s.io/cri-api/pkg/apis/runtime/v1"
)

const builtinRootfs = "/users/Arun1903/fc-vm/rootfs.ext4"

func clearFcwebEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{"FCWEB_VCPUS", "FCWEB_MEM_MIB", "FCWEB_ROOTFS"} {
		t.Setenv(k, "")
		require.NoError(t, os.Unsetenv(k))
	}
}

func tempRootfs(t *testing.T, name string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(p, nil, 0o644))
	return p
}

func TestFcwebConfigFromEnv(t *testing.T) {
	fnRootfs := tempRootfs(t, "fn.ext4")
	vhiveRootfs := tempRootfs(t, "vhive.ext4")

	for _, tc := range []struct {
		name    string
		vhive   map[string]string // vHive process FCWEB_*
		fn      map[string]string // function container FC_*
		want    fcwebConfig
		wantErr string
	}{
		// Fallback order: FC_* > FCWEB_* > built-in.
		{name: "built-in defaults",
			want: fcwebConfig{1, 256, builtinRootfs}},
		{name: "vhive env",
			vhive: map[string]string{"FCWEB_VCPUS": "2", "FCWEB_MEM_MIB": "512", "FCWEB_ROOTFS": vhiveRootfs},
			want:  fcwebConfig{2, 512, vhiveRootfs}},
		{name: "function env beats vhive env",
			vhive: map[string]string{"FCWEB_VCPUS": "2", "FCWEB_MEM_MIB": "512", "FCWEB_ROOTFS": vhiveRootfs},
			fn:    map[string]string{"FC_VCPUS": "3", "FC_MEM_MIB": "768", "FC_ROOTFS": fnRootfs},
			want:  fcwebConfig{3, 768, fnRootfs}},
		{name: "function env beats built-in",
			fn:   map[string]string{"FC_VCPUS": "4", "FC_MEM_MIB": "1024", "FC_ROOTFS": fnRootfs},
			want: fcwebConfig{4, 1024, fnRootfs}},
		{name: "partial override keeps the rest",
			vhive: map[string]string{"FCWEB_MEM_MIB": "512"},
			fn:    map[string]string{"FC_VCPUS": "2"},
			want:  fcwebConfig{2, 512, builtinRootfs}},
		{name: "empty FC value counts as unset",
			vhive: map[string]string{"FCWEB_VCPUS": "2"},
			fn:    map[string]string{"FC_VCPUS": "", "FC_MEM_MIB": "", "FC_ROOTFS": ""},
			want:  fcwebConfig{2, 256, builtinRootfs}},
		// Existing behaviour: a bad FCWEB_* value silently falls back to built-in.
		{name: "bad vhive env falls back to built-in",
			vhive: map[string]string{"FCWEB_VCPUS": "9", "FCWEB_MEM_MIB": "lots"},
			want:  fcwebConfig{1, 256, builtinRootfs}},

		// Limits.
		{name: "vcpus min", fn: map[string]string{"FC_VCPUS": "1"}, want: fcwebConfig{1, 256, builtinRootfs}},
		{name: "vcpus max", fn: map[string]string{"FC_VCPUS": "4"}, want: fcwebConfig{4, 256, builtinRootfs}},
		{name: "vcpus 0", fn: map[string]string{"FC_VCPUS": "0"}, wantErr: `FC_VCPUS="0": want an integer 1-4`},
		{name: "vcpus 5", fn: map[string]string{"FC_VCPUS": "5"}, wantErr: `FC_VCPUS="5": want an integer 1-4`},
		{name: "vcpus negative", fn: map[string]string{"FC_VCPUS": "-1"}, wantErr: "FC_VCPUS"},
		{name: "vcpus word", fn: map[string]string{"FC_VCPUS": "two"}, wantErr: "FC_VCPUS"},
		{name: "vcpus fraction", fn: map[string]string{"FC_VCPUS": "1.5"}, wantErr: "FC_VCPUS"},
		{name: "vcpus padded", fn: map[string]string{"FC_VCPUS": " 2"}, wantErr: "FC_VCPUS"},
		{name: "mem min", fn: map[string]string{"FC_MEM_MIB": "128"}, want: fcwebConfig{1, 128, builtinRootfs}},
		{name: "mem max", fn: map[string]string{"FC_MEM_MIB": "1024"}, want: fcwebConfig{1, 1024, builtinRootfs}},
		{name: "mem 127", fn: map[string]string{"FC_MEM_MIB": "127"}, wantErr: `FC_MEM_MIB="127": want an integer 128-1024`},
		{name: "mem 1025", fn: map[string]string{"FC_MEM_MIB": "1025"}, wantErr: `FC_MEM_MIB="1025": want an integer 128-1024`},
		{name: "mem with unit", fn: map[string]string{"FC_MEM_MIB": "512Mi"}, wantErr: "FC_MEM_MIB"},
		{name: "rootfs missing", fn: map[string]string{"FC_ROOTFS": "/nonexistent/rootfs.ext4"}, wantErr: `FC_ROOTFS="/nonexistent/rootfs.ext4"`},
		{name: "bad FC value is not rescued by vhive env",
			vhive:   map[string]string{"FCWEB_VCPUS": "2"},
			fn:      map[string]string{"FC_VCPUS": "8"},
			wantErr: "FC_VCPUS"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clearFcwebEnv(t)
			for k, v := range tc.vhive {
				t.Setenv(k, v)
			}
			got, err := fcwebConfigFromEnv(func(k string) string { return tc.fn[k] })
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestPodVMUsesFunctionEnv(t *testing.T) {
	clearFcwebEnv(t)
	t.Setenv("FCWEB_MEM_MIB", "512")
	rootfs := tempRootfs(t, "fn.ext4")
	fs, vms, _ := newTestService(t)

	_, err := fs.CreateContainer(context.Background(), userContainerReq(sandboxA, firecrackerPodGuestImage,
		&criapi.KeyValue{Key: "FC_VCPUS", Value: "2"},
		&criapi.KeyValue{Key: "FC_ROOTFS", Value: rootfs},
	))
	require.NoError(t, err)
	require.Equal(t, fcwebConfig{VCPUs: 2, MemMiB: 512, Rootfs: rootfs}, *vms.lastCfg.Load())
	require.Equal(t, fcwebConfig{VCPUs: 2, MemMiB: 512, Rootfs: rootfs}, fs.podvms.pods[sandboxA].cfg)
}

func TestPodVMBadFunctionEnvFailsBeforeBoot(t *testing.T) {
	clearFcwebEnv(t)
	for _, kv := range []*criapi.KeyValue{
		{Key: "FC_VCPUS", Value: "16"},
		{Key: "FC_MEM_MIB", Value: "64"},
		{Key: "FC_ROOTFS", Value: "/nonexistent.ext4"},
	} {
		t.Run(kv.Key, func(t *testing.T) {
			fs, vms, stock := newTestService(t)
			_, err := fs.CreateContainer(context.Background(), userContainerReq(sandboxA, firecrackerPodGuestImage, kv))
			require.ErrorContains(t, err, kv.Key)
			require.ErrorContains(t, err, "per-pod Firecracker VM config")
			require.Empty(t, vms.created(), "no VM created")
			require.Zero(t, vms.boots.Load(), "nothing booted")
			require.Empty(t, slotsInUse(fs.podvms), "no slot taken")
			require.Equal(t, []string{"ctr-1"}, stock.removedIDs(), "placeholder removed")
			require.False(t, fs.coordinator.isActive("ctr-1"))
		})
	}
}

func TestPodVMBadFunctionEnvOnRestartKeepsVM(t *testing.T) {
	clearFcwebEnv(t)
	fs, vms, stock := newTestService(t)
	ctx := context.Background()

	_, err := fs.CreateContainer(ctx, userContainerReq(sandboxA, firecrackerPodGuestImage))
	require.NoError(t, err)
	_, err = fs.CreateContainer(ctx, userContainerReq(sandboxA, firecrackerPodGuestImage,
		&criapi.KeyValue{Key: "FC_VCPUS", Value: "99"}))
	require.ErrorContains(t, err, "FC_VCPUS")

	require.True(t, vms.created()[0].running())
	require.Equal(t, int32(1), vms.boots.Load())
	require.Equal(t, []int{1}, slotsInUse(fs.podvms))
	require.Equal(t, []string{"ctr-2"}, stock.removedIDs())
}

func TestSharedModeIgnoresFunctionEnv(t *testing.T) {
	clearFcwebEnv(t)
	for name, env := range map[string][]*criapi.KeyValue{
		"valid": {{Key: "FC_VCPUS", Value: "2"}, {Key: "FC_MEM_MIB", Value: "512"}},
		"bad":   {{Key: "FC_VCPUS", Value: "99"}, {Key: "FC_ROOTFS", Value: "/nonexistent.ext4"}},
	} {
		t.Run(name, func(t *testing.T) {
			fs, vms, stock := newTestService(t)
			// Shared VM already up with the default config. If FC_* were applied
			// the configs would differ and acquire would refuse.
			fs.fcwebvm.cfg = defaultFcwebConfig()
			fs.fcwebvm.cmd, fs.fcwebvm.exited = &exec.Cmd{}, make(chan struct{})

			_, err := fs.CreateContainer(context.Background(), userContainerReq(sandboxA, firecrackerGuestImage, env...))
			require.NoError(t, err)
			require.Equal(t, defaultFcwebConfig(), fs.fcwebvm.cfg)
			require.Equal(t, 1, fs.fcwebvm.refs)
			require.Empty(t, vms.created())
			require.Empty(t, stock.removedIDs())
		})
	}
}
