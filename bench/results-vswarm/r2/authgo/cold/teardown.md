Columns: stock = firecracker-containerd pane lines 'stop VM: VMID:"<id>"' -> next 'firecracker exited: ...' (exact log lines); pidwatch exit = my 20 ms poller.
mine: col 3 = vhive line time minus took (vhive's own timer, started before SIGTERM; no separate log line); col 4 = vhive 'per-pod Firecracker VM stopped' line = END OF CLEANUP (process gone + tap/socket removed), NOT the process-exit instant; col 5 = took; process exit instant only from pidwatch (col 6/7).

| mode | VM | stop request (log) | process exit (log) | request→exit s | pidwatch exit | request→pidwatch exit s | other |
|---|---|---|---|---|---|---|---|
| stock | 58-b5584525-154c-48 | 21:51:45.488 | 21:51:50.561 | 5.073 | 21:51:50.595 | 5.106 | exit line: firecracker exited: signal: killed |
| stock | 59-ef9beac3-25ff-4d | 21:55:21.212 | 21:55:26.272 | 5.060 | 21:55:26.306 | 5.093 | exit line: firecracker exited: signal: killed |
| stock | 60-68b509d8-87c9-47 | 21:58:56.950 | 21:59:02.014 | 5.064 | 21:59:02.031 | 5.080 | exit line: firecracker exited: signal: killed |
| stock | 61-6f3f224c-a072-40 | 22:02:33.687 | 22:02:38.744 | 5.057 | 22:02:38.778 | 5.091 | exit line: firecracker exited: signal: killed |
| stock | 62-7c432926-fede-49 | 22:06:11.414 | 22:06:16.476 | 5.062 | 22:06:16.522 | 5.108 | exit line: firecracker exited: signal: killed |
| stock | 63-bb4f6c6e-8007-42 | 22:09:49.177 | 22:09:54.234 | 5.057 | 22:09:54.249 | 5.071 | exit line: firecracker exited: signal: killed |
| stock | 64-c770d0b9-d03d-4c | 22:13:44.446 | 22:13:49.509 | 5.063 | 22:13:49.553 | 5.106 | exit line: firecracker exited: signal: killed |
| stock | 65-7d61f5ba-1393-40 | 22:17:21.714 | 22:17:26.776 | 5.062 | 22:17:26.810 | 5.096 | exit line: firecracker exited: signal: killed |
| NOT AUTH-GO: aes-go vary pod VM still up when the watchers started (pid 690626 already running at 21:48:38), excluded | slot 1 firecracker-vm/pod-1-3315ba6a6b8c | 21:49:58.533 (= line − took) | 21:49:58.606 (cleanup done, took=0.073) | 0.073 | 21:49:58.577 | 0.044 | |
| mine | slot 1 firecracker-vm/pod-1-fe1e4656db62 | 21:53:34.604 (= line − took) | 21:53:34.703 (cleanup done, took=0.099) | 0.099 | 21:53:34.686 | 0.082 | |
| mine | slot 1 firecracker-vm/pod-1-ab6ac57d6537 | 21:57:10.611 (= line − took) | 21:57:10.696 (cleanup done, took=0.085) | 0.085 | 21:57:10.673 | 0.062 | |
| mine | slot 1 firecracker-vm/pod-1-a95631e74ccf | 22:00:44.624 (= line − took) | 22:00:44.717 (cleanup done, took=0.094) | 0.094 | 22:00:44.697 | 0.073 | |
| mine | slot 1 firecracker-vm/pod-1-f680f10b2fd2 | 22:04:22.603 (= line − took) | 22:04:22.690 (cleanup done, took=0.087) | 0.087 | 22:04:22.665 | 0.062 | |
| mine | slot 1 firecracker-vm/pod-1-fa154adf24d3 | 22:08:00.615 (= line − took) | 22:08:00.703 (cleanup done, took=0.088) | 0.088 | 22:08:00.680 | 0.064 | |
| mine | slot 1 firecracker-vm/pod-1-e70a0362c64f | 22:11:38.602 (= line − took) | 22:11:38.693 (cleanup done, took=0.091) | 0.091 | 22:11:38.674 | 0.072 | |
| mine | slot 1 firecracker-vm/pod-1-87ce6ad0f6cd | 22:15:32.592 (= line − took) | 22:15:32.666 (cleanup done, took=0.074) | 0.074 | 22:15:32.648 | 0.055 | |
| mine | slot 1 firecracker-vm/pod-1-9555b54a53eb | 22:19:10.608 (= line − took) | 22:19:10.700 (cleanup done, took=0.092) | 0.092 | 22:19:10.659 | 0.050 | |
