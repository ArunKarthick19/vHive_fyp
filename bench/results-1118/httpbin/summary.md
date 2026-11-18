| service | n (cycles 2+) | n timed | mean s | median s | min s | max s | stdev s | non-200/failed |
|---|---|---|---|---|---|---|---|---|
| httpbin-stock | 10 | 10 | 4.342 | 4.379 | 4.167 | 4.488 | 0.109 | 0 |
| httpbin-mine | 10 | 10 | 2.282 | 2.230 | 2.206 | 2.793 | 0.180 | 0 |

ratio httpbin-stock/httpbin-mine: mean 1.90x, median 1.96x

all cycles (warm-up flagged):

| service | cycle | warmup | HTTP | curl_exit | time_total s | idle_wait s | bytes | sha256/16 | vm boot (vhive log) | note |
|---|---|---|---|---|---|---|---|---|---|---|
| httpbin-stock | 1 | 1 | 200 | 0 | 4.117568 | 101 | 9593 | 7c7b091e007ce3f2 |  |  |
| httpbin-mine | 1 | 1 | 200 | 0 | 2.231760 | 105 | 9593 | 7c7b091e007ce3f2 | firecrackerVM=fcweb1 launchToHTTP200=861.646123ms probe=tcp |  |
| httpbin-stock | 2 | 0 | 200 | 0 | 4.352213 | 104 | 9593 | 7c7b091e007ce3f2 |  |  |
| httpbin-mine | 2 | 0 | 200 | 0 | 2.206226 | 108 | 9593 | 7c7b091e007ce3f2 | firecrackerVM=fcweb1 launchToHTTP200=855.521348ms probe=tcp |  |
| httpbin-stock | 3 | 0 | 200 | 0 | 4.410750 | 103 | 9593 | 7c7b091e007ce3f2 |  |  |
| httpbin-mine | 3 | 0 | 200 | 0 | 2.231543 | 108 | 9593 | 7c7b091e007ce3f2 | firecrackerVM=fcweb1 launchToHTTP200=868.144471ms probe=tcp |  |
| httpbin-stock | 4 | 0 | 200 | 0 | 4.317439 | 103 | 9593 | 7c7b091e007ce3f2 |  |  |
| httpbin-mine | 4 | 0 | 200 | 0 | 2.229873 | 108 | 9593 | 7c7b091e007ce3f2 | firecrackerVM=fcweb1 launchToHTTP200=861.74677ms probe=tcp |  |
| httpbin-stock | 5 | 0 | 200 | 0 | 4.405335 | 101 | 9593 | 7c7b091e007ce3f2 |  |  |
| httpbin-mine | 5 | 0 | 200 | 0 | 2.216717 | 146 | 9593 | 7c7b091e007ce3f2 | firecrackerVM=fcweb1 launchToHTTP200=849.161413ms probe=tcp |  |
| httpbin-stock | 6 | 0 | 200 | 0 | 4.225481 | 103 | 9593 | 7c7b091e007ce3f2 |  |  |
| httpbin-mine | 6 | 0 | 200 | 0 | 2.213979 | 107 | 9593 | 7c7b091e007ce3f2 | firecrackerVM=fcweb1 launchToHTTP200=848.069762ms probe=tcp |  |
| httpbin-stock | 7 | 0 | 200 | 0 | 4.424523 | 101 | 9593 | 7c7b091e007ce3f2 |  |  |
| httpbin-mine | 7 | 0 | 200 | 0 | 2.222776 | 150 | 9593 | 7c7b091e007ce3f2 | firecrackerVM=fcweb1 launchToHTTP200=849.959152ms probe=tcp |  |
| httpbin-stock | 8 | 0 | 200 | 0 | 4.207890 | 104 | 9593 | 7c7b091e007ce3f2 |  |  |
| httpbin-mine | 8 | 0 | 200 | 0 | 2.235932 | 108 | 9593 | 7c7b091e007ce3f2 | firecrackerVM=fcweb1 launchToHTTP200=853.325498ms probe=tcp |  |
| httpbin-stock | 9 | 0 | 200 | 0 | 4.166733 | 101 | 9593 | 7c7b091e007ce3f2 |  |  |
| httpbin-mine | 9 | 0 | 200 | 0 | 2.231077 | 107 | 9593 | 7c7b091e007ce3f2 | firecrackerVM=fcweb1 launchToHTTP200=844.403739ms probe=tcp |  |
| httpbin-stock | 10 | 0 | 200 | 0 | 4.487864 | 101 | 9593 | 7c7b091e007ce3f2 |  |  |
| httpbin-mine | 10 | 0 | 200 | 0 | 2.793127 | 114 | 9593 | 7c7b091e007ce3f2 | firecrackerVM=fcweb1 launchToHTTP200=864.518496ms probe=tcp |  |
| httpbin-stock | 11 | 0 | 200 | 0 | 4.421227 | 104 | 9593 | 7c7b091e007ce3f2 |  |  |
| httpbin-mine | 11 | 0 | 200 | 0 | 2.239609 | 108 | 9593 | 7c7b091e007ce3f2 | firecrackerVM=fcweb1 launchToHTTP200=856.568287ms probe=tcp |  |
