# Stock cold start breakdown (phase C)

Cycles found: 6; statistics use cycles [2, 3, 4, 5, 6] (cycle 1 is a warm-up).

| step | n | mean s | min s | max s | what it covers |
|---|---|---|---|---|---|
| k8s: request -> sandbox ready | 5 | 0.651 | 0.636 | 0.665 | activator, autoscaler, scheduler, kubelet, pause sandbox |
| pre-VM: sandbox -> create VM request | 5 | 0.005 | 0.004 | 0.005 | kubelet CreateContainer -> vhive; vhive network setup + image lookup (cached, not logged separately) |
| VM1 VMM launch | 5 | 0.052 | 0.052 | 0.053 | shim start, firecracker process, machine/boot/drive/NIC config |
| VM2 guest boot + agent | 5 | 2.182 | 2.101 | 2.202 | kernel + systemd + firecracker-containerd agent |
| VM3 container create (vhive) | 5 | 0.008 | 0.006 | 0.009 | vhive NewContainer: container metadata, snapshot request |
| VM4 rootfs snapshot (unpack equivalent) | 5 | 0.079 | 0.074 | 0.089 | devmapper thin snapshot of the cached image + hot-plug into the VM |
| VM5 task create | 5 | 0.305 | 0.274 | 0.322 | agent mounts rootfs and creates the container task in the guest |
| VM6 task start -> app up | 5 | 0.034 | 0.030 | 0.037 | task.Wait/Start, busybox httpd prints "httpd started" |
| VM stage total (create VM -> app up) | 5 | 2.660 | 2.589 | 2.701 | the "~2.66 s" stage |
| post-VM: app up -> response | 5 | 0.889 | 0.209 | 1.183 | placeholder UC + queue-proxy start, readiness, first proxied request |
| end-to-end | 5 | 4.206 | 3.568 | 4.443 | client view |

## Log lines used

- `sandbox_req`: containerd: RunPodSandbox for the pod
- `sandbox_ready`: containerd: RunPodSandbox returns sandbox id
- `fc_create_req`: firecracker-containerd: create VM request (MachineCfg)
- `vmm_started`: firecracker-containerd: startInstance successful (VMM running, guest kernel booting)
- `agent_ready`: firecracker-containerd: successfully started the VM (in-guest agent answered)
- `snap_prepare`: firecracker-containerd: prepare snapshot for the container rootfs
- `snap_device`: firecracker-containerd: creating snapshot device (devmapper thin snapshot)
- `drive_patched`: firecracker-containerd: PatchGuestDrive successful (rootfs hot-plugged into VM)
- `task_created`: firecracker-containerd: successfully created task
- `app_started`: vhive log: guest stdout "httpd started"
- `uc_created`: containerd: CreateContainer user-container returns
- `uc_started`: containerd: StartContainer (user-container) returns successfully
- `qp_started`: containerd: StartContainer (queue-proxy) returns successfully

## Not measurable from the logs

- **Image pull**: vHive (devmapper snapshotter) calls getImage *before* CreateVM and logs it only at debug level; the daemon runs at info level, so there is no timestamp. The image is already cached on the node, so no network pull happens; whatever lookup time exists is inside "pre-VM".
- **vHive's own StartVM sub-step timings** (`StartVM: Creating a new container`, `...task`, `...Starting the task`) are debug-level and absent; the firecracker-containerd lines above stand in for them.
- Pane timestamps are the daemons' own; pod creation time from the API has only 1 s resolution and is not used.
