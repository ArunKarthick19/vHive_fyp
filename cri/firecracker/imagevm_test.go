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
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/vhive-serverless/vhive/cri/firecracker/guest"
	"github.com/vhive-serverless/vhive/cri/firecracker/rootfs"
	"google.golang.org/grpc"
	criapi "k8s.io/cri-api/pkg/apis/runtime/v1"
)

const (
	testImageID  = "sha256:24349d78cb62b52f99af125f18ada6149df5ed7fe6bdeb286004412a202c270d"
	testImageRef = "ghcr.io/vhive-serverless/crccheck-hello-world@sha256:0404ca69b522f8629d7d4e9034a7afe0300b713354e8bf12ec9657581cf59400"
	testDisk     = "/cache/sha256-0404-abc.ext4"
)

// fakeDisks is the rootfs cache.
type fakeDisks struct {
	mu    sync.Mutex
	refs  []string
	built bool
	err   error
}

func (d *fakeDisks) Ensure(_ context.Context, ref string) (rootfs.Result, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.refs = append(d.refs, ref)
	if d.err != nil {
		return rootfs.Result{}, d.err
	}
	return rootfs.Result{Path: testDisk, Digest: "sha256:0404", Built: d.built}, nil
}

func (d *fakeDisks) asked() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.refs...)
}

func newImageTestService(t *testing.T) (*FirecrackerService, *fakeVMs, *fakeStock, *fakeDisks, *[]string) {
	t.Helper()
	clearFcwebEnv(t)
	fs, vms, stock := newTestService(t)
	disks := &fakeDisks{}
	var named []string
	fs.images = disks
	fs.nameImage = func(_ context.Context, image string) (string, error) {
		named = append(named, image)
		if image == testImageID {
			return testImageRef, nil
		}
		return "", errors.New("unknown image " + image)
	}
	return fs, vms, stock, disks, &named
}

func imageContainerReq(sandboxID string, extraEnv ...*criapi.KeyValue) *criapi.CreateContainerRequest {
	r := userContainerReq(sandboxID, firecrackerImageGuestImage, extraEnv...)
	r.Config.Image = &criapi.ImageSpec{Image: testImageID}
	r.SandboxConfig = &criapi.PodSandboxConfig{
		Hostname: "fcimg-00001-deployment-abc",
		Metadata: &criapi.PodSandboxMetadata{Name: "fcimg-00001-deployment-abc"},
	}
	return r
}

func decodeDrive(t *testing.T, cfg fcwebConfig) guest.PodConfig {
	t.Helper()
	pc, err := guest.DecodePodConfig([]byte(cfg.GuestConfig))
	require.NoError(t, err)
	return pc
}

func TestImageVMBootsTheRequestImage(t *testing.T) {
	fs, vms, stock, disks, named := newImageTestService(t)
	ctx := context.Background()

	resp, err := fs.CreateContainer(ctx, imageContainerReq(sandboxA,
		&criapi.KeyValue{Key: guestPortEnv, Value: "8000"},
		&criapi.KeyValue{Key: "K_REVISION", Value: "fcimg-00001"},
		&criapi.KeyValue{Key: "FC_MEM_MIB", Value: "512"},
	))
	require.NoError(t, err)
	require.Equal(t, "ctr-1", resp.ContainerId)

	require.Equal(t, []string{testImageID}, *named, "image ID named via the image service")
	require.Equal(t, []string{testImageRef}, disks.asked(), "rootfs looked up by the named ref")

	cfg := *vms.lastCfg.Load()
	require.Equal(t, testDisk, cfg.Rootfs)
	require.Equal(t, guest.InitPath, cfg.Init)
	require.Equal(t, "8000", cfg.Port)
	require.True(t, cfg.ProbeTCP)
	require.Equal(t, 512, cfg.MemMiB, "FC_* sizing applies")
	require.Equal(t, 1, cfg.VCPUs)

	pc := decodeDrive(t, cfg)
	require.Equal(t, "fcimg-00001-deployment-abc", pc.Hostname)
	require.Contains(t, pc.Env, "K_REVISION=fcimg-00001")
	require.Contains(t, pc.Env, "GUEST_PORT=8000")

	// Same slots as firecracker-vm://pod, and queue-proxy is pointed at the app port.
	require.Equal(t, slotFcwebSpec(1), vms.created()[0].spec)
	vmc, err := fs.getVMConfig(sandboxA)
	require.NoError(t, err)
	require.Equal(t, "10.200.0.6", vmc.guestIP)
	require.Equal(t, "8000", vmc.guestPort)
	require.True(t, fs.coordinator.activeInstances["ctr-1"].perPodVM)

	// StopPodSandbox tears the VM down like any per-pod VM.
	_, err = fs.StopPodSandbox(ctx, &criapi.StopPodSandboxRequest{PodSandboxId: sandboxA})
	require.NoError(t, err)
	require.False(t, vms.created()[0].running())
	require.Empty(t, slotsInUse(fs.podvms))
	require.Empty(t, stock.removedIDs())
}

