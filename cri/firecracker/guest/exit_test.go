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
	"errors"
	"strings"
	"testing"
)

func TestAppExitStatus(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		exited, signaled       bool
		code, signo            int
		signame                string
		wantCode               int
		wantReason, wantSignal string
	}{
		{"exit 0", true, false, 0, 0, "", 0, ReasonExited, ""},
		{"exit 3", true, false, 3, 0, "", 3, ReasonExited, ""},
		{"SIGKILL", false, true, 0, 9, "killed", 137, ReasonSignaled, "killed"},
		{"SIGSEGV", false, true, 0, 11, "segmentation fault", 139, ReasonSignaled, "segmentation fault"},
		{"neither", false, false, 0, 0, "", 255, ReasonExited, ""},
	} {
		got := AppExitStatus(tc.exited, tc.code, tc.signaled, tc.signo, tc.signame)
		if got.ExitCode != tc.wantCode || got.Reason != tc.wantReason || got.Signal != tc.wantSignal {
			t.Errorf("%s: got %+v, want code %d reason %s signal %q", tc.name, got, tc.wantCode, tc.wantReason, tc.wantSignal)
		}
	}
}

func TestExitStatusRoundTrip(t *testing.T) {
	in := ExitStatus{ExitCode: 3, Reason: ReasonExited, Message: "bye"}
	data, err := EncodeExitStatus(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != StatusDriveSize {
		t.Fatalf("encoded size %d, want %d", len(data), StatusDriveSize)
	}
	out, err := DecodeExitStatus(data)
	if err != nil || out != in {
		t.Fatalf("round trip: got %+v, %v; want %+v", out, err, in)
	}
}

func TestExitStatusLongMessageIsShortened(t *testing.T) {
	data, err := EncodeExitStatus(ExitStatus{ExitCode: 1, Reason: ReasonInitError, Message: strings.Repeat("x", 3*StatusDriveSize)})
	if err != nil {
		t.Fatal(err)
	}
	out, err := DecodeExitStatus(data)
	if err != nil || out.ExitCode != 1 || out.Reason != ReasonInitError || len(out.Message) == 0 || len(out.Message) >= StatusDriveSize {
		t.Fatalf("got %+v (message %d bytes), %v", out, len(out.Message), err)
	}
}

func TestDecodeExitStatusEmptyAndBad(t *testing.T) {
	if _, err := DecodeExitStatus(make([]byte, StatusDriveSize)); !errors.Is(err, ErrNoExitStatus) {
		t.Errorf("zero drive: got %v, want ErrNoExitStatus", err)
	}
	if _, err := DecodeExitStatus(nil); !errors.Is(err, ErrNoExitStatus) {
		t.Errorf("nil: got %v, want ErrNoExitStatus", err)
	}
	if _, err := DecodeExitStatus([]byte("{not json")); err == nil || errors.Is(err, ErrNoExitStatus) {
		t.Errorf("garbage: got %v, want a parse error", err)
	}
	if _, err := DecodeExitStatus([]byte(`{"exitCode":3}`)); err == nil {
		t.Errorf("record without reason: got nil error")
	}
}
