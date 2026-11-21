| service | n (cycles 2+) | n timed | mean s | median s | min s | max s | stdev s | non-OK/failed |
|---|---|---|---|---|---|---|---|---|
| fibgo-stock | 6 | 6 | 3.978 | 3.944 | 3.900 | 4.100 | 0.079 | 0 |
| fibgo-mine | 6 | 6 | 1.712 | 1.683 | 1.600 | 1.855 | 0.103 | 0 |

ratio fibgo-stock/fibgo-mine: mean 2.32x, median 2.34x

all cycles (warm-up flagged):

| service | cycle | warmup | HTTP | curl_exit | time_total s | idle_wait s | bytes | sha256/16 | vm boot (vhive log) | note |
|---|---|---|---|---|---|---|---|---|---|---|
| fibgo-stock | 1 | 1 | OK | 0 | 3.694386 | 9 | 63 | 8f3171cb13a2651b |  |  |
| fibgo-mine | 1 | 1 | OK | 0 | 1.580428 | 159 | 63 | 8f3171cb13a2651b | firecrackerVM=fcweb1 launchToHTTP200=135.556975ms probe=tcp |  |
| fibgo-stock | 2 | 0 | OK | 0 | 3.950013 | 101 | 63 | 8f3171cb13a2651b |  |  |
| fibgo-mine | 2 | 0 | OK | 0 | 1.707946 | 106 | 63 | 8f3171cb13a2651b | firecrackerVM=fcweb1 launchToHTTP200=136.435938ms probe=tcp |  |
| fibgo-stock | 3 | 0 | OK | 0 | 3.925919 | 101 | 63 | 8f3171cb13a2651b |  |  |
| fibgo-mine | 3 | 0 | OK | 0 | 1.855147 | 155 | 63 | 8f3171cb13a2651b | firecrackerVM=fcweb1 launchToHTTP200=134.13553ms probe=tcp |  |
| fibgo-stock | 4 | 0 | OK | 0 | 4.052057 | 99 | 63 | 8f3171cb13a2651b |  |  |
| fibgo-mine | 4 | 0 | OK | 0 | 1.633841 | 106 | 63 | 8f3171cb13a2651b | firecrackerVM=fcweb1 launchToHTTP200=132.556997ms probe=tcp |  |
| fibgo-stock | 5 | 0 | OK | 0 | 4.099861 | 99 | 63 | 8f3171cb13a2651b |  |  |
| fibgo-mine | 5 | 0 | OK | 0 | 1.600173 | 159 | 63 | 8f3171cb13a2651b | firecrackerVM=fcweb1 launchToHTTP200=138.641901ms probe=tcp |  |
| fibgo-stock | 6 | 0 | OK | 0 | 3.937889 | 101 | 63 | 8f3171cb13a2651b |  |  |
| fibgo-mine | 6 | 0 | OK | 0 | 1.657340 | 106 | 63 | 8f3171cb13a2651b | firecrackerVM=fcweb1 launchToHTTP200=135.779987ms probe=tcp |  |
| fibgo-stock | 7 | 0 | OK | 0 | 3.900291 | 101 | 63 | 8f3171cb13a2651b |  |  |
| fibgo-mine | 7 | 0 | OK | 0 | 1.815578 | 155 | 63 | 8f3171cb13a2651b | firecrackerVM=fcweb1 launchToHTTP200=135.031699ms probe=tcp |  |