func TestImageVMQueueProxyGetsAppPort(t *testing.T) {
	fs, _, stock, _, _ := newImageTestService(t)
	ctx := context.Background()
	_, err := fs.CreateContainer(ctx, imageContainerReq(sandboxA, &criapi.KeyValue{Key: guestPortEnv, Value: "8000"}))
	require.NoError(t, err)

	qp := &criapi.CreateContainerRequest{PodSandboxId: sandboxA, Config: &criapi.ContainerConfig{
		Metadata: &criapi.ContainerMetadata{Name: queueProxyName}}}
	_, err = fs.CreateContainer(ctx, qp)
	require.NoError(t, err)
	env := map[string]string{}
	for _, kv := range qp.Config.Envs {
		env[kv.Key] = kv.Value
	}
	require.Equal(t, "10.200.0.6", env[guestIPEnv])
	require.Equal(t, "8000", env[guestPortEnv])
	_ = stock
}

func TestImageVMFCImageOverridesRequestImage(t *testing.T) {
	fs, vms, _, disks, named := newImageTestService(t)
	_, err := fs.CreateContainer(context.Background(), imageContainerReq(sandboxA,
		&criapi.KeyValue{Key: fcImageEnv, Value: "docker.io/library/nginx:1.27"}))
	require.NoError(t, err)
	require.Empty(t, *named, "image service not asked")
	require.Equal(t, []string{"docker.io/library/nginx:1.27"}, disks.asked())
	require.Equal(t, defaultImagePort, vms.lastCfg.Load().Port, "no GUEST_PORT/PORT: Knative's 8080")
}

func TestImageVMPortFallsBackToPORT(t *testing.T) {
	fs, vms, _, _, _ := newImageTestService(t)
	_, err := fs.CreateContainer(context.Background(), imageContainerReq(sandboxA, &criapi.KeyValue{Key: "PORT", Value: "9090"}))
	require.NoError(t, err)
	require.Equal(t, "9090", vms.lastCfg.Load().Port)
}

func TestImageVMBuiltDuringColdStartStillBoots(t *testing.T) {
	fs, vms, _, disks, _ := newImageTestService(t)
	disks.built = true
	_, err := fs.CreateContainer(context.Background(), imageContainerReq(sandboxA))
	require.NoError(t, err)
	require.Equal(t, int32(1), vms.boots.Load())
}

