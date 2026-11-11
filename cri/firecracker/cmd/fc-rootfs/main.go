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

// fc-rootfs builds the read-only ext4 root filesystem that a
// firecracker-vm://image VM boots, from a container image reference, and
// caches it by digest. Run it before benchmarking so no cold start pays for
// a build:
//
//	fc-rootfs build ghcr.io/vhive-serverless/crccheck-hello-world:latest
//	fc-rootfs path  ghcr.io/vhive-serverless/crccheck-hello-world:latest
//
// No root needed. Paths: -cache (default $FC_IMAGE_CACHE or
// /users/Arun1903/fc-vm/images) and -init (default $FC_INIT or
// <cache>/fcinit, built with `make -C cri/firecracker fcinit`).
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/vhive-serverless/vhive/cri/firecracker/rootfs"
)

func main() {
	fs := flag.NewFlagSet("fc-rootfs", flag.ExitOnError)
	cache := fs.String("cache", rootfs.DefaultCacheDir(), "cache directory")
	initPath := fs.String("init", "", "fcinit binary (default <cache>/fcinit or $FC_INIT)")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: fc-rootfs [-cache DIR] [-init FILE] build|path IMAGE-REF...")
		fs.PrintDefaults()
	}
	if len(os.Args) < 2 {
		fs.Usage()
		os.Exit(2)
	}
	_ = fs.Parse(os.Args[1:])
	args := fs.Args()
	if len(args) < 2 || (args[0] != "build" && args[0] != "path") {
		fs.Usage()
		os.Exit(2)
	}
	ip := *initPath
	if ip == "" {
		if ip = os.Getenv("FC_INIT"); ip == "" {
			ip = *cache + "/fcinit"
		}
	}
	b := &rootfs.Builder{CacheDir: *cache, InitPath: ip, Source: rootfs.Remote{},
		Logf: func(f string, a ...any) { fmt.Fprintf(os.Stderr, f+"\n", a...) }}

	ctx := context.Background()
	rc := 0
	for _, ref := range args[1:] {
		if args[0] == "path" {
			res, ok, err := b.Lookup(ctx, ref)
			switch {
			case err != nil:
				fmt.Fprintf(os.Stderr, "%s: %v\n", ref, err)
				rc = 1
			case !ok:
				fmt.Fprintf(os.Stderr, "%s: not built (would be %s)\n", ref, res.Path)
				rc = 1
			default:
				fmt.Println(res.Path)
			}
			continue
		}
		start := time.Now()
		res, err := b.Ensure(ctx, ref)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", ref, err)
			rc = 1
			continue
		}
		state := "cached"
		if res.Built {
			state = "built in " + time.Since(start).Round(time.Millisecond).String()
		}
		fmt.Fprintf(os.Stderr, "%s -> %s (%s, %s)\n", ref, res.Path, res.Digest, state)
		fmt.Println(res.Path)
	}
	os.Exit(rc)
}
