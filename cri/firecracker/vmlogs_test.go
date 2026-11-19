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
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/vhive-serverless/vhive/cri/firecracker/guest"
	criapi "k8s.io/cri-api/pkg/apis/runtime/v1"
)

var testNow = time.Date(2026, 11, 18, 21, 30, 0, 123456789, time.UTC)

const testTS = "2026-11-18T21:30:00.123456789Z"

func TestCRILogLines(t *testing.T) {
	console := "2026-11-18T21:31:29.402416732 [anonymous-instance:main] Running Firecracker v1.13.1\n" +
		"fcinit[0.06s]: root ready\r\n" +
		"httpd started\r\n" +
		"half a li"
	out, rest := criLogLines(testNow, nil, []byte(console))
	require.Equal(t, testTS+" stdout F fcinit[0.06s]: root ready\n"+testTS+" stdout F httpd started\n", string(out),
		"Firecracker's own line dropped, CRs dropped, CRI format")
	require.Equal(t, "half a li", string(rest))

	out, rest = criLogLines(testNow, rest, []byte("ne\nnext"))
	require.Equal(t, testTS+" stdout F half a line\n", string(out), "a line split across reads is joined")
	require.Equal(t, "next", string(rest))

	long := strings.Repeat("y", vmLogLineMax+10)
	out, rest = criLogLines(testNow, nil, []byte(long+"\n"))
	require.Empty(t, rest)
	lines := strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
	require.Len(t, lines, 2)
	require.True(t, strings.HasPrefix(lines[0], testTS+" stdout P "), "long line: partial record first")
	require.Equal(t, testTS+" stdout F "+strings.Repeat("y", 10), lines[1])
}

func waitForFile(t *testing.T, path, want string) {
	t.Helper()
	require.Eventually(t, func() bool {
		b, _ := os.ReadFile(path)
		return strings.Contains(string(b), want)
	}, 3*time.Second, 20*time.Millisecond, "%s should contain %q", path, want)
}

func appendTo(t *testing.T, path, s string) {
	t.Helper()
	require.NoError(t, appendFile(path, []byte(s)))
}

func TestLogForwarderFollowsRotationAndDrains(t *testing.T) {
	dir := t.TempDir()
	src, dst := filepath.Join(dir, "console.log"), filepath.Join(dir, "0.log")
	appendTo(t, src, "before\n")
	f := startLogForwarder(src, dst, int64(len("before\n"))) // reused VM: skip what was there
	appendTo(t, src, "one\n")
	waitForFile(t, dst, " stdout F one\n")

	// kubelet rotates: rename, new file appears on the next write.
	require.NoError(t, os.Rename(dst, dst+".rotated"))
	appendTo(t, src, "two\n")
	waitForFile(t, dst, " stdout F two\n")

	appendTo(t, src, "no newline at exit")
	f.stop()
	f.stop() // idempotent
	b, err := os.ReadFile(dst)
	require.NoError(t, err)
	require.Contains(t, string(b), " stdout F no newline at exit\n", "stop drains the last line")
	old, err := os.ReadFile(dst + ".rotated")
	require.NoError(t, err)
	require.NotContains(t, string(old)+string(b), "before", "output before the offset is not copied")
	require.NotContains(t, string(b), "one", "rotated file keeps the old lines")
}

func TestImageVMLogsGoToContainerLog(t *testing.T) {
	fs, vms, stock, _, _ := newImageTestService(t)
	fs.podvms.onAppExit = fs.imageAppExited
	vmDir := t.TempDir()
	fs.podvms.dirOf = func(int) string { return vmDir }
	logDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(logDir, "user-container"), 0o755))
	console := filepath.Join(vmDir, "console.log")
	appendTo(t, console, "fcinit[0.06s]: root ready\n")

	req := imageContainerReq(sandboxA)
	req.SandboxConfig.LogDirectory = logDir
	req.Config.LogPath = "user-container/0.log"
	_, err := fs.CreateContainer(context.Background(), req)
	require.NoError(t, err)

	appendTo(t, console, "app says hi\n")
	dst := filepath.Join(logDir, "user-container/0.log")
	waitForFile(t, dst, " stdout F app says hi\n")

	// The app exits: its last line lands in this attempt's log before the placeholder is stopped.
	appendTo(t, console, "fcinit[20.08s]: app exited (status 3, exit code 3); powering off\n")
	vms.created()[0].crash(vmExit{Status: guest.ExitStatus{ExitCode: 3, Reason: guest.ReasonExited}})
	require.Eventually(t, func() bool { return len(stock.stoppedCtrs()) == 1 }, 2*time.Second, 5*time.Millisecond)
	b, err := os.ReadFile(dst)
	require.NoError(t, err)
	require.Contains(t, string(b), "fcinit[0.06s]: root ready", "a fresh VM's whole console belongs to this attempt")
	require.Contains(t, string(b), "app exited (status 3, exit code 3)")
	fs.exitMu.Lock()
	require.Empty(t, fs.vmLogs, "forwarder stopped at the exit")
	fs.exitMu.Unlock()
}

func TestPodModeHasNoLogForwarder(t *testing.T) {
	fs, _, _ := newTestService(t)
	clearFcwebEnv(t)
	req := userContainerReq(sandboxA, firecrackerPodGuestImage)
	req.SandboxConfig = &criapi.PodSandboxConfig{LogDirectory: t.TempDir()}
	req.Config.LogPath = "user-container/0.log"
	_, err := fs.CreateContainer(context.Background(), req)
	require.NoError(t, err)
	require.Empty(t, fs.vmLogs, "firecracker-vm://pod logs as before")
}