func TestImageVMErrorsFailBeforeBoot(t *testing.T) {
	for _, tc := range []struct {
		name    string
		env     []*criapi.KeyValue
		image   string
		diskErr error
		want    string
	}{
		{name: "rootfs build fails", diskErr: errors.New("registry down"), want: "registry down"},
		{name: "unknown image ID", image: "sha256:" + strings.Repeat("f", 64), want: "unknown image"},
		{name: "FC_ROOTFS conflicts", env: []*criapi.KeyValue{{Key: "FC_ROOTFS", Value: "/x.ext4"}}, want: "FC_ROOTFS cannot be combined"},
		{name: "bad port", env: []*criapi.KeyValue{{Key: guestPortEnv, Value: "http"}}, want: `port "http"`},
		{name: "bad size", env: []*criapi.KeyValue{{Key: "FC_VCPUS", Value: "64"}}, want: "FC_VCPUS"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fs, vms, stock, disks, _ := newImageTestService(t)
			disks.err = tc.diskErr
			r := imageContainerReq(sandboxA, tc.env...)
			if tc.image != "" {
				r.Config.Image.Image = tc.image
			}
			_, err := fs.CreateContainer(context.Background(), r)
			require.ErrorContains(t, err, tc.want)
			require.ErrorContains(t, err, "per-pod Firecracker VM config")
			require.Zero(t, vms.boots.Load(), "nothing booted")
			require.Empty(t, slotsInUse(fs.podvms), "no slot taken")
			require.Equal(t, []string{"ctr-1"}, stock.removedIDs(), "placeholder removed")
			require.False(t, fs.coordinator.isActive("ctr-1"))
		})
	}
}

// Regression: the baked-in per-pod mode ignores the image entirely.
func TestPodModeIgnoresImage(t *testing.T) {
	fs, vms, _, disks, named := newImageTestService(t)
	r := userContainerReq(sandboxA, firecrackerPodGuestImage)
	r.Config.Image = &criapi.ImageSpec{Image: testImageID}
	_, err := fs.CreateContainer(context.Background(), r)
	require.NoError(t, err)
	require.Empty(t, *named)
	require.Empty(t, disks.asked())
	require.Equal(t, defaultFcwebConfig(), *vms.lastCfg.Load())
	vmc, err := fs.getVMConfig(sandboxA)
	require.NoError(t, err)
	require.Equal(t, firecrackerGuestPort, vmc.guestPort)
}

func TestImagePort(t *testing.T) {
	for env, want := range map[string]string{"": "8080", "GUEST_PORT=8000": "8000", "PORT=9000": "9000", "GUEST_PORT=1,PORT=2": "1"} {
		m := map[string]string{}
		for _, kv := range strings.Split(env, ",") {
			if k, v, ok := strings.Cut(kv, "="); ok {
				m[k] = v
			}
		}
		got, err := imagePort(func(k string) string { return m[k] })
		require.NoError(t, err, env)
		require.Equal(t, want, got, env)
	}
	for _, bad := range []string{"0", "65536", "80a", "-1"} {
		_, err := imagePort(func(k string) string {
			if k == guestPortEnv {
				return bad
			}
			return ""
		})
		require.Error(t, err, bad)
	}
}

// fakeImageService answers ImageStatus only.
type fakeImageService struct {
	criapi.ImageServiceClient
	status map[string]*criapi.Image
	asked  []string
}

func (f *fakeImageService) ImageStatus(_ context.Context, r *criapi.ImageStatusRequest, _ ...grpc.CallOption) (*criapi.ImageStatusResponse, error) {
	f.asked = append(f.asked, r.GetImage().GetImage())
	return &criapi.ImageStatusResponse{Image: f.status[r.GetImage().GetImage()]}, nil
}

func TestStockImageNamer(t *testing.T) {
	hexID := strings.Repeat("a", 64)
	svc := &fakeImageService{status: map[string]*criapi.Image{
		testImageID:                         {RepoDigests: []string{testImageRef}, RepoTags: []string{"ghcr.io/x:latest"}},
		hexID:                               {RepoTags: []string{"ghcr.io/y:v1"}},
		"sha256:" + strings.Repeat("b", 64): {},
	}}
	name := stockImageNamer(svc)
	ctx := context.Background()

	got, err := name(ctx, "ghcr.io/vhive-serverless/crccheck-hello-world:latest")
	require.NoError(t, err)
	require.Equal(t, "ghcr.io/vhive-serverless/crccheck-hello-world:latest", got)
	require.Empty(t, svc.asked, "names are used as is")

	got, err = name(ctx, testImageID)
	require.NoError(t, err)
	require.Equal(t, testImageRef, got, "repo digest preferred")

	got, err = name(ctx, hexID)
	require.NoError(t, err)
	require.Equal(t, "ghcr.io/y:v1", got)

	_, err = name(ctx, "sha256:"+strings.Repeat("b", 64))
	require.ErrorContains(t, err, "no repo digest or tag")
	_, err = name(ctx, "sha256:"+strings.Repeat("c", 64))
	require.ErrorContains(t, err, "not found")

	_, err = stockImageNamer(nil)(ctx, testImageID)
	require.ErrorContains(t, err, fcImageEnv)
}

