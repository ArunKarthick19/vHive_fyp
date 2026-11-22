| service | n (cycles 2+) | n timed | mean s | median s | min s | max s | stdev s | non-OK/failed |
|---|---|---|---|---|---|---|---|---|
| aesgo-stock | 7 | 7 | 4.076 | 4.079 | 3.984 | 4.181 | 0.060 | 0 |
| aesgo-mine | 7 | 7 | 1.742 | 1.752 | 1.409 | 1.964 | 0.167 | 0 |

ratio aesgo-stock/aesgo-mine: mean 2.34x, median 2.33x

all cycles (warm-up flagged):

| service | cycle | warmup | HTTP | curl_exit | time_total s | idle_wait s | bytes | sha256/16 | vm boot (vhive log) | note |
|---|---|---|---|---|---|---|---|---|---|---|
| aesgo-stock | 1 | 1 | OK | 0 | 4.459091 | 9 | 122 | 7bfd0cc3ed50de86 |  |  |
| aesgo-mine | 1 | 1 | OK | 0 | 1.701407 | 108 | 122 | 7bfd0cc3ed50de86 | firecrackerVM=fcweb1 launchToHTTP200=138.46514ms probe=tcp |  |
| aesgo-stock | 2 | 0 | OK | 0 | 4.080565 | 102 | 122 | 7bfd0cc3ed50de86 |  |  |
| aesgo-mine | 2 | 0 | OK | 0 | 1.730070 | 106 | 122 | 7bfd0cc3ed50de86 | firecrackerVM=fcweb1 launchToHTTP200=134.66573ms probe=tcp |  |
| aesgo-stock | 3 | 0 | OK | 0 | 3.983971 | 101 | 122 | 7bfd0cc3ed50de86 |  |  |
| aesgo-mine | 3 | 0 | OK | 0 | 1.769150 | 105 | 122 | 7bfd0cc3ed50de86 | firecrackerVM=fcweb1 launchToHTTP200=138.871541ms probe=tcp |  |
| aesgo-stock | 4 | 0 | OK | 0 | 4.035398 | 101 | 122 | 7bfd0cc3ed50de86 |  |  |
| aesgo-mine | 4 | 0 | OK | 0 | 1.751032 | 106 | 122 | 7bfd0cc3ed50de86 | firecrackerVM=fcweb1 launchToHTTP200=142.441322ms probe=tcp |  |
| aesgo-stock | 5 | 0 | OK | 0 | 4.079220 | 102 | 122 | 7bfd0cc3ed50de86 |  |  |
| aesgo-mine | 5 | 0 | OK | 0 | 1.751683 | 106 | 122 | 7bfd0cc3ed50de86 | firecrackerVM=fcweb1 launchToHTTP200=138.279129ms probe=tcp |  |
| aesgo-stock | 6 | 0 | OK | 0 | 4.180571 | 99 | 122 | 7bfd0cc3ed50de86 |  |  |
| aesgo-mine | 6 | 0 | OK | 0 | 1.963605 | 138 | 122 | 7bfd0cc3ed50de86 | firecrackerVM=fcweb1 launchToHTTP200=131.915235ms probe=tcp |  |
| aesgo-stock | 7 | 0 | OK | 0 | 4.103757 | 101 | 122 | 7bfd0cc3ed50de86 |  |  |
| aesgo-mine | 7 | 0 | OK | 0 | 1.408733 | 129 | 122 | 7bfd0cc3ed50de86 | firecrackerVM=fcweb1 launchToHTTP200=140.784525ms probe=tcp |  |
| aesgo-stock | 8 | 0 | OK | 0 | 4.068019 | 101 | 122 | 7bfd0cc3ed50de86 |  |  |
| aesgo-mine | 8 | 0 | OK | 0 | 1.818024 | 105 | 122 | 7bfd0cc3ed50de86 | firecrackerVM=fcweb1 launchToHTTP200=136.941254ms probe=tcp |  |
