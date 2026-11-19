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
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
	criapi "k8s.io/cri-api/pkg/apis/runtime/v1"
)

// kubectl logs for firecracker-vm://image (docs/DRAWBACKS_PLAN.md §2).
//
// kubelet reads a container's log from the file at ContainerStatus.LogPath,
// in the CRI log format. containerd writes the host placeholder's output
// there; the app in the VM writes to the serial console (console.log). A
// logForwarder tails console.log and appends the VM's lines to that same
// file. The file is opened per write with O_APPEND, so kubelet's log
// rotation (rename, then ReopenContainerLog for containerd) is followed
// without handling ReopenContainerLog here.

const (
	vmLogPollInterval = 250 * time.Millisecond
	vmLogReadMax      = 1 << 20 // bytes read from the console per poll
	vmLogLineMax      = 16 << 10
)

// firecrackerOwnLine matches the lines the Firecracker process itself
// prints on stdout (e.g. "2026-11-18T21:31:29.402416732 [anonymous-instance:main] Running Firecracker v1.13.1").
var firecrackerOwnLine = regexp.MustCompile(`^\d{4}-\d\d-\d\dT[0-9:.]+ \[[^\]]*:[a-z_]+\] `)

// keepConsoleLine reports whether a console line belongs in the container
// log: the app's output and fcinit's lines stay, Firecracker's own do not.
func keepConsoleLine(line []byte) bool {
	return !firecrackerOwnLine.Match(line)
}

// criLogLines turns console bytes into CRI log lines ("<RFC3339Nano> stdout F <line>").
// partial is an unterminated line carried over from the previous call; the
// returned rest is the new unterminated tail. Lines longer than vmLogLineMax
// are split into partial ("P") records, as containerd does. stdout and
// stderr share one pipe in the VM, so everything is "stdout".
func criLogLines(now time.Time, partial, data []byte) (out, rest []byte) {
	ts := now.Format(time.RFC3339Nano)
	buf := append(append([]byte(nil), partial...), data...)
	for {
		i := bytes.IndexByte(buf, '\n')
		if i < 0 {
			break
		}
		line := bytes.TrimRight(buf[:i], "\r")
		buf = buf[i+1:]
		if !keepConsoleLine(line) {
			continue
		}
		for len(line) > vmLogLineMax {
			out = append(out, ts+" stdout P "...)
			out = append(append(out, line[:vmLogLineMax]...), '\n')
			line = line[vmLogLineMax:]
		}
		out = append(out, ts+" stdout F "...)
		out = append(append(out, line...), '\n')
	}
	if len(buf) > vmLogLineMax { // a huge unterminated line: emit what we have
		out = append(out, ts+" stdout P "...)
		out = append(append(out, bytes.TrimRight(buf, "\r")...), '\n')
		buf = nil
	}
	return out, buf
}

// logForwarder copies one VM's console into one container log file.
type logForwarder struct {
	src, dst string
	offset   int64
	partial  []byte

	stopOnce sync.Once
	stopCh   chan struct{}
	done     chan struct{}
}

// startLogForwarder starts copying src (from byte offset) into dst.
func startLogForwarder(src, dst string, offset int64) *logForwarder {
	f := &logForwarder{src: src, dst: dst, offset: offset, stopCh: make(chan struct{}), done: make(chan struct{})}
	go f.run()
	return f
}

func (f *logForwarder) run() {
	defer close(f.done)
	t := time.NewTicker(vmLogPollInterval)
	defer t.Stop()
	for {
		select {
		case <-f.stopCh:
			f.copyNew(true) // final drain, including an unterminated last line
			return
		case <-t.C:
			f.copyNew(false)
		}
	}
}

// stop drains what is left and waits for the forwarder to finish. Idempotent.
func (f *logForwarder) stop() {
	f.stopOnce.Do(func() { close(f.stopCh) })
	<-f.done
}

func (f *logForwarder) copyNew(final bool) {
	for {
		data, err := readFrom(f.src, f.offset, vmLogReadMax)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			log.WithError(err).WithField("console", f.src).Debug("reading VM console for the container log")
		}
		f.offset += int64(len(data))
		out, rest := criLogLines(time.Now(), f.partial, data)
		f.partial = rest
		if final && len(data) < vmLogReadMax && len(f.partial) > 0 {
			o, _ := criLogLines(time.Now(), nil, append(f.partial, '\n'))
			out, f.partial = append(out, o...), nil
		}
		if len(out) > 0 {
			if err := appendFile(f.dst, out); err != nil {
				log.WithError(err).WithField("log", f.dst).Warn("writing the VM's output to the container log")
			}
		}
		if len(data) < vmLogReadMax {
			return
		}
	}
}

func readFrom(path string, offset int64, max int) ([]byte, error) {
	fh, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer fh.Close()
	buf := make([]byte, max)
	n, err := fh.ReadAt(buf, offset)
	if errors.Is(err, io.EOF) {
		err = nil
	}
	return buf[:n], err
}

// appendFile appends to path, creating it like containerd does (0640).
// Opening per write follows a rename by kubelet's log rotation.
func appendFile(path string, data []byte) error {
	fh, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o640)
	if err != nil {
		return err
	}
	if _, err := fh.Write(data); err != nil {
		fh.Close()
		return err
	}
	return fh.Close()
}

// startVMLog starts copying the sandbox's VM console into the log file
// kubelet reads for the container r creates, replacing a forwarder of an
// earlier container in the sandbox. fresh: the VM was just booted, so its
// whole console is this container's; otherwise only what comes next is.
func (fs *FirecrackerService) startVMLog(sandboxID string, r *criapi.CreateContainerRequest, fresh bool) {
	dir, rel := r.GetSandboxConfig().GetLogDirectory(), r.GetConfig().GetLogPath()
	src := fs.podvms.consolePath(sandboxID)
	if dir == "" || rel == "" || src == "" {
		return
	}
	var offset int64
	if !fresh {
		if st, err := os.Stat(src); err == nil {
			offset = st.Size()
		}
	}
	f := startLogForwarder(src, filepath.Join(dir, rel), offset)

	fs.exitMu.Lock()
	old := fs.vmLogs[sandboxID]
	if fs.vmLogs == nil {
		fs.vmLogs = make(map[string]*logForwarder)
	}
	fs.vmLogs[sandboxID] = f
	fs.exitMu.Unlock()
	if old != nil {
		old.stop()
	}
}

// stopVMLog stops the sandbox's forwarder after a last copy.
func (fs *FirecrackerService) stopVMLog(sandboxID string) {
	fs.exitMu.Lock()
	f := fs.vmLogs[sandboxID]
	delete(fs.vmLogs, sandboxID)
	fs.exitMu.Unlock()
	if f != nil {
		f.stop()
	}
}