func TestImageVMConfigJSON(t *testing.T) {
	drive, err := guest.EncodePodConfig(guest.PodConfig{Env: []string{"A=1"}})
	require.NoError(t, err)
	spec := slotFcwebSpec(2)
	data, err := fcwebConfigJSON(spec, fcwebConfig{VCPUs: 1, MemMiB: 256, Rootfs: testDisk,
		Init: guest.InitPath, Port: "8000", ProbeTCP: true, GuestConfig: string(drive)})
	require.NoError(t, err)

	var cfg struct {
		Boot struct {
			Args string `json:"boot_args"`
		} `json:"boot-source"`
		Drives []map[string]any `json:"drives"`
	}
	require.NoError(t, json.Unmarshal(data, &cfg))
	require.Contains(t, cfg.Boot.Args, " init=/.fcinit/init fcweb.ip=10.200.0.10/30 fcweb.gw=10.200.0.9")
	require.NotContains(t, cfg.Boot.Args, "/sbin/fcinit")
	require.Len(t, cfg.Drives, 2)
	require.Equal(t, map[string]any{"drive_id": "rootfs", "path_on_host": testDisk, "is_root_device": true, "is_read_only": true}, cfg.Drives[0])
	require.Equal(t, map[string]any{"drive_id": "config", "path_on_host": "/run/vhive-fcweb/2/guest-config.bin",
		"is_root_device": false, "is_read_only": true}, cfg.Drives[1])
}

// Regression: the baked-in per-pod VM's config is byte-identical to the one
// per-pod-vm (4d02a14) produced.
func TestPodConfigJSONMatchesGolden(t *testing.T) {
	want, err := os.ReadFile("testdata/pod-vm-slot3-config.golden.json")
	require.NoError(t, err)
	got, err := fcwebConfigJSON(slotFcwebSpec(3), fcwebConfig{VCPUs: 2, MemMiB: 512, Rootfs: builtinRootfs})
	require.NoError(t, err)
	require.Equal(t, string(want), string(got))
}

func TestWriteConfigWritesConfigDrive(t *testing.T) {
	spec := slotFcwebSpec(4)
	spec.Dir = t.TempDir()
	drive, err := guest.EncodePodConfig(guest.PodConfig{Env: []string{"K=v"}})
	require.NoError(t, err)

	vm := &firecrackerVM{spec: spec, cfg: fcwebConfig{VCPUs: 1, MemMiB: 256, Rootfs: testDisk, Init: guest.InitPath, GuestConfig: string(drive)}}
	cfgPath, err := vm.writeConfig()
	require.NoError(t, err)
	got, err := os.ReadFile(filepath.Join(spec.Dir, fcwebGuestConfigFile))
	require.NoError(t, err)
	require.Equal(t, drive, got)
	data, err := os.ReadFile(cfgPath)
	require.NoError(t, err)
	require.Contains(t, string(data), filepath.Join(spec.Dir, fcwebGuestConfigFile))

	// The baked-in mode writes no config drive.
	spec.Dir = t.TempDir()
	vm = &firecrackerVM{spec: spec, cfg: fcwebConfig{VCPUs: 1, MemMiB: 256, Rootfs: testDisk}}
	_, err = vm.writeConfig()
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(spec.Dir, fcwebGuestConfigFile))
	require.True(t, os.IsNotExist(err))
}
