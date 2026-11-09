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
	"os"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
)

// podVMHandle is what the per-pod pathway needs from one Firecracker VM.
// *firecrackerVM implements it; tests use a fake.
type podVMHandle interface {
	// coldBoot cleans up leftovers, boots with cfg and waits until the guest
	// answers. On failure it has already tried to clean up.
	coldBoot(ctx context.Context, cfg fcwebConfig) error
	running() bool
	// cleanup stops the VM and removes its tap and socket, and fails if any of
	// them is still there. It is idempotent.
	cleanup() error
	guestIP() string
}

func newRealPodVM(spec fcwebSpec) podVMHandle {
	return &firecrackerVM{spec: spec, port: firecrackerGuestPort}
}

// podVM is the Firecracker VM of one pod sandbox (firecracker-vm://pod).
type podVM struct {
	// Serializes boot and teardown of this sandbox's VM, so parallel creates
	// in one sandbox boot it once and a stop cannot race a boot.
	sync.Mutex

	sandboxID string
	slot      int
	vmID      string
	vm        podVMHandle
	cfg       fcwebConfig
	gone      bool // torn down; the sandbox needs a new podVM
}

// podVMs owns every per-pod VM, keyed by pod sandbox ID.
type podVMs struct {
	mu    sync.Mutex
	slots *slotAllocator
	pods  map[string]*podVM

	newVM     func(fcwebSpec) podVMHandle
	removeDir func(string) error
}

func newPodVMs(newVM func(fcwebSpec) podVMHandle) *podVMs {
	return &podVMs{
		slots:     newSlotAllocator(),
		pods:      make(map[string]*podVM),
		newVM:     newVM,
		removeDir: os.RemoveAll,
	}
}

// podVMID is unique per VM: the slot tells VMs alive at the same time apart,
// the sandbox ID prefix tells apart VMs that reused a slot.
func podVMID(slot int, sandboxID string) string {
	if len(sandboxID) > 12 {
		sandboxID = sandboxID[:12]
	}
	return fmt.Sprintf("firecracker-vm/pod-%d-%s", slot, sandboxID)
}

func (p *podVM) logger() *log.Entry {
	return log.WithFields(log.Fields{"vmID": p.vmID, "sandbox": p.sandboxID, "slot": p.slot})
}

// acquire returns the guest IP of the sandbox's VM, booting it if it is not
// running. fresh is true when this call booted it; the caller then owns
// tearing it down if the rest of its container create fails. A container
// restart in the same sandbox reuses the running VM with fresh false.
func (m *podVMs) acquire(ctx context.Context, sandboxID string, cfg fcwebConfig) (ip, vmID string, fresh bool, err error) {
	m.mu.Lock()
	p, ok := m.pods[sandboxID]
	if !ok {
		slot, err := m.slots.alloc()
		if err != nil {
			m.mu.Unlock()
			return "", "", false, err
		}
		p = &podVM{
			sandboxID: sandboxID,
			slot:      slot,
			vmID:      podVMID(slot, sandboxID),
			vm:        m.newVM(slotFcwebSpec(slot)),
		}
		m.pods[sandboxID] = p
	}
	m.mu.Unlock()

	p.Lock()
	defer p.Unlock()

	if p.gone {
		return "", "", false, fmt.Errorf("Firecracker VM %s for sandbox %s was torn down while this container was being created", p.vmID, sandboxID)
	}
	if p.vm.running() {
		if cfg != p.cfg {
			p.logger().WithFields(log.Fields{"running": p.cfg, "wanted": cfg}).Warn("reusing the sandbox's running VM; its size/image is unchanged")
		}
		p.logger().Info("per-pod Firecracker VM already running, reusing it")
		return p.vm.guestIP(), p.vmID, false, nil
	}

	start := time.Now()
	if err := p.vm.coldBoot(ctx, cfg); err != nil {
		if terr := m.teardownLocked(p); terr != nil {
			p.logger().WithError(terr).Error("failed to clean up after failed boot")
		}
		return "", "", false, err
	}
	p.cfg = cfg
	p.logger().WithFields(log.Fields{"ip": p.vm.guestIP(), "took": time.Since(start)}).Info("per-pod Firecracker VM reachable")
	return p.vm.guestIP(), p.vmID, true, nil
}

// stop tears down the sandbox's VM. Stopping a sandbox with no VM, or one
// already stopped, does nothing.
func (m *podVMs) stop(sandboxID string) error {
	m.mu.Lock()
	p := m.pods[sandboxID]
	m.mu.Unlock()
	if p == nil {
		return nil
	}

	p.Lock()
	defer p.Unlock()
	if p.gone {
		return nil
	}
	start := time.Now()
	if err := m.teardownLocked(p); err != nil {
		return err
	}
	p.logger().WithField("took", time.Since(start)).Info("per-pod Firecracker VM stopped")
	return nil
}

// teardownLocked cleans up p's VM and forgets it. The slot is freed only
// once cleanup has succeeded; otherwise it stays reserved so no other pod
// gets a tap or socket something may still hold. The caller holds p's lock.
func (m *podVMs) teardownLocked(p *podVM) error {
	err := p.vm.cleanup()

	m.mu.Lock()
	defer m.mu.Unlock()
	p.gone = true
	if m.pods[p.sandboxID] == p {
		delete(m.pods, p.sandboxID)
	}
	if err != nil {
		_ = m.slots.reserve(p.slot)
		p.logger().WithError(err).Error("per-pod Firecracker VM cleanup failed, keeping its slot reserved")
		return err
	}
	m.slots.free(p.slot)
	return nil
}

// sweep removes what a previous vHive run left in the per-pod slots 1..16:
// firecracker processes on their sockets, taps fcweb1..fcweb16 and the
// directories /run/vhive-fcweb/<n>. It never touches the shared VM (fcweb0,
// /run/vhive-fcweb/fc.sock). A slot that cannot be cleaned is reserved.
func (m *podVMs) sweep() {
	var errs []error
	for n := 1; n <= fcwebMaxSlots; n++ {
		spec := slotFcwebSpec(n)
		if err := m.newVM(spec).cleanup(); err != nil {
			_ = m.slots.reserve(n)
			errs = append(errs, fmt.Errorf("slot %d: %w", n, err))
			continue
		}
		if err := m.removeDir(spec.Dir); err != nil {
			log.WithError(err).WithField("dir", spec.Dir).Warn("failed to remove per-pod VM directory")
		}
	}
	if err := errors.Join(errs...); err != nil {
		log.WithError(err).Error("startup sweep could not clean every per-pod Firecracker slot; those slots stay reserved")
	}
}
