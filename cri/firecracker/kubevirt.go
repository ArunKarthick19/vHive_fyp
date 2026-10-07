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
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
)

// Prototype: talks to the API server through kubectl rather than client-go to
// avoid adding Kubernetes dependencies to vHive.
const (
	kubevirtKubeconfig   = "/etc/kubernetes/admin.conf"
	kubevirtPollInterval = 500 * time.Millisecond
	kubevirtStopTimeout  = 3 * time.Minute
)

// kubevirtVM is a pre-defined KubeVirt VirtualMachine shared by every pod of a
// function. It is started when the first pod needs it and stopped (not deleted)
// when the last pod using it is removed.
type kubevirtVM struct {
	// Serializes start and stop so a pod being removed cannot stop the VM while
	// a new pod is starting it.
	sync.Mutex

	namespace string
	name      string
	port      string
	refs      int
}

func newKubevirtVM(namespace, name, port string) *kubevirtVM {
	return &kubevirtVM{namespace: namespace, name: name, port: port}
}

func (vm *kubevirtVM) logger() *log.Entry {
	return log.WithField("kubevirtVM", vm.namespace+"/"+vm.name)
}

// acquire starts the VM if needed, waits until its guest port answers HTTP and
// returns the guest IP. Each successful acquire must be paired with a release.
func (vm *kubevirtVM) acquire(ctx context.Context) (string, error) {
	vm.Lock()
	defer vm.Unlock()

	start := time.Now()
	if err := vm.setRunning(ctx, true); err != nil {
		return "", err
	}

	ip, err := vm.waitReachable(ctx)
	if err != nil {
		return "", err
	}

	vm.refs++
	vm.logger().WithFields(log.Fields{"ip": ip, "refs": vm.refs, "took": time.Since(start)}).Info("KubeVirt VM reachable")
	return ip, nil
}

// release stops the VM once no pod uses it, and waits for its VMI to be gone so
// a following acquire cannot mistake the terminating instance for a ready one.
func (vm *kubevirtVM) release(ctx context.Context) error {
	vm.Lock()
	defer vm.Unlock()

	if vm.refs > 0 {
		vm.refs--
	}
	if vm.refs > 0 {
		vm.logger().WithField("refs", vm.refs).Info("KubeVirt VM still in use, not stopping")
		return nil
	}

	start := time.Now()
	if err := vm.setRunning(ctx, false); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, kubevirtStopTimeout)
	defer cancel()
	for {
		out, err := vm.kubectl(ctx, "get", "vmi", vm.name, "--ignore-not-found", "-o", "name")
		if err == nil && out == "" {
			break
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("VMI %s/%s still present after stop: %w", vm.namespace, vm.name, ctx.Err())
		case <-time.After(kubevirtPollInterval):
		}
	}

	vm.logger().WithField("took", time.Since(start)).Info("KubeVirt VM stopped")
	return nil
}

func (vm *kubevirtVM) setRunning(ctx context.Context, running bool) error {
	patch := fmt.Sprintf(`{"spec":{"running":%t}}`, running)
	if _, err := vm.kubectl(ctx, "patch", "vm", vm.name, "--type=merge", "-p", patch); err != nil {
		vm.logger().WithError(err).Error("failed to patch VirtualMachine")
		return err
	}
	return nil
}

// waitReachable polls until the VMI is Running, not being deleted, has an IP
// and its guest port answers an HTTP request.
func (vm *kubevirtVM) waitReachable(ctx context.Context) (string, error) {
	client := &http.Client{Timeout: 2 * time.Second}
	jsonPath := `{.metadata.deletionTimestamp}|{.status.phase}|{.status.interfaces[0].ipAddress}`

	for {
		out, err := vm.kubectl(ctx, "get", "vmi", vm.name, "--ignore-not-found", "-o", "jsonpath="+jsonPath)
		if err == nil {
			fields := strings.Split(out, "|")
			if len(fields) == 3 && fields[0] == "" && fields[1] == "Running" && fields[2] != "" {
				ip := fields[2]
				if resp, err := client.Get("http://" + ip + ":" + vm.port + "/"); err == nil {
					resp.Body.Close()
					return ip, nil
				}
			}
		}

		select {
		case <-ctx.Done():
			return "", fmt.Errorf("KubeVirt VM %s/%s not reachable: %w", vm.namespace, vm.name, ctx.Err())
		case <-time.After(kubevirtPollInterval):
		}
	}
}

func (vm *kubevirtVM) kubectl(ctx context.Context, args ...string) (string, error) {
	args = append([]string{"--kubeconfig", kubevirtKubeconfig, "-n", vm.namespace}, args...)
	cmd := exec.CommandContext(ctx, "kubectl", args...)

	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("kubectl %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(string(out)), nil
}
