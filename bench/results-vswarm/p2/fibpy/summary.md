| service | n (cycles 2+) | n timed | mean s | median s | min s | max s | stdev s | non-OK/failed |
|---|---|---|---|---|---|---|---|---|
| fibpy-stock | 3 | 3 | 4.807 | 4.826 | 4.761 | 4.835 | 0.040 | 0 |
| fibpy-mine | 3 | 3 | 2.733 | 2.698 | 2.696 | 2.804 | 0.062 | 0 |

ratio fibpy-stock/fibpy-mine: mean 1.76x, median 1.79x

all cycles (warm-up flagged):

| service | cycle | warmup | HTTP | curl_exit | time_total s | idle_wait s | bytes | sha256/16 | vm boot (vhive log) | note |
|---|---|---|---|---|---|---|---|---|---|---|
| fibpy-stock | 1 | 1 | OK | 0 | 4.716883 | 99 | 63 | 6b353fb5e0c790b9 |  |  |
| fibpy-mine | 1 | 1 | OK | 0 | 3.477785 | 378 | 63 | 6b353fb5e0c790b9 | firecrackerVM=fcweb1 launchToHTTP200=1.77020001s probe=tcp |  |
| fibpy-stock | 2 | 0 | OK | 0 | 4.825793 | 371 | 63 | 6b353fb5e0c790b9 |  |  |
| fibpy-mine | 2 | 0 | OK | 0 | 2.695856 | 393 | 63 | 6b353fb5e0c790b9 | firecrackerVM=fcweb1 launchToHTTP200=1.78611638s probe=tcp |  |
| fibpy-stock | 3 | 0 | OK | 0 | 4.834572 | 371 | 63 | 6b353fb5e0c790b9 |  |  |
| fibpy-mine | 3 | 0 | OK | 0 | 2.698318 | 377 | 63 | 6b353fb5e0c790b9 | firecrackerVM=fcweb1 launchToHTTP200=1.758472926s probe=tcp |  |
| fibpy-stock | 4 | 0 | OK | 0 | 4.761439 | 371 | 63 | 6b353fb5e0c790b9 |  |  |
| fibpy-mine | 4 | 0 | OK | 0 | 2.803757 | 376 | 63 | 6b353fb5e0c790b9 | firecrackerVM=fcweb1 launchToHTTP200=1.799054067s probe=tcp |  |
