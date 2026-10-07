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
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/vishvananda/netlink"
)

// Boots the real VM on the host: needs root, the rootfs from `fcvm.sh build`,
// and FCWEB_TEST=1. Build with `go test -c` and run the binary under sudo.
func TestFirecrackerVM(t *testing.T) {
	if os.Getenv("FCWEB_TEST") != "1" || os.Geteuid() != 0 {
		t.Skip("set FCWEB_TEST=1 and run as root")
	}

	requireClean := func(t *testing.T) {
		t.Helper()
		require.Empty(t, fcwebPids(), "firecracker still running")
		_, err := netlink.LinkByName(fcwebTap)
		require.Error(t, err, "tap still present")
		_, err = os.Stat(fcwebSock)
		require.ErrorIs(t, err, os.ErrNotExist, "socket still present")
	}
	get := func(t *testing.T, ip string) string {
		t.Helper()
		resp, err := http.Get("http://" + ip + "/")
		require.NoError(t, err)
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return string(body)
	}
	ctx := context.Background()

	t.Run("Refcount", func(t *testing.T) {
		vm := newFirecrackerVM("80")
		start := time.Now()
		ip, err := vm.acquire(ctx)
		require.NoError(t, err)
		t.Logf("cold acquire took %v", time.Since(start))
		require.Contains(t, get(t, ip), "hello from firecracker webvm")
		pid := vm.cmd.Process.Pid

		_, err = vm.acquire(ctx)
		require.NoError(t, err)
		require.Equal(t, pid, vm.cmd.Process.Pid, "second acquire must reuse the VM")

		require.NoError(t, vm.release(ctx))
		require.True(t, vm.running(), "VM stopped while still referenced")

		require.NoError(t, vm.release(ctx))
		requireClean(t)
	})

	t.Run("StaleProcessAndTap", func(t *testing.T) {
		// A VM left behind by a previous vHive run.
		old := newFirecrackerVM("80")
		_, err := old.acquire(ctx)
		require.NoError(t, err)
		oldPid := old.cmd.Process.Pid

		vm := newFirecrackerVM("80")
		ip, err := vm.acquire(ctx)
		require.NoError(t, err)
		require.NotEqual(t, oldPid, vm.cmd.Process.Pid)
		require.Equal(t, []int{vm.cmd.Process.Pid}, fcwebPids())
		require.Contains(t, get(t, ip), "hello from firecracker webvm")

		require.NoError(t, vm.release(ctx))
		requireClean(t)
	})

	t.Run("StaleTapOnly", func(t *testing.T) {
		require.NoError(t, createFcwebTap())
		vm := newFirecrackerVM("80")
		_, err := vm.acquire(ctx)
		require.NoError(t, err)
		require.NoError(t, vm.release(ctx))
		requireClean(t)
	})

	t.Run("FailedAcquire", func(t *testing.T) {
		// Nothing listens on 81: the VM boots but never becomes reachable.
		vm := newFirecrackerVM("81")
		ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		_, err := vm.acquire(ctx)
		require.Error(t, err)
		require.True(t, strings.Contains(err.Error(), "not reachable"), err.Error())
		require.Zero(t, vm.refs)
		requireClean(t)
	})
}
