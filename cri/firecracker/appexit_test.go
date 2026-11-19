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
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/vhive-serverless/vhive/cri/firecracker/guest"
	criapi "k8s.io/cri-api/pkg/apis/runtime/v1"
)

func exitedStatus(id string) *criapi.ContainerStatusResponse {
	return &criapi.ContainerStatusResponse{Status: &criapi.ContainerStatus{
		Id: id, State: criapi.ContainerState_CONTAINER_EXITED, ExitCode: 137, Reason: "Error",
	}}
}

func TestAppExitFromVM(t *testing.T) {
	for _, tc := range []struct {
		name       string
		in         vmExit
		wantCode   int32
		wantReason string
		wantInMsg  []string
	}{
		{"exit 3", vmExit{Status: guest.ExitStatus{ExitCode: 3, Reason: guest.ReasonExited}, Console: "app said\r\nbye"},
			3, "Error", []string{"exit code 3", "app said\nbye"}},
		{"exit 0", vmExit{Status: guest.ExitStatus{ExitCode: 0, Reason: guest.ReasonExited}}, 0, "Completed", []string{"exit code 0"}},
		{"signal", vmExit{Status: guest.ExitStatus{ExitCode: 139, Reason: guest.ReasonSignaled, Signal: "segmentation fault"}},
			139, "Error", []string{"exit code 139 (segmentation fault)"}},
		{"init error", vmExit{Status: guest.ExitStatus{ExitCode: guest.InitErrorCode, Reason: guest.ReasonInitError, Message: "no such file"}},
			guest.InitErrorCode, "StartError", []string{"no such file"}},
		{"no record", vmExit{Err: guest.ErrNoExitStatus, Console: "Kernel panic"}, appExitUnknownCode, "VMExited",
			[]string{"without an exit record", "Kernel panic"}},
	} {
		got := appExitFromVM(tc.in)
		require.Equal(t, tc.wantCode, got.ExitCode, tc.name)
		require.Equal(t, tc.wantReason, got.Reason, tc.name)
		for _, s := range tc.wantInMsg {
			require.Contains(t, got.Message, s, tc.name)
		}
		require.NotContains(t, got.Message, "\r", tc.name)
	}
	long := appExitFromVM(vmExit{Status: guest.ExitStatus{ExitCode: 1, Reason: guest.ReasonExited}, Console: strings.Repeat("x", 10*appExitMessageMax) + "END"})
	require.Less(t, len(long.Message), 2*appExitMessageMax, "console tail is bounded")
	require.True(t, strings.HasSuffix(long.Message, "END"), "the tail keeps the last bytes")
}

func TestImageVMConfigJSONStatusDrive(t *testing.T) {
	drive, err := guest.EncodePodConfig(guest.PodConfig{})
	require.NoError(t, err)
	data, err := fcwebConfigJSON(slotFcwebSpec(2), fcwebConfig{VCPUs: 1, MemMiB: 256, Rootfs: testDisk,
		Init: guest.InitPath, GuestConfig: string(drive), StatusDrive: true})
	require.NoError(t, err)
	var cfg struct {
		Drives []map[string]any `json:"drives"`
	}
	require.NoError(t, json.Unmarshal(data, &cfg))
	require.Len(t, cfg.Drives, 3)
	require.Equal(t, "config", cfg.Drives[1]["drive_id"])
	require.Equal(t, map[string]any{"drive_id": "status", "path_on_host": "/run/vhive-fcweb/2/exit-status.bin",
		"is_root_device": false, "is_read_only": false}, cfg.Drives[2], "third drive = /dev/vdc, writable")

	_, err = fcwebConfigJSON(slotFcwebSpec(2), fcwebConfig{VCPUs: 1, MemMiB: 256, Rootfs: testDisk, StatusDrive: true})
	require.Error(t, err, "without a config drive the status drive would not be /dev/vdc")
}

func TestStatusDriveFileLifecycle(t *testing.T) {
	spec := slotFcwebSpec(6)
	spec.Dir = t.TempDir()
	spec.Sock = spec.Dir + "/fc.sock"
	spec.Tap = "fcwebtest-none"
	drive, err := guest.EncodePodConfig(guest.PodConfig{})
	require.NoError(t, err)
	vm := &firecrackerVM{spec: spec, cfg: fcwebConfig{VCPUs: 1, MemMiB: 256, Rootfs: testDisk, Init: guest.InitPath,
		GuestConfig: string(drive), StatusDrive: true}}
	_, err = vm.writeConfig()
	require.NoError(t, err)
	st, err := os.Stat(statusPath(spec))
	require.NoError(t, err)
	require.Equal(t, int64(guest.StatusDriveSize), st.Size())
	require.Equal(t, os.FileMode(0o600), st.Mode().Perm())

	// Fresh drive: no record. After fcinit writes one, it is read back.
	r := vm.exitReport()
	require.ErrorIs(t, r.Err, guest.ErrNoExitStatus)
	rec, err := guest.EncodeExitStatus(guest.ExitStatus{ExitCode: 3, Reason: guest.ReasonExited})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(statusPath(spec), rec, 0o600))
	require.NoError(t, os.WriteFile(spec.Dir+"/console.log", []byte("fcinit: app exited\n"), 0o644))
	r = vm.exitReport()
	require.NoError(t, r.Err)
	require.Equal(t, 3, r.Status.ExitCode)
	require.Contains(t, r.Console, "app exited")

	require.NoError(t, vm.cleanup())
	_, err = os.Stat(statusPath(spec))
	require.True(t, os.IsNotExist(err), "cleanup removes the status drive")

	// Baked-in modes get no status drive.
	spec.Dir = t.TempDir()
	vm = &firecrackerVM{spec: spec, cfg: fcwebConfig{VCPUs: 1, MemMiB: 256, Rootfs: testDisk}}
	_, err = vm.writeConfig()
	require.NoError(t, err)
	_, err = os.Stat(statusPath(spec))
	require.True(t, os.IsNotExist(err))
}

