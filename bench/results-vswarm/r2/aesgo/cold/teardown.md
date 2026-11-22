Columns: stock = firecracker-containerd pane lines 'stop VM: VMID:"<id>"' -> next 'firecracker exited: ...' (exact log lines); pidwatch exit = my 20 ms poller.
mine: col 3 = vhive line time minus took (vhive's own timer, started before SIGTERM; no separate log line); col 4 = vhive 'per-pod Firecracker VM stopped' line = END OF CLEANUP (process gone + tap/socket removed), NOT the process-exit instant; col 5 = took; process exit instant only from pidwatch (col 6/7).

| mode | VM | stop request (log) | process exit (log) | request→exit s | pidwatch exit | request→pidwatch exit s | other |
|---|---|---|---|---|---|---|---|
| stock | 43-6d7ea0ec-6df1-41 | 20:41:09.225 | 20:41:14.276 | 5.051 | 20:41:14.289 | 5.063 | exit line: firecracker exited: signal: killed |
| stock | 44-6f0c6a77-568f-49 | 20:44:44.926 | 20:44:49.992 | 5.066 | 20:44:50.022 | 5.096 | exit line: firecracker exited: signal: killed |
| stock | 45-668dd530-4f69-4e | 20:48:21.662 | 20:48:26.725 | 5.063 | 20:48:26.750 | 5.087 | exit line: firecracker exited: signal: killed |
| stock | 46-ef30bc4d-0eb7-45 | 20:51:57.376 | 20:52:02.438 | 5.062 | 20:52:02.477 | 5.101 | exit line: firecracker exited: signal: killed |
| stock | 47-8f744388-b7fc-45 | 20:55:33.092 | 20:55:38.144 | 5.052 | 20:55:38.187 | 5.094 | exit line: firecracker exited: signal: killed |
| stock | 48-933badec-77c0-45 | 20:59:37.828 | 20:59:42.881 | 5.053 | 20:59:42.888 | 5.060 | exit line: firecracker exited: signal: killed |
| stock | 49-5361be55-a642-42 | 21:03:38.240 | 21:03:43.297 | 5.057 | 21:03:43.326 | 5.086 | exit line: firecracker exited: signal: killed |
| stock | 50-85fcf119-e606-42 | 21:07:13.646 | 21:07:18.702 | 5.056 | 21:07:18.746 | 5.100 | exit line: firecracker exited: signal: killed |
| mine | slot 1 firecracker-vm/pod-1-89004623ffc7 | 20:42:58.507 (= line − took) | 20:42:58.589 (cleanup done, took=0.082) | 0.082 | 20:42:58.571 | 0.064 | |
| mine | slot 1 firecracker-vm/pod-1-5e77d69c25e6 | 20:46:34.505 (= line − took) | 20:46:34.606 (cleanup done, took=0.101) | 0.101 | 20:46:34.556 | 0.051 | |
| mine | slot 1 firecracker-vm/pod-1-68834ec467c0 | 20:50:10.502 (= line − took) | 20:50:10.578 (cleanup done, took=0.076) | 0.076 | 20:50:10.563 | 0.060 | |
| mine | slot 1 firecracker-vm/pod-1-b61bf393d6e6 | 20:53:46.502 (= line − took) | 20:53:46.573 (cleanup done, took=0.071) | 0.071 | 20:53:46.566 | 0.063 | |
| mine | slot 1 firecracker-vm/pod-1-397ea566c886 | 20:57:20.508 (= line − took) | 20:57:20.582 (cleanup done, took=0.074) | 0.074 | 20:57:20.576 | 0.068 | |
| mine | slot 1 firecracker-vm/pod-1-7e52c3574448 | 21:01:26.530 (= line − took) | 21:01:26.628 (cleanup done, took=0.098) | 0.098 | 21:01:26.573 | 0.043 | |
| mine | slot 1 firecracker-vm/pod-1-59c1b9ab1898 | 21:05:26.507 (= line − took) | 21:05:26.575 (cleanup done, took=0.068) | 0.068 | 21:05:26.557 | 0.050 | |
| mine | slot 1 firecracker-vm/pod-1-b7f27d9b8e8d | 21:09:02.508 (= line − took) | 21:09:02.588 (cleanup done, took=0.080) | 0.080 | 21:09:02.543 | 0.034 | |
