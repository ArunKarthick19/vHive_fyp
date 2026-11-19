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
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/vhive-serverless/vhive/cri/firecracker/guest"
	criapi "k8s.io/cri-api/pkg/apis/runtime/v1"
)

// Crash reporting for firecracker-vm://image (docs/DRAWBACKS_PLAN.md §1).
//
// kubelet learns a container's state from containerd, which only knows the
// host placeholder of the user container. When the app in the pod's image VM
// exits, vhive (1) records the exit for the sandbox's current user container,
// (2) stops the placeholder, so containerd really reports it EXITED, and
// (3) patches ContainerStatus of that container ID with the app's exit code,
// reason and console tail. kubelet then restarts it as for any container.

const (
	// appExitUnknownCode is reported when the VM stopped without fcinit
	// recording the app's exit (kernel panic, out of memory, killed).
	appExitUnknownCode = 255
	// appExitMessageMax bounds the console tail put into the status message.
	appExitMessageMax = 1024
	// placeholderStopTimeout bounds the StopContainer call to containerd.
	placeholderStopTimeout = 30 * time.Second
)

// appExit is what ContainerStatus reports for a user container whose VM's
// app has exited.
type appExit struct {
	ExitCode int32
	Reason   string
	Message  string
}

// appExitFromVM turns a VM's exit report into a container status, using
// the reasons Kubernetes shows for ordinary containers where they fit.
func appExitFromVM(e vmExit) appExit {
	tail := strings.ReplaceAll(e.Console, "\r", "") // the serial console ends lines with CRLF
	if len(tail) > appExitMessageMax {
		tail = tail[len(tail)-appExitMessageMax:]
	}
	if e.Err != nil {
		return appExit{
			ExitCode: appExitUnknownCode,
			Reason:   "VMExited",
			Message:  fmt.Sprintf("the Firecracker VM stopped without an exit record from fcinit (%v); console tail:\n%s", e.Err, tail),
		}
	}
	a := appExit{ExitCode: int32(e.Status.ExitCode)}
	switch {
	case e.Status.Reason == guest.ReasonInitError:
		a.Reason = "StartError"
	case e.Status.ExitCode == 0:
		a.Reason = "Completed"
	default:
		a.Reason = "Error"
	}
	what := fmt.Sprintf("exit code %d", e.Status.ExitCode)
	if e.Status.Signal != "" {
		what += " (" + e.Status.Signal + ")"
	}
	if e.Status.Message != "" {
		what += ": " + e.Status.Message
	}
	a.Message = fmt.Sprintf("app in the Firecracker VM ended (%s, %s); console tail:\n%s", e.Status.Reason, what, tail)
	return a
}

// trackImageContainer remembers the sandbox's current user container (a
// restart in the same sandbox replaces it).
func (fs *FirecrackerService) trackImageContainer(sandboxID, containerID string) {
	fs.exitMu.Lock()
	defer fs.exitMu.Unlock()
	if fs.imageUC == nil {
		fs.imageUC = make(map[string]string)
	}
	fs.imageUC[sandboxID] = containerID
}

func (fs *FirecrackerService) untrackSandbox(sandboxID string) {
	fs.exitMu.Lock()
	defer fs.exitMu.Unlock()
	delete(fs.imageUC, sandboxID)
}

// forgetContainer drops what is kept for a removed container.
func (fs *FirecrackerService) forgetContainer(containerID string) {
	fs.exitMu.Lock()
	defer fs.exitMu.Unlock()
	delete(fs.appExited, containerID)
	for sb, id := range fs.imageUC {
		if id == containerID {
			delete(fs.imageUC, sb)
		}
	}
}

// imageAppExited is podVMs.onAppExit: record the exit, then stop the
// placeholder so containerd reports the container as exited.
func (fs *FirecrackerService) imageAppExited(sandboxID string, e vmExit) {
	a := appExitFromVM(e)

	fs.exitMu.Lock()
	containerID := fs.imageUC[sandboxID]
	if containerID != "" {
		if fs.appExited == nil {
			fs.appExited = make(map[string]appExit)
		}
		fs.appExited[containerID] = a
	}
	fs.exitMu.Unlock()

	l := log.WithFields(log.Fields{"sandbox": sandboxID, "containerID": containerID, "exitCode": a.ExitCode, "reason": a.Reason})
	if containerID == "" {
		l.Warn("image VM app exited but its sandbox has no tracked user container; nothing to report")
		return
	}
	// Copy the app's last lines into this attempt's log before kubelet
	// restarts the container (kubectl logs --previous).
	fs.stopVMLog(sandboxID)
	ctx, cancel := context.WithTimeout(context.Background(), placeholderStopTimeout)
	defer cancel()
	if _, err := fs.stockRuntimeClient.StopContainer(ctx, &criapi.StopContainerRequest{ContainerId: containerID, Timeout: 0}); err != nil {
		l.WithError(err).Error("failed to stop the placeholder of an exited image VM; kubelet will not see the exit")
		return
	}
	l.Info("stopped the placeholder of an exited image VM; ContainerStatus reports the app's exit")
}

// PatchContainerStatus implements cri.ContainerStatusPatcher. It changes
// only an EXITED status of a user container whose image VM's app exited;
// every other status (stock, baked-in VM modes, running) is left untouched.
func (fs *FirecrackerService) PatchContainerStatus(resp *criapi.ContainerStatusResponse) {
	st := resp.GetStatus()
	if st == nil || st.GetState() != criapi.ContainerState_CONTAINER_EXITED {
		return
	}
	fs.exitMu.Lock()
	a, ok := fs.appExited[st.GetId()]
	fs.exitMu.Unlock()
	if !ok {
		return
	}
	st.ExitCode = a.ExitCode
	st.Reason = a.Reason
	st.Message = a.Message
}
