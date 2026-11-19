| service | n (cycles 2+) | n timed | mean s | median s | min s | max s | stdev s | non-200/failed |
|---|---|---|---|---|---|---|---|---|
| nginx-stock | 10 | 10 | 4.115 | 4.123 | 3.988 | 4.188 | 0.052 | 0 |
| nginx-mine | 10 | 10 | 1.765 | 1.766 | 1.670 | 1.845 | 0.064 | 0 |

ratio nginx-stock/nginx-mine: mean 2.33x, median 2.33x

all cycles (warm-up flagged):

| service | cycle | warmup | HTTP | curl_exit | time_total s | idle_wait s | bytes | sha256/16 | vm boot (vhive log) | note |
|---|---|---|---|---|---|---|---|---|---|---|
| nginx-stock | 1 | 1 | 200 | 0 | 4.353600 | 102 | 615 | fb47468a2cd3953c |  |  |
| nginx-mine | 1 | 1 | 200 | 0 | 1.522164 | 112 | 615 | fb47468a2cd3953c | firecrackerVM=fcweb1 launchToHTTP200=173.433907ms probe=tcp |  |
| nginx-stock | 2 | 0 | 200 | 0 | 4.122796 | 102 | 615 | fb47468a2cd3953c |  |  |
| nginx-mine | 2 | 0 | 200 | 0 | 1.845215 | 106 | 615 | fb47468a2cd3953c | firecrackerVM=fcweb1 launchToHTTP200=176.494089ms probe=tcp |  |
| nginx-stock | 3 | 0 | 200 | 0 | 4.143299 | 101 | 615 | fb47468a2cd3953c |  |  |
| nginx-mine | 3 | 0 | 200 | 0 | 1.810655 | 106 | 615 | fb47468a2cd3953c | firecrackerVM=fcweb1 launchToHTTP200=176.466092ms probe=tcp |  |
| nginx-stock | 4 | 0 | 200 | 0 | 4.188027 | 101 | 615 | fb47468a2cd3953c |  |  |
| nginx-mine | 4 | 0 | 200 | 0 | 1.827230 | 105 | 615 | fb47468a2cd3953c | firecrackerVM=fcweb1 launchToHTTP200=182.500016ms probe=tcp |  |
| nginx-stock | 5 | 0 | 200 | 0 | 4.099950 | 102 | 615 | fb47468a2cd3953c |  |  |
| nginx-mine | 5 | 0 | 200 | 0 | 1.806301 | 106 | 615 | fb47468a2cd3953c | firecrackerVM=fcweb1 launchToHTTP200=184.584444ms probe=tcp |  |
| nginx-stock | 6 | 0 | 200 | 0 | 3.988223 | 104 | 615 | fb47468a2cd3953c |  |  |
| nginx-mine | 6 | 0 | 200 | 0 | 1.824311 | 106 | 615 | fb47468a2cd3953c | firecrackerVM=fcweb1 launchToHTTP200=181.423986ms probe=tcp |  |
| nginx-stock | 7 | 0 | 200 | 0 | 4.102282 | 102 | 615 | fb47468a2cd3953c |  |  |
| nginx-mine | 7 | 0 | 200 | 0 | 1.725646 | 108 | 615 | fb47468a2cd3953c | firecrackerVM=fcweb1 launchToHTTP200=175.38103ms probe=tcp |  |
| nginx-stock | 8 | 0 | 200 | 0 | 4.133489 | 101 | 615 | fb47468a2cd3953c |  |  |
| nginx-mine | 8 | 0 | 200 | 0 | 1.724605 | 108 | 615 | fb47468a2cd3953c | firecrackerVM=fcweb1 launchToHTTP200=174.379943ms probe=tcp |  |
| nginx-stock | 9 | 0 | 200 | 0 | 4.122281 | 101 | 615 | fb47468a2cd3953c |  |  |
| nginx-mine | 9 | 0 | 200 | 0 | 1.690402 | 108 | 615 | fb47468a2cd3953c | firecrackerVM=fcweb1 launchToHTTP200=176.051652ms probe=tcp |  |
| nginx-stock | 10 | 0 | 200 | 0 | 4.142680 | 102 | 615 | fb47468a2cd3953c |  |  |
| nginx-mine | 10 | 0 | 200 | 0 | 1.669874 | 108 | 615 | fb47468a2cd3953c | firecrackerVM=fcweb1 launchToHTTP200=176.968018ms probe=tcp |  |
| nginx-stock | 11 | 0 | 200 | 0 | 4.107026 | 102 | 615 | fb47468a2cd3953c |  |  |
| nginx-mine | 11 | 0 | 200 | 0 | 1.722917 | 162 | 615 | fb47468a2cd3953c | firecrackerVM=fcweb1 launchToHTTP200=174.628541ms probe=tcp |  |
