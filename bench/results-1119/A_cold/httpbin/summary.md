| service | n (cycles 2+) | n timed | mean s | median s | min s | max s | stdev s | non-200/failed |
|---|---|---|---|---|---|---|---|---|
| httpbin-stock | 10 | 10 | 4.153 | 4.161 | 4.060 | 4.243 | 0.061 | 0 |
| httpbin-mine | 10 | 10 | 2.494 | 2.520 | 2.190 | 2.812 | 0.255 | 0 |

ratio httpbin-stock/httpbin-mine: mean 1.66x, median 1.65x

all cycles (warm-up flagged):

| service | cycle | warmup | HTTP | curl_exit | time_total s | idle_wait s | bytes | sha256/16 | vm boot (vhive log) | note |
|---|---|---|---|---|---|---|---|---|---|---|
| httpbin-stock | 1 | 1 | 200 | 0 | 5.298116 | 100 | 9593 | 7c7b091e007ce3f2 |  |  |
| httpbin-mine | 1 | 1 | 200 | 0 | 2.624489 | 108 | 9593 | 7c7b091e007ce3f2 | firecrackerVM=fcweb1 launchToHTTP200=866.369353ms probe=tcp |  |
| httpbin-stock | 2 | 0 | 200 | 0 | 4.243164 | 101 | 9593 | 7c7b091e007ce3f2 |  |  |
| httpbin-mine | 2 | 0 | 200 | 0 | 2.225416 | 108 | 9593 | 7c7b091e007ce3f2 | firecrackerVM=fcweb1 launchToHTTP200=858.70772ms probe=tcp |  |
| httpbin-stock | 3 | 0 | 200 | 0 | 4.119268 | 104 | 9593 | 7c7b091e007ce3f2 |  |  |
| httpbin-mine | 3 | 0 | 200 | 0 | 2.573419 | 106 | 9593 | 7c7b091e007ce3f2 | firecrackerVM=fcweb1 launchToHTTP200=882.138446ms probe=tcp |  |
| httpbin-stock | 4 | 0 | 200 | 0 | 4.164641 | 103 | 9593 | 7c7b091e007ce3f2 |  |  |
| httpbin-mine | 4 | 0 | 200 | 0 | 2.245066 | 105 | 9593 | 7c7b091e007ce3f2 | firecrackerVM=fcweb1 launchToHTTP200=839.241081ms probe=tcp |  |
| httpbin-stock | 5 | 0 | 200 | 0 | 4.102678 | 103 | 9593 | 7c7b091e007ce3f2 |  |  |
| httpbin-mine | 5 | 0 | 200 | 0 | 2.466886 | 106 | 9593 | 7c7b091e007ce3f2 | firecrackerVM=fcweb1 launchToHTTP200=855.656657ms probe=tcp |  |
| httpbin-stock | 6 | 0 | 200 | 0 | 4.232380 | 102 | 9593 | 7c7b091e007ce3f2 |  |  |
| httpbin-mine | 6 | 0 | 200 | 0 | 2.773282 | 108 | 9593 | 7c7b091e007ce3f2 | firecrackerVM=fcweb1 launchToHTTP200=851.578492ms probe=tcp |  |
| httpbin-stock | 7 | 0 | 200 | 0 | 4.195559 | 104 | 9593 | 7c7b091e007ce3f2 |  |  |
| httpbin-mine | 7 | 0 | 200 | 0 | 2.653805 | 108 | 9593 | 7c7b091e007ce3f2 | firecrackerVM=fcweb1 launchToHTTP200=845.275886ms probe=tcp |  |
| httpbin-stock | 8 | 0 | 200 | 0 | 4.166115 | 101 | 9593 | 7c7b091e007ce3f2 |  |  |
| httpbin-mine | 8 | 0 | 200 | 0 | 2.232742 | 106 | 9593 | 7c7b091e007ce3f2 | firecrackerVM=fcweb1 launchToHTTP200=854.931695ms probe=tcp |  |
| httpbin-stock | 9 | 0 | 200 | 0 | 4.085292 | 101 | 9593 | 7c7b091e007ce3f2 |  |  |
| httpbin-mine | 9 | 0 | 200 | 0 | 2.772086 | 106 | 9593 | 7c7b091e007ce3f2 | firecrackerVM=fcweb1 launchToHTTP200=846.401001ms probe=tcp |  |
| httpbin-stock | 10 | 0 | 200 | 0 | 4.158303 | 101 | 9593 | 7c7b091e007ce3f2 |  |  |
| httpbin-mine | 10 | 0 | 200 | 0 | 2.812132 | 108 | 9593 | 7c7b091e007ce3f2 | firecrackerVM=fcweb1 launchToHTTP200=863.264912ms probe=tcp |  |
| httpbin-stock | 11 | 0 | 200 | 0 | 4.059869 | 104 | 9593 | 7c7b091e007ce3f2 |  |  |
| httpbin-mine | 11 | 0 | 200 | 0 | 2.190085 | 108 | 9593 | 7c7b091e007ce3f2 | firecrackerVM=fcweb1 launchToHTTP200=856.489037ms probe=tcp |  |
