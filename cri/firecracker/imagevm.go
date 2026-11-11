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
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/vhive-serverless/vhive/cri/firecracker/guest"
	"github.com/vhive-serverless/vhive/cri/firecracker/rootfs"
	criapi "k8s.io/cri-api/pkg/apis/runtime/v1"
)

// firecracker-vm://image: a per-pod Firecracker VM (same slots, teardown and
// FC_VCPUS/FC_MEM_MIB sizing as firecracker-vm://pod) that boots the
// function's own container image instead of the baked-in rootfs. See
// docs/DESIGN.md.
const (
	firecrackerImageGuestImage = "firecracker-vm://image"

	// fcImageEnv overrides the image to boot; by default it is the image of
	// the CRI request, i.e. the ksvc's `image:`.
	fcImageEnv = "FC_IMAGE"
	// defaultImagePort is Knative's default container port, used when the
	// ksvc sets neither GUEST_PORT nor PORT.
	defaultImagePort = "8080"
)

// imageDisks returns the read-only rootfs for an image ref, building it on
// a cache miss. *rootfs.Builder implements it; tests use a fake.
type imageDisks interface {
	Ensure(ctx context.Context, ref string) (rootfs.Result, error)
}

// imageNamer maps the CRI request's image (often an image ID, sha256:...)
// to a reference a registry understands.
type imageNamer func(ctx context.Context, image string) (string, error)

// isImageID reports whether image is a bare image ID rather than a name.
// Kubelet sends the ID it got back from PullImage.
func isImageID(image string) bool {
	if strings.HasPrefix(image, "sha256:") {
		return true
	}
	return len(image) == 64 && strings.Trim(image, "0123456789abcdef") == ""
}

// stockImageNamer asks containerd (ImageStatus) for a pullable name of an
// image ID: a repo digest (name@sha256:..., resolvable without a registry
// round trip) if there is one, else a repo tag.
func stockImageNamer(client criapi.ImageServiceClient) imageNamer {
	return func(ctx context.Context, image string) (string, error) {
		if !isImageID(image) {
			return image, nil
		}
		if client == nil {
			return "", fmt.Errorf("image %s is an image ID and no image service is available to name it; set %s", image, fcImageEnv)
		}
		resp, err := client.ImageStatus(ctx, &criapi.ImageStatusRequest{Image: &criapi.ImageSpec{Image: image}})
		if err != nil {
			return "", fmt.Errorf("ImageStatus %s: %w", image, err)
		}
		img := resp.GetImage()
		if img == nil {
			return "", fmt.Errorf("ImageStatus %s: image not found", image)
		}
		if d := img.GetRepoDigests(); len(d) > 0 {
			return d[0], nil
		}
		if t := img.GetRepoTags(); len(t) > 0 {
			return t[0], nil
		}
		return "", fmt.Errorf("image %s has no repo digest or tag; set %s", image, fcImageEnv)
	}
}

// imagePort is the app's port: GUEST_PORT (the variable the stock path
// uses), else PORT, else Knative's default 8080.
func imagePort(get func(string) string) (string, error) {
	p := get(guestPortEnv)
	if p == "" {
		p = get("PORT")
	}
	if p == "" {
		return defaultImagePort, nil
	}
	if n, err := strconv.Atoi(p); err != nil || n < 1 || n > 65535 {
		return "", fmt.Errorf("port %q: want 1-65535", p)
	}
	return p, nil
}

// imageVMConfig turns a user container request into the config of its
// image VM, and returns the port queue-proxy must forward to.
func (fs *FirecrackerService) imageVMConfig(ctx context.Context, r *criapi.CreateContainerRequest) (fcwebConfig, string, error) {
	config := r.GetConfig()
	get := containerEnvGetter(config)
	if get("FC_ROOTFS") != "" {
		return fcwebConfig{}, "", errors.New("FC_ROOTFS cannot be combined with " + firecrackerImageGuestImage + "; the rootfs is built from the image")
	}
	cfg, err := fcwebConfigFromEnv(get)
	if err != nil {
		return cfg, "", err
	}
	port, err := imagePort(get)
	if err != nil {
		return cfg, "", err
	}

	ref := get(fcImageEnv)
	if ref == "" {
		if fs.nameImage == nil {
			return cfg, "", errors.New("no image namer configured")
		}
		if ref, err = fs.nameImage(ctx, config.GetImage().GetImage()); err != nil {
			return cfg, "", err
		}
	}
	if ref == "" {
		return cfg, "", fmt.Errorf("no image: the request has none and %s is unset", fcImageEnv)
	}
	if fs.images == nil {
		return cfg, "", errors.New("no image rootfs builder configured")
	}
	start := time.Now()
	disk, err := fs.images.Ensure(ctx, ref)
	if err != nil {
		return cfg, "", fmt.Errorf("rootfs for %s: %w", ref, err)
	}
	if disk.Built {
		log.WithFields(log.Fields{"image": ref, "took": time.Since(start), "rootfs": disk.Path}).
			Warn("built rootfs during cold start; this cold start is not representative, pre-build with fc-rootfs")
	}

	hostname := r.GetSandboxConfig().GetHostname()
	if hostname == "" {
		hostname = r.GetSandboxConfig().GetMetadata().GetName()
	}
	// The whole container env, as the stock path passes it to its container.
	podEnv := make([]string, 0, len(config.GetEnvs()))
	for _, kv := range config.GetEnvs() {
		podEnv = append(podEnv, kv.GetKey()+"="+kv.GetValue())
	}
	drive, err := guest.EncodePodConfig(guest.PodConfig{Env: podEnv, Hostname: hostname})
	if err != nil {
		return cfg, "", err
	}

	cfg.Rootfs = disk.Path
	cfg.Init = guest.InitPath
	cfg.Port = port
	cfg.ProbeTCP = true
	cfg.GuestConfig = string(drive)
	return cfg, port, nil
}
