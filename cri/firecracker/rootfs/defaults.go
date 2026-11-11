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

package rootfs

import "os"

// Defaults shared by vhive and the fc-rootfs CLI, so a disk pre-built by
// the CLI is the one vhive finds. vhive runs as root, where $HOME is /root,
// so the defaults are absolute paths in the user's fc-vm folder (like the
// FCWEB_ROOTFS default).
const (
	defaultCacheDir = "/users/Arun1903/fc-vm/images"
	defaultInitName = "fcinit"
)

// DefaultCacheDir is $FC_IMAGE_CACHE, or /users/Arun1903/fc-vm/images.
func DefaultCacheDir() string {
	if d := os.Getenv("FC_IMAGE_CACHE"); d != "" {
		return d
	}
	return defaultCacheDir
}

// DefaultInitPath is $FC_INIT, or fcinit inside the cache dir.
func DefaultInitPath() string {
	if p := os.Getenv("FC_INIT"); p != "" {
		return p
	}
	return DefaultCacheDir() + "/" + defaultInitName
}

// NewDefault is a registry-backed Builder using the default paths.
func NewDefault() *Builder {
	return &Builder{CacheDir: DefaultCacheDir(), InitPath: DefaultInitPath(), Source: Remote{}}
}
