| service | n (cycles 2+) | n timed | mean s | median s | min s | max s | stdev s | non-200/failed |
|---|---|---|---|---|---|---|---|---|
| nginx-stock | 10 | 10 | 4.260 | 4.456 | 3.444 | 4.493 | 0.426 | 0 |
| nginx-mine | 10 | 10 | 1.230 | 1.169 | 1.025 | 2.007 | 0.281 | 0 |

ratio nginx-stock/nginx-mine: mean 3.46x, median 3.81x

all cycles (warm-up flagged):

| service | cycle | warmup | HTTP | curl_exit | time_total s | idle_wait s | bytes | sha256/16 | vm boot (vhive log) | note |
|---|---|---|---|---|---|---|---|---|---|---|
| nginx-stock | 1 | 1 | 200 | 0 | 4.124211 | 102 | 615 | fb47468a2cd3953c |  |  |
| nginx-mine | 1 | 1 | 200 | 0 | 1.225110 | 105 | 615 | fb47468a2cd3953c | firecrackerVM=fcweb1 launchToHTTP200=179.881795ms probe=tcp |  |
| nginx-stock | 2 | 0 | 200 | 0 | 4.474546 | 101 | 615 | fb47468a2cd3953c |  |  |
| nginx-mine | 2 | 0 | 200 | 0 | 1.093832 | 107 | 615 | fb47468a2cd3953c | firecrackerVM=fcweb1 launchToHTTP200=174.767808ms probe=tcp |  |
| nginx-stock | 3 | 0 | 200 | 0 | 4.467333 | 102 | 615 | fb47468a2cd3953c |  |  |
| nginx-mine | 3 | 0 | 200 | 0 | 1.185200 | 106 | 615 | fb47468a2cd3953c | firecrackerVM=fcweb1 launchToHTTP200=178.157984ms probe=tcp |  |
| nginx-stock | 4 | 0 | 200 | 0 | 3.462858 | 101 | 615 | fb47468a2cd3953c |  |  |
| nginx-mine | 4 | 0 | 200 | 0 | 1.231299 | 106 | 615 | fb47468a2cd3953c | firecrackerVM=fcweb1 launchToHTTP200=168.455368ms probe=tcp |  |
| nginx-stock | 5 | 0 | 200 | 0 | 4.479740 | 101 | 615 | fb47468a2cd3953c |  |  |
| nginx-mine | 5 | 0 | 200 | 0 | 1.154068 | 105 | 615 | fb47468a2cd3953c | firecrackerVM=fcweb1 launchToHTTP200=174.014787ms probe=tcp |  |
| nginx-stock | 6 | 0 | 200 | 0 | 3.444269 | 101 | 615 | fb47468a2cd3953c |  |  |
| nginx-mine | 6 | 0 | 200 | 0 | 1.181809 | 105 | 615 | fb47468a2cd3953c | firecrackerVM=fcweb1 launchToHTTP200=178.447196ms probe=tcp |  |
| nginx-stock | 7 | 0 | 200 | 0 | 4.478345 | 101 | 615 | fb47468a2cd3953c |  |  |
| nginx-mine | 7 | 0 | 200 | 0 | 1.054953 | 108 | 615 | fb47468a2cd3953c | firecrackerVM=fcweb1 launchToHTTP200=175.650242ms probe=tcp |  |
| nginx-stock | 8 | 0 | 200 | 0 | 4.419690 | 104 | 615 | fb47468a2cd3953c |  |  |
| nginx-mine | 8 | 0 | 200 | 0 | 1.208530 | 106 | 615 | fb47468a2cd3953c | firecrackerVM=fcweb1 launchToHTTP200=177.230909ms probe=tcp |  |
| nginx-stock | 9 | 0 | 200 | 0 | 4.492811 | 101 | 615 | fb47468a2cd3953c |  |  |
| nginx-mine | 9 | 0 | 200 | 0 | 2.007256 | 110 | 615 | fb47468a2cd3953c | firecrackerVM=fcweb1 launchToHTTP200=172.733452ms probe=tcp |  |
| nginx-stock | 10 | 0 | 200 | 0 | 4.437149 | 101 | 615 | fb47468a2cd3953c |  |  |
| nginx-mine | 10 | 0 | 200 | 0 | 1.156180 | 105 | 615 | fb47468a2cd3953c | firecrackerVM=fcweb1 launchToHTTP200=181.175426ms probe=tcp |  |
| nginx-stock | 11 | 0 | 200 | 0 | 4.444372 | 101 | 615 | fb47468a2cd3953c |  |  |
| nginx-mine | 11 | 0 | 200 | 0 | 1.024598 | 107 | 615 | fb47468a2cd3953c | firecrackerVM=fcweb1 launchToHTTP200=174.812436ms probe=tcp |  |
