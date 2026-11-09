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
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	criapi "k8s.io/cri-api/pkg/apis/runtime/v1"
)

// fakeVM stands in for a Firecracker VM: it only records calls.
type fakeVM struct {
	spec fcwebSpec
	f    *fakeVMs

	mu       sync.Mutex
	up       bool
	boots    int
	cleanups int
}

func (v *fakeVM) coldBoot(ctx context.Context, cfg fcwebConfig) error {
	v.f.boots.Add(1)
	v.f.lastCfg.Store(&cfg)
	time.Sleep(v.f.bootDelay)
	v.mu.Lock()
	v.boots++
	v.mu.Unlock()
	if err := v.f.bootErr; err != nil {
		_ = v.cleanup()
		return err
	}
	v.mu.Lock()
	v.up = true
	v.mu.Unlock()
	return nil
}

func (v *fakeVM) running() bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.up
}

func (v *fakeVM) cleanup() error {
	if hook := v.f.onCleanup; hook != nil {
		hook(v)
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	v.cleanups++
	if err := v.f.cleanupErr; err != nil {
		return err
	}
	v.up = false
	return nil
}

func (v *fakeVM) guestIP() string { return v.spec.GuestIP }

// fakeVMs is the VM factory given to podVMs.
type fakeVMs struct {
	mu  sync.Mutex
	vms []*fakeVM

	boots      atomic.Int32
	lastCfg    atomic.Pointer[fcwebConfig]
	bootDelay  time.Duration
	bootErr    error
	cleanupErr error
	onCleanup  func(*fakeVM)
}

func (f *fakeVMs) new(spec fcwebSpec) podVMHandle {
	f.mu.Lock()
	defer f.mu.Unlock()
	v := &fakeVM{spec: spec, f: f}
	f.vms = append(f.vms, v)
	return v
}

func (f *fakeVMs) created() []*fakeVM {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*fakeVM(nil), f.vms...)
}

// fakeStock is containerd: only the calls FirecrackerService makes.
type fakeStock struct {
	criapi.RuntimeServiceClient // nil: any other call panics

	mu        sync.Mutex
	nextID    int
	createErr error
	removed   []string
	stopped   []string

	stopResp *criapi.StopPodSandboxResponse
	stopErr  error
}

func (s *fakeStock) CreateContainer(ctx context.Context, r *criapi.CreateContainerRequest, _ ...grpc.CallOption) (*criapi.CreateContainerResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.createErr != nil {
		return nil, s.createErr
	}
	s.nextID++
	return &criapi.CreateContainerResponse{ContainerId: fmt.Sprintf("ctr-%d", s.nextID)}, nil
}

func (s *fakeStock) RemoveContainer(ctx context.Context, r *criapi.RemoveContainerRequest, _ ...grpc.CallOption) (*criapi.RemoveContainerResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.removed = append(s.removed, r.GetContainerId())
	return &criapi.RemoveContainerResponse{}, nil
}

func (s *fakeStock) StopPodSandbox(ctx context.Context, r *criapi.StopPodSandboxRequest, _ ...grpc.CallOption) (*criapi.StopPodSandboxResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopped = append(s.stopped, r.GetPodSandboxId())
	return s.stopResp, s.stopErr
}

func (s *fakeStock) removedIDs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.removed...)
}

func newTestService(t *testing.T) (*FirecrackerService, *fakeVMs, *fakeStock) {
	t.Helper()
	vms := &fakeVMs{}
	stock := &fakeStock{stopResp: &criapi.StopPodSandboxResponse{}}
	fs := &FirecrackerService{
		stockRuntimeClient: stock,
		coordinator:        newFirecrackerCoordinator(nil, withoutOrchestrator()),
		vmConfigs:          make(map[string]*VMConfig),
		fcwebvm:            newFirecrackerVM(firecrackerGuestPort),
		podvms:             newPodVMs(vms.new),
	}
	fs.podvms.removeDir = func(string) error { return nil }
	return fs, vms, stock
}

