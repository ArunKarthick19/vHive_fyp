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
	"net"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

// allSpecs is the shared VM followed by every per-pod slot.
func allSpecs() []fcwebSpec {
	specs := []fcwebSpec{sharedFcwebSpec()}
	for n := 1; n <= fcwebMaxSlots; n++ {
		specs = append(specs, slotFcwebSpec(n))
	}
	return specs
}

func TestSlotFcwebSpecValues(t *testing.T) {
	require.Equal(t, fcwebSpec{
		Tap:     "fcweb1",
		HostIP:  "10.200.0.5",
		GuestIP: "10.200.0.6",
		MAC:     "06:00:0a:c8:00:06",
		Dir:     "/run/vhive-fcweb/1",
		Sock:    "/run/vhive-fcweb/1/fc.sock",
	}, slotFcwebSpec(1))
	require.Equal(t, fcwebSpec{
		Tap:     "fcweb16",
		HostIP:  "10.200.0.65",
		GuestIP: "10.200.0.66",
		MAC:     "06:00:0a:c8:00:42",
		Dir:     "/run/vhive-fcweb/16",
		Sock:    "/run/vhive-fcweb/16/fc.sock",
	}, slotFcwebSpec(16))
	// The numbering rule reproduces the shared VM at slot 0.
	require.Equal(t, sharedFcwebSpec().Tap, slotFcwebSpec(0).Tap)
	require.Equal(t, sharedFcwebSpec().HostIP, slotFcwebSpec(0).HostIP)
	require.Equal(t, sharedFcwebSpec().GuestIP, slotFcwebSpec(0).GuestIP)
	require.Equal(t, sharedFcwebSpec().MAC, slotFcwebSpec(0).MAC)
}

func TestSlotSpecsDoNotOverlap(t *testing.T) {
	specs := allSpecs()
	require.Len(t, specs, 17)

	_, all, err := net.ParseCIDR("10.200.0.0/24")
	require.NoError(t, err)

	nets := make([]*net.IPNet, len(specs))
	for i, s := range specs {
		require.LessOrEqual(t, len(s.Tap), 15, "IFNAMSIZ-1: %q", s.Tap)

		hostIP, hostNet, err := net.ParseCIDR(s.HostIP + "/30")
		require.NoError(t, err)
		guestIP := net.ParseIP(s.GuestIP)
		require.True(t, hostNet.Contains(guestIP), "%s: guest %s outside host /30 %s", s.Tap, s.GuestIP, hostNet)
		require.True(t, all.Contains(hostIP))
		// Neither end may be the network or broadcast address of the /30.
		require.Equal(t, hostNet.IP.To4()[3]+1, hostIP.To4()[3], s.Tap)
		require.Equal(t, hostNet.IP.To4()[3]+2, guestIP.To4()[3], s.Tap)
		nets[i] = hostNet

		mac, err := net.ParseMAC(s.MAC)
		require.NoError(t, err)
		require.Equal(t, []byte{0x06, 0x00}, []byte(mac[:2]), "locally administered unicast prefix")
		require.Equal(t, []byte(guestIP.To4()), []byte(mac[2:]), "MAC encodes guest IP")

		require.Equal(t, filepath.Join(s.Dir, "fc.sock"), s.Sock)
	}

	for i := range specs {
		for j := range specs {
			if i == j {
				continue
			}
			a, b := specs[i], specs[j]
			require.NotEqual(t, a.Tap, b.Tap)
			require.NotEqual(t, a.MAC, b.MAC)
			require.NotEqual(t, a.Sock, b.Sock)
			require.NotEqual(t, a.Dir, b.Dir)
			require.False(t, nets[i].Contains(nets[j].IP), "%s %s overlaps %s %s", a.Tap, nets[i], b.Tap, nets[j])
			// No file one VM writes is a file another VM writes.
			for _, f := range []string{"fc.sock", "console.log", "vm-config.json", "firecracker.log"} {
				for _, g := range []string{"fc.sock", "console.log", "vm-config.json", "firecracker.log"} {
					require.NotEqual(t, filepath.Join(a.Dir, f), filepath.Join(b.Dir, g))
				}
			}
			// The shared VM's process match must not catch a slot VM and vice versa.
			cmd := []byte(strings.Join([]string{"firecracker", "--api-sock", a.Sock, "--config-file", "x"}, "\x00"))
			require.False(t, cmdlineUsesSock(cmd, b.Sock), "%s matched %s", a.Sock, b.Sock)
		}
	}
}

