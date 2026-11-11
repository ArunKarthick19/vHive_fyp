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

// Package guest holds what vhive and the guest init (fcinit) agree on: the
// image config baked into a rootfs, the per-VM config drive, and the pure
// logic fcinit uses to turn them into a process to start.
//
// It has no dependencies outside the standard library, so fcinit stays a
// small static binary.
package guest

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
)

// Paths inside the guest rootfs, written by the rootfs builder.
const (
	Dir             = "/.fcinit"
	InitPath        = Dir + "/init"       // the fcinit binary; the kernel's init=
	ImageConfigPath = Dir + "/image.json" // ImageConfig
	RWDir           = Dir + "/rw"         // tmpfs holding the overlay's upper and work dirs
	RootDir         = Dir + "/root"       // where the overlay root is mounted before chroot

	// ConfigDrive is the per-VM config drive (second virtio-blk device).
	ConfigDrive = "/dev/vdb"
	// ConfigDriveAlign is the size the config drive file is padded to.
	// Firecracker exposes a drive in 512-byte sectors and drops a partial
	// last sector, so the JSON must be followed by zero padding.
	ConfigDriveAlign = 4096
)

// ImageConfig is the part of an OCI image config fcinit needs. It is per
// image, so it is baked into the image's rootfs next to fcinit.
type ImageConfig struct {
	Ref        string   `json:"ref"`    // image reference it was built from (informational)
	Digest     string   `json:"digest"` // cache key digest (informational)
	Entrypoint []string `json:"entrypoint,omitempty"`
	Cmd        []string `json:"cmd,omitempty"`
	Env        []string `json:"env,omitempty"`
	WorkingDir string   `json:"workingDir,omitempty"`
	User       string   `json:"user,omitempty"`
}

// PodConfig is what differs per VM of the same image: the container env
// from the CRI request. It is delivered on the config drive.
type PodConfig struct {
	Env      []string `json:"env,omitempty"`
	Hostname string   `json:"hostname,omitempty"`
}

// EncodePodConfig returns the config drive contents for c: JSON, then zero
// padding up to a multiple of ConfigDriveAlign.
func EncodePodConfig(c PodConfig) ([]byte, error) {
	data, err := json.Marshal(c)
	if err != nil {
		return nil, err
	}
	n := (len(data)/ConfigDriveAlign + 1) * ConfigDriveAlign // at least one zero byte
	out := make([]byte, n)
	copy(out, data)
	return out, nil
}

// DecodePodConfig parses config drive contents: JSON up to the first NUL.
func DecodePodConfig(data []byte) (PodConfig, error) {
	var c PodConfig
	if i := bytes.IndexByte(data, 0); i >= 0 {
		data = data[:i]
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return c, errors.New("config drive is empty")
	}
	if err := json.Unmarshal(data, &c); err != nil {
		return c, fmt.Errorf("config drive: %w", err)
	}
	return c, nil
}