func userContainerReq(sandboxID, guestImage string, extraEnv ...*criapi.KeyValue) *criapi.CreateContainerRequest {
	envs := append([]*criapi.KeyValue{{Key: guestImageEnv, Value: guestImage}}, extraEnv...)
	return &criapi.CreateContainerRequest{
		PodSandboxId: sandboxID,
		Config: &criapi.ContainerConfig{
			Metadata: &criapi.ContainerMetadata{Name: userContainerName},
			Envs:     envs,
		},
	}
}

const (
	sandboxA = "aaaaaaaaaaaa1111111111111111"
	sandboxB = "bbbbbbbbbbbb2222222222222222"
)

func slotsInUse(m *podVMs) []int {
	var used []int
	for n := 1; n <= fcwebMaxSlots; n++ {
		if m.slots.inUse(n) {
			used = append(used, n)
		}
	}
	return used
}

func TestPodVMCreateBootsOwnVM(t *testing.T) {
	fs, vms, _ := newTestService(t)
	ctx := context.Background()

	resp, err := fs.CreateContainer(ctx, userContainerReq(sandboxA, firecrackerPodGuestImage))
	require.NoError(t, err)
	require.Equal(t, "ctr-1", resp.ContainerId)

	require.Len(t, vms.created(), 1)
	require.Equal(t, slotFcwebSpec(1), vms.created()[0].spec)
	require.Equal(t, []int{1}, slotsInUse(fs.podvms))

	cfg, err := fs.getVMConfig(sandboxA)
	require.NoError(t, err)
	require.Equal(t, "10.200.0.6", cfg.guestIP)
	require.Equal(t, "80", cfg.guestPort)

	fi := fs.coordinator.activeInstances["ctr-1"]
	require.NotNil(t, fi)
	require.True(t, fi.perPodVM)
	require.Equal(t, "firecracker-vm/pod-1-aaaaaaaaaaaa", fi.VmID)
	require.Nil(t, fi.firecrackerVM, "must not be tied to the shared VM")
}

func TestPodVMIDsAreUnique(t *testing.T) {
	fs, _, _ := newTestService(t)
	ctx := context.Background()

	_, err := fs.CreateContainer(ctx, userContainerReq(sandboxA, firecrackerPodGuestImage))
	require.NoError(t, err)
	_, err = fs.CreateContainer(ctx, userContainerReq(sandboxB, firecrackerPodGuestImage))
	require.NoError(t, err)
	require.Equal(t, "firecracker-vm/pod-1-aaaaaaaaaaaa", fs.coordinator.activeInstances["ctr-1"].VmID)
	require.Equal(t, "firecracker-vm/pod-2-bbbbbbbbbbbb", fs.coordinator.activeInstances["ctr-2"].VmID)

	// Slot 1 reused by another sandbox: still a different ID.
	_, err = fs.StopPodSandbox(ctx, &criapi.StopPodSandboxRequest{PodSandboxId: sandboxA})
	require.NoError(t, err)
	_, err = fs.CreateContainer(ctx, userContainerReq("cccccccccccc3333", firecrackerPodGuestImage))
	require.NoError(t, err)
	require.Equal(t, "firecracker-vm/pod-1-cccccccccccc", fs.coordinator.activeInstances["ctr-3"].VmID)

	require.Equal(t, "firecracker-vm/pod-7-short", podVMID(7, "short"))
}

func TestPodVMRepeatCreateReusesVM(t *testing.T) {
	fs, vms, stock := newTestService(t)
	ctx := context.Background()

	_, err := fs.CreateContainer(ctx, userContainerReq(sandboxA, firecrackerPodGuestImage))
	require.NoError(t, err)

	// The user container crashed: kubelet removes it and creates a new one in
	// the same sandbox.
	_, err = fs.RemoveContainer(ctx, &criapi.RemoveContainerRequest{ContainerId: "ctr-1"})
	require.NoError(t, err)
	require.Eventually(t, func() bool { return !fs.coordinator.isActive("ctr-1") }, time.Second, time.Millisecond)

	resp, err := fs.CreateContainer(ctx, userContainerReq(sandboxA, firecrackerPodGuestImage))
	require.NoError(t, err)
	require.Equal(t, "ctr-2", resp.ContainerId)

	require.Len(t, vms.created(), 1, "same sandbox, same VM")
	require.Equal(t, int32(1), vms.boots.Load())
	require.Zero(t, vms.created()[0].cleanups, "RemoveContainer must not stop a per-pod VM")
	require.True(t, vms.created()[0].running())
	require.Equal(t, []int{1}, slotsInUse(fs.podvms))
	require.Equal(t, "firecracker-vm/pod-1-aaaaaaaaaaaa", fs.coordinator.activeInstances["ctr-2"].VmID)
	require.Equal(t, []string{"ctr-1"}, stock.removedIDs(), "only kubelet's own remove")
}

