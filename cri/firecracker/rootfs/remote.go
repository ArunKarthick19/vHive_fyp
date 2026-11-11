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

import (
	"context"
	"fmt"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

// Remote is a Source backed by container registries. Public images need no
// credentials; private ones use the Docker keychain (~/.docker/config.json
// of the user running the build).
type Remote struct {
	Options []remote.Option // extra options, e.g. a transport in tests
}

var amd64 = v1.Platform{OS: "linux", Architecture: "amd64"}

func (r Remote) opts(ctx context.Context) []remote.Option {
	return append([]remote.Option{
		remote.WithContext(ctx),
		remote.WithAuthFromKeychain(authn.DefaultKeychain),
		remote.WithPlatform(amd64),
	}, r.Options...)
}

// Digest returns the digest ref names. A digest reference is answered
// locally; a tag costs one HEAD request to the registry.
func (r Remote) Digest(ctx context.Context, ref string) (string, error) {
	n, err := name.ParseReference(ref)
	if err != nil {
		return "", err
	}
	if d, ok := n.(name.Digest); ok {
		return d.DigestStr(), nil
	}
	desc, err := remote.Head(n, r.opts(ctx)...)
	if err != nil {
		return "", err
	}
	return desc.Digest.String(), nil
}

// Image returns ref's linux/amd64 image (resolving a multi-arch index).
func (r Remote) Image(ctx context.Context, ref string) (v1.Image, error) {
	n, err := name.ParseReference(ref)
	if err != nil {
		return nil, err
	}
	img, err := remote.Image(n, r.opts(ctx)...)
	if err != nil {
		return nil, fmt.Errorf("pulling %s: %w", ref, err)
	}
	return img, nil
}
