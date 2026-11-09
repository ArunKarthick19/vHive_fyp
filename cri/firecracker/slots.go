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
	"fmt"
	"net"
	"strconv"
	"sync"
)

// Per-pod Firecracker VMs (firecracker-vm://pod) each use one numbered slot.
// Slot 0 is the shared VM (sharedFcwebSpec) and is never handed out.
const fcwebMaxSlots = 16

// slotFcwebSpec is the identity of the VM in slot n (1..fcwebMaxSlots): tap
// fcweb<n>, the /30 10.200.0.4n/30 with the host on .4n+1 and the guest on
// .4n+2, a MAC of 06:00 followed by the guest IP bytes, and its own directory
// under fcwebRunDir. Slot 0 would give back the shared VM's values.
func slotFcwebSpec(n int) fcwebSpec {
	host := net.IPv4(10, 200, 0, byte(4*n+1)).To4()
	guest := net.IPv4(10, 200, 0, byte(4*n+2)).To4()
	dir := fcwebRunDir + "/" + strconv.Itoa(n)
	return fcwebSpec{
		Tap:     "fcweb" + strconv.Itoa(n),
		HostIP:  host.String(),
		GuestIP: guest.String(),
		MAC:     fmt.Sprintf("06:00:%02x:%02x:%02x:%02x", guest[0], guest[1], guest[2], guest[3]),
		Dir:     dir,
		Sock:    dir + "/fc.sock",
	}
}

// slotAllocator hands out slots 1..fcwebMaxSlots, lowest free first.
type slotAllocator struct {
	mu    sync.Mutex
	taken [fcwebMaxSlots + 1]bool // index 0 is the shared VM, never used
}

func newSlotAllocator() *slotAllocator {
	return &slotAllocator{}
}

func validSlot(n int) bool { return n >= 1 && n <= fcwebMaxSlots }

// alloc returns the lowest free slot.
func (a *slotAllocator) alloc() (int, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for n := 1; n <= fcwebMaxSlots; n++ {
		if !a.taken[n] {
			a.taken[n] = true
			return n, nil
		}
	}
	return 0, fmt.Errorf("no free per-pod Firecracker VM slot: all %d are in use", fcwebMaxSlots)
}

// free returns slot n to the pool. Freeing a free or invalid slot does nothing.
// Call it only once the slot's VM, tap and socket are verifiably gone.
func (a *slotAllocator) free(n int) {
	if !validSlot(n) {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.taken[n] = false
}

// reserve marks slot n as taken whether or not it already was. It is used
// when cleaning up a slot failed, so that the leftover tap or process is never
// handed to a new VM.
func (a *slotAllocator) reserve(n int) error {
	if !validSlot(n) {
		return fmt.Errorf("invalid Firecracker VM slot %d: want 1-%d", n, fcwebMaxSlots)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.taken[n] = true
	return nil
}

// inUse reports whether slot n is taken.
func (a *slotAllocator) inUse(n int) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return validSlot(n) && a.taken[n]
}