func TestPodVMParallelCreatesBootOnce(t *testing.T) {
	fs, vms, _ := newTestService(t)
	vms.bootDelay = 30 * time.Millisecond
	ctx := context.Background()

	const n = 8
	var wg sync.WaitGroup
	errs := make(chan error, n)
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := fs.CreateContainer(ctx, userContainerReq(sandboxA, firecrackerPodGuestImage))
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}

	require.Equal(t, int32(1), vms.boots.Load(), "parallel creates in one sandbox must boot one VM")
	require.Len(t, vms.created(), 1)
	require.Equal(t, []int{1}, slotsInUse(fs.podvms))
}

func TestPodVMFailedBootFreesSlotAndRemovesPlaceholder(t *testing.T) {
	fs, vms, stock := newTestService(t)
	vms.bootErr = errors.New("guest never answered")
	ctx := context.Background()

	_, err := fs.CreateContainer(ctx, userContainerReq(sandboxA, firecrackerPodGuestImage))
	require.ErrorContains(t, err, "guest never answered")

	require.Empty(t, slotsInUse(fs.podvms), "slot freed after successful cleanup")
	require.Empty(t, fs.podvms.pods)
	require.Equal(t, []string{"ctr-1"}, stock.removedIDs(), "placeholder removed")
	require.False(t, fs.coordinator.isActive("ctr-1"))
	_, err = fs.getVMConfig(sandboxA)
	require.Error(t, err)

	// kubelet retries: a fresh VM in the lowest slot.
	vms.bootErr = nil
	_, err = fs.CreateContainer(ctx, userContainerReq(sandboxA, firecrackerPodGuestImage))
	require.NoError(t, err)
	require.Equal(t, []int{1}, slotsInUse(fs.podvms))
}

func TestPodVMFailedPlaceholderOnFirstCreateStopsVM(t *testing.T) {
	fs, vms, stock := newTestService(t)
	stock.createErr = errors.New("containerd said no")
	ctx := context.Background()

	_, err := fs.CreateContainer(ctx, userContainerReq(sandboxA, firecrackerPodGuestImage))
	require.ErrorContains(t, err, "containerd said no")

	require.Len(t, vms.created(), 1)
	require.False(t, vms.created()[0].running(), "VM booted by a failed create is stopped")
	require.Empty(t, slotsInUse(fs.podvms))
	require.Empty(t, stock.removedIDs(), "no placeholder to remove")
}

func TestPodVMFailedRestartKeepsRunningVM(t *testing.T) {
	fs, vms, stock := newTestService(t)
	ctx := context.Background()

	_, err := fs.CreateContainer(ctx, userContainerReq(sandboxA, firecrackerPodGuestImage))
	require.NoError(t, err)

	stock.mu.Lock()
	stock.createErr = errors.New("containerd said no")
	stock.mu.Unlock()
	_, err = fs.CreateContainer(ctx, userContainerReq(sandboxA, firecrackerPodGuestImage))
	require.Error(t, err)

	vm := vms.created()[0]
	require.True(t, vm.running(), "a failed restart must not stop the sandbox's running VM")
	require.Zero(t, vm.cleanups)
	require.Equal(t, []int{1}, slotsInUse(fs.podvms))
}