func TestSlotAllocatorLowestFirstAndExhaustion(t *testing.T) {
	a := newSlotAllocator()
	for want := 1; want <= fcwebMaxSlots; want++ {
		n, err := a.alloc()
		require.NoError(t, err)
		require.Equal(t, want, n)
	}

	_, err := a.alloc()
	require.Error(t, err)
	require.True(t, strings.HasSuffix(err.Error(), "all 16 are in use"), err.Error())

	a.free(9)
	a.free(3)
	a.free(12)
	for _, want := range []int{3, 9, 12} {
		n, err := a.alloc()
		require.NoError(t, err)
		require.Equal(t, want, n, "freed slots are reused lowest first")
	}
	_, err = a.alloc()
	require.Error(t, err)
}

func TestSlotAllocatorFreeIsIdempotent(t *testing.T) {
	a := newSlotAllocator()
	n, err := a.alloc()
	require.NoError(t, err)
	a.free(n)
	a.free(n)
	a.free(0)  // the shared VM's slot: ignored
	a.free(17) // out of range: ignored

	for want := 1; want <= fcwebMaxSlots; want++ {
		got, err := a.alloc()
		require.NoError(t, err)
		require.Equal(t, want, got, "double free must not hand a slot out twice")
	}
	_, err = a.alloc()
	require.Error(t, err)
}

func TestSlotAllocatorReserve(t *testing.T) {
	a := newSlotAllocator()
	n, err := a.alloc()
	require.NoError(t, err)
	require.Equal(t, 1, n)

	// Cleanup of slot 1 failed: it stays taken.
	require.NoError(t, a.reserve(1))
	require.True(t, a.inUse(1))
	got, err := a.alloc()
	require.NoError(t, err)
	require.Equal(t, 2, got)

	// A slot not currently allocated can be reserved (e.g. a leftover found by
	// the startup sweep that could not be removed).
	require.NoError(t, a.reserve(5))
	for _, want := range []int{3, 4, 6} {
		got, err := a.alloc()
		require.NoError(t, err)
		require.Equal(t, want, got)
	}

	require.Error(t, a.reserve(0), "slot 0 is the shared VM")
	require.Error(t, a.reserve(17))
	require.False(t, a.inUse(0))
}

// Run with -race -count=20.
func TestSlotAllocatorParallel(t *testing.T) {
	const workers = 64

	t.Run("one shot", func(t *testing.T) {
		a := newSlotAllocator()
		var (
			wg    sync.WaitGroup
			mu    sync.Mutex
			got   = map[int]int{}
			fails int
		)
		start := make(chan struct{})
		for i := 0; i < workers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				n, err := a.alloc()
				mu.Lock()
				defer mu.Unlock()
				if err != nil {
					if !strings.HasSuffix(err.Error(), "all 16 are in use") {
						t.Errorf("unexpected error: %v", err)
					}
					fails++
					return
				}
				got[n]++
			}()
		}
		close(start)
		wg.Wait()

		require.Len(t, got, fcwebMaxSlots)
		for n, c := range got {
			require.True(t, validSlot(n), "slot %d", n)
			require.Equal(t, 1, c, "slot %d handed out %d times", n, c)
		}
		require.Equal(t, workers-fcwebMaxSlots, fails)
	})

	t.Run("churn", func(t *testing.T) {
		a := newSlotAllocator()
		var owners [fcwebMaxSlots + 1]atomic.Int32
		var wg sync.WaitGroup
		for i := 1; i <= workers; i++ {
			wg.Add(1)
			go func(id int32) {
				defer wg.Done()
				for k := 0; k < 200; k++ {
					n, err := a.alloc()
					if err != nil {
						continue
					}
					if !owners[n].CompareAndSwap(0, id) {
						t.Errorf("slot %d given to %d while held by %d", n, id, owners[n].Load())
						return
					}
					owners[n].Store(0)
					a.free(n)
				}
			}(int32(i))
		}
		wg.Wait()
		for n := 1; n <= fcwebMaxSlots; n++ {
			require.False(t, a.inUse(n), "slot %d leaked", n)
		}
	})
}