func TestImageVMSetsStatusDrive(t *testing.T) {
	fs, vms, _, _, _ := newImageTestService(t)
	_, err := fs.CreateContainer(context.Background(), imageContainerReq(sandboxA))
	require.NoError(t, err)
	require.True(t, vms.lastCfg.Load().StatusDrive)
}

func TestImageVMAppExitIsReportedAndRestarts(t *testing.T) {
	fs, vms, stock, _, _ := newImageTestService(t)
	fs.podvms.onAppExit = fs.imageAppExited
	ctx := context.Background()

	resp, err := fs.CreateContainer(ctx, imageContainerReq(sandboxA))
	require.NoError(t, err)
	require.Equal(t, "ctr-1", resp.ContainerId)

	// Before the exit, nothing is patched.
	st := exitedStatus("ctr-1")
	fs.PatchContainerStatus(st)
	require.Equal(t, int32(137), st.Status.ExitCode)

	vms.created()[0].crash(vmExit{Status: guest.ExitStatus{ExitCode: 3, Reason: guest.ReasonExited}, Console: "fcexit: exiting 3"})
	require.Eventually(t, func() bool { return len(stock.stoppedCtrs()) == 1 }, 2*time.Second, 5*time.Millisecond)
	require.Equal(t, []string{"ctr-1"}, stock.stoppedCtrs(), "the placeholder of the exited container is stopped")

	st = exitedStatus("ctr-1")
	fs.PatchContainerStatus(st)
	require.Equal(t, int32(3), st.Status.ExitCode)
	require.Equal(t, "Error", st.Status.Reason)
	require.Contains(t, st.Status.Message, "fcexit: exiting 3")

	// A RUNNING status is never rewritten, nor is another container's.
	running := &criapi.ContainerStatusResponse{Status: &criapi.ContainerStatus{Id: "ctr-1", State: criapi.ContainerState_CONTAINER_RUNNING}}
	fs.PatchContainerStatus(running)
	require.Zero(t, running.Status.ExitCode)
	require.Empty(t, running.Status.Reason)
	other := exitedStatus("ctr-9")
	fs.PatchContainerStatus(other)
	require.Equal(t, int32(137), other.Status.ExitCode)
	fs.PatchContainerStatus(&criapi.ContainerStatusResponse{}) // no status: no panic

	// kubelet restarts the container in the same sandbox: a fresh VM in the same slot.
	resp, err = fs.CreateContainer(ctx, imageContainerReq(sandboxA))
	require.NoError(t, err)
	require.Equal(t, "ctr-2", resp.ContainerId)
	require.Len(t, vms.created(), 1, "same podVM / slot")
	require.Equal(t, int32(2), vms.boots.Load(), "the VM was booted again")

	// The second exit is reported for the new container only.
	vms.created()[0].crash(vmExit{Err: guest.ErrNoExitStatus, Console: "Kernel panic"})
	require.Eventually(t, func() bool { return len(stock.stoppedCtrs()) == 2 }, 2*time.Second, 5*time.Millisecond)
	require.Equal(t, []string{"ctr-1", "ctr-2"}, stock.stoppedCtrs())
	st = exitedStatus("ctr-2")
	fs.PatchContainerStatus(st)
	require.Equal(t, int32(appExitUnknownCode), st.Status.ExitCode)
	require.Equal(t, "VMExited", st.Status.Reason)

	// Removing a container forgets its record.
	_, err = fs.RemoveContainer(ctx, &criapi.RemoveContainerRequest{ContainerId: "ctr-1"})
	require.NoError(t, err)
	st = exitedStatus("ctr-1")
	fs.PatchContainerStatus(st)
	require.Equal(t, int32(137), st.Status.ExitCode)
}

func TestImageVMTeardownIsNotAnAppExit(t *testing.T) {
	fs, _, stock, _, _ := newImageTestService(t)
	fs.podvms.onAppExit = fs.imageAppExited
	ctx := context.Background()
	_, err := fs.CreateContainer(ctx, imageContainerReq(sandboxA))
	require.NoError(t, err)
	_, err = fs.StopPodSandbox(ctx, &criapi.StopPodSandboxRequest{PodSandboxId: sandboxA})
	require.NoError(t, err)
	time.Sleep(100 * time.Millisecond)
	require.Empty(t, stock.stoppedCtrs(), "scale-down must not be reported as a crash")
}

func TestPodModeExitIsNotWatched(t *testing.T) {
	fs, vms, stock := newTestService(t)
	clearFcwebEnv(t)
	fs.podvms.onAppExit = fs.imageAppExited
	_, err := fs.CreateContainer(context.Background(), userContainerReq(sandboxA, firecrackerPodGuestImage))
	require.NoError(t, err)
	require.False(t, vms.lastCfg.Load().StatusDrive, "baked-in per-pod VMs get no status drive")
	vms.created()[0].crash(vmExit{Status: guest.ExitStatus{ExitCode: 3, Reason: guest.ReasonExited}})
	time.Sleep(100 * time.Millisecond)
	require.Empty(t, stock.stoppedCtrs(), "firecracker-vm://pod behaves as before")
	st := exitedStatus("ctr-1")
	fs.PatchContainerStatus(st)
	require.Equal(t, int32(137), st.Status.ExitCode)
}