func TestPodVMFailedCleanupKeepsSlot(t *testing.T) {
	fs, vms, stock := newTestService(t)
	ctx := context.Background()

	var sawFree atomic.Bool
	vms.onCleanup = func(v *fakeVM) {
		if !fs.podvms.slots.inUse(1) {
			sawFree.Store(true)
		}
	}

	_, err := fs.CreateContainer(ctx, userContainerReq(sandboxA, firecrackerPodGuestImage))
	require.NoError(t, err)

	vms.cleanupErr = errors.New("tap fcweb1 still present")
	stock.stopErr = errors.New("containerd stop error")
	resp, err := fs.StopPodSandbox(ctx, &criapi.StopPodSandboxRequest{PodSandboxId: sandboxA})
	require.Same(t, stock.stopResp, resp, "containerd's response is returned unchanged")
	require.Same(t, stock.stopErr, err, "containerd's error is returned unchanged")

	require.Equal(t, []int{1}, slotsInUse(fs.podvms), "slot whose cleanup failed stays taken")
	require.False(t, sawFree.Load(), "slot must not be freed before cleanup")

	vms.cleanupErr = nil
	_, err = fs.CreateContainer(ctx, userContainerReq(sandboxB, firecrackerPodGuestImage))
	require.NoError(t, err)
	require.Equal(t, slotFcwebSpec(2), vms.created()[1].spec, "next pod skips the reserved slot")
}

func TestPodVMSlotFreedOnlyAfterCleanup(t *testing.T) {
	fs, vms, _ := newTestService(t)
	ctx := context.Background()

	var freeDuringCleanup atomic.Bool
	vms.onCleanup = func(v *fakeVM) {
		if !fs.podvms.slots.inUse(1) {
			freeDuringCleanup.Store(true)
		}
	}
	_, err := fs.CreateContainer(ctx, userContainerReq(sandboxA, firecrackerPodGuestImage))
	require.NoError(t, err)
	_, err = fs.StopPodSandbox(ctx, &criapi.StopPodSandboxRequest{PodSandboxId: sandboxA})
	require.NoError(t, err)

	require.False(t, freeDuringCleanup.Load(), "slot 1 was free while its VM was being cleaned up")
	require.Empty(t, slotsInUse(fs.podvms))
}

func TestPodVMStopTwice(t *testing.T) {
	fs, vms, stock := newTestService(t)
	ctx := context.Background()

	_, err := fs.CreateContainer(ctx, userContainerReq(sandboxA, firecrackerPodGuestImage))
	require.NoError(t, err)

	for i := 0; i < 2; i++ {
		resp, err := fs.StopPodSandbox(ctx, &criapi.StopPodSandboxRequest{PodSandboxId: sandboxA})
		require.NoError(t, err)
		require.Same(t, stock.stopResp, resp)
	}
	vm := vms.created()[0]
	require.Equal(t, 1, vm.cleanups, "second stop is a no-op")
	require.False(t, vm.running())
	require.Empty(t, slotsInUse(fs.podvms))
	require.Equal(t, []string{sandboxA, sandboxA}, stock.stopped, "both stops forwarded")

	// A sandbox that never had a VM: forwarded, nothing else.
	_, err = fs.StopPodSandbox(ctx, &criapi.StopPodSandboxRequest{PodSandboxId: "unknown"})
	require.NoError(t, err)
	require.Len(t, vms.created(), 1)
}

func TestPodVM17thPodFails(t *testing.T) {
	fs, vms, stock := newTestService(t)
	ctx := context.Background()

	for i := 1; i <= fcwebMaxSlots; i++ {
		_, err := fs.CreateContainer(ctx, userContainerReq(fmt.Sprintf("sandbox-%02d", i), firecrackerPodGuestImage))
		require.NoError(t, err)
	}
	_, err := fs.CreateContainer(ctx, userContainerReq("sandbox-17", firecrackerPodGuestImage))
	require.Error(t, err)
	require.True(t, strings.HasSuffix(err.Error(), "all 16 are in use"), err.Error())
	require.Len(t, vms.created(), fcwebMaxSlots, "no VM for the 17th pod")
	require.Equal(t, []string{"ctr-17"}, stock.removedIDs(), "its placeholder is removed")
	for _, v := range vms.created() {
		require.True(t, v.running(), "the 16 running VMs are untouched")
	}
}

