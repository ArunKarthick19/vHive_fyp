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

package guest

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
)

// The status drive: a small writable virtio disk (third drive, after the
// rootfs and the config drive) on which fcinit records how the app ended,
// so that vhive can report it to Kubernetes after the VM has powered off.
const (
	StatusDrive     = "/dev/vdc"
	StatusDriveSize = 4096
)

// Exit reasons recorded by fcinit.
const (
	ReasonExited    = "Exited"    // the app exited on its own (any code, including 0)
	ReasonSignaled  = "Signaled"  // the app was killed by a signal
	ReasonInitError = "InitError" // fcinit itself failed (before or while starting the app)
)

// InitErrorCode is the exit code reported when fcinit fails before the app
// runs: the code a shell uses for "cannot execute".
const InitErrorCode = 126

// ExitStatus is how the app in an image VM ended.
type ExitStatus struct {
	ExitCode int    `json:"exitCode"`
	Reason   string `json:"reason"`
	Signal   string `json:"signal,omitempty"`
	Message  string `json:"message,omitempty"`
}

// ErrNoExitStatus means the status drive holds no record: the VM stopped
// without fcinit writing one (kernel panic, out of memory, killed from
// outside) or it is still running.
var ErrNoExitStatus = errors.New("no exit status recorded")

// AppExitStatus maps a wait status to an ExitStatus the way container
// runtimes do: a normal exit keeps its code, death by signal n is 128+n.
func AppExitStatus(exited bool, code int, signaled bool, signo int, signame string) ExitStatus {
	if signaled {
		return ExitStatus{ExitCode: 128 + signo, Reason: ReasonSignaled, Signal: signame}
	}
	if !exited {
		// Stopped or continued is not an end; treat defensively as unknown.
		return ExitStatus{ExitCode: 255, Reason: ReasonExited, Message: "unexpected wait status"}
	}
	return ExitStatus{ExitCode: code, Reason: ReasonExited}
}

// EncodeExitStatus returns the status drive contents: JSON, zero padded to
// StatusDriveSize. A message too long to fit is shortened.
func EncodeExitStatus(s ExitStatus) ([]byte, error) {
	for {
		data, err := json.Marshal(s)
		if err != nil {
			return nil, err
		}
		if len(data) < StatusDriveSize {
			out := make([]byte, StatusDriveSize)
			copy(out, data)
			return out, nil
		}
		if s.Message == "" {
			return nil, fmt.Errorf("exit status does not fit in %d bytes", StatusDriveSize)
		}
		s.Message = s.Message[:len(s.Message)/2]
	}
}

// DecodeExitStatus parses status drive contents. An all-zero drive (the
// state vhive creates it in) gives ErrNoExitStatus.
func DecodeExitStatus(data []byte) (ExitStatus, error) {
	var s ExitStatus
	if i := bytes.IndexByte(data, 0); i >= 0 {
		data = data[:i]
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return s, ErrNoExitStatus
	}
	if err := json.Unmarshal(data, &s); err != nil {
		return s, fmt.Errorf("status drive: %w", err)
	}
	if s.Reason == "" {
		return s, fmt.Errorf("status drive: record has no reason")
	}
	return s, nil
}