func TestSharedModeUnaffected(t *testing.T) {
	fs, vms, stock := newTestService(t)
	ctx := context.Background()

	// Pretend the shared VM is already up, so acquire takes the no-boot path
	// and nothing touches the host.
	fs.fcwebvm.cfg = defaultFcwebConfig()
	// cmd is never started: only running() looks at it on this path.
	fs.fcwebvm.cmd, fs.fcwebvm.exited = &exec.Cmd{}, make(chan struct{})

	resp, err := fs.CreateContainer(ctx, userContainerReq(sandboxA, firecrackerGuestImage))
	require.NoError(t, err)
	require.Equal(t, 1, fs.fcwebvm.refs)

	fi := fs.coordinator.activeInstances[resp.ContainerId]
	require.Same(t, fs.fcwebvm, fi.firecrackerVM)
	require.False(t, fi.perPodVM)
	require.Equal(t, "firecracker-vm/fcweb0", fi.VmID)
	cfg, err := fs.getVMConfig(sandboxA)
	require.NoError(t, err)
	require.Equal(t, "10.200.0.2", cfg.guestIP)

	// StopPodSandbox only forwards for a shared-VM pod.
	_, err = fs.StopPodSandbox(ctx, &criapi.StopPodSandboxRequest{PodSandboxId: sandboxA})
	require.NoError(t, err)
	require.Equal(t, 1, fs.fcwebvm.refs, "shared VM is released by RemoveContainer only")
	require.Equal(t, []string{sandboxA}, stock.stopped)

	require.Empty(t, vms.created(), "no per-pod VM")
	require.Empty(t, slotsInUse(fs.podvms))
	require.Empty(t, stock.removedIDs())
}

func TestStopVMPerPodExit(t *testing.T) {
	// A coordinator with a nil orchestrator that is not in test mode: reaching
	// the firecracker-containerd path would dereference it.
	c := &coordinator{activeInstances: map[string]*funcInstance{}}
	require.NoError(t, c.insertActive("ctr-1", newPodFuncInstance("firecracker-vm/pod-1-aaaaaaaaaaaa")))
	require.NotPanics(t, func() {
		require.NoError(t, c.stopVM(context.Background(), "ctr-1"))
	})
	require.False(t, c.isActive("ctr-1"))
}

func TestPodVMSweepOnlyNumberedSlots(t *testing.T) {
	vms := &fakeVMs{}
	m := newPodVMs(vms.new)
	var removed []string
	m.removeDir = func(d string) error { removed = append(removed, d); return nil }

	vms.onCleanup = func(v *fakeVM) {
		if v.spec.Tap == "fcweb4" {
			v.f.cleanupErr = errors.New("tap fcweb4 still present")
		} else {
			v.f.cleanupErr = nil
		}
	}
	m.sweep()

	shared := sharedFcwebSpec()
	var taps, wantTaps, wantDirs []string
	for i, v := range vms.created() {
		require.Equal(t, slotFcwebSpec(i+1), v.spec)
		require.Equal(t, 1, v.cleanups)
		require.NotEqual(t, shared.Tap, v.spec.Tap)
		require.NotEqual(t, shared.Sock, v.spec.Sock)
		require.NotEqual(t, shared.Dir, v.spec.Dir)
		taps = append(taps, v.spec.Tap)
	}
	for n := 1; n <= fcwebMaxSlots; n++ {
		wantTaps = append(wantTaps, fmt.Sprintf("fcweb%d", n))
		if n != 4 {
			wantDirs = append(wantDirs, fmt.Sprintf("/run/vhive-fcweb/%d", n))
		}
	}
	require.Equal(t, wantTaps, taps)
	require.Equal(t, wantDirs, removed, "dirs of cleaned slots only; never /run/vhive-fcweb itself")
	require.Equal(t, []int{4}, slotsInUse(m), "slot that could not be cleaned is reserved")
	require.Empty(t, m.pods)
}
