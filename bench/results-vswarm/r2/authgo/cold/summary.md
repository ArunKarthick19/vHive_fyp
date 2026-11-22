| service | n (cycles 2+) | n timed | mean s | median s | min s | max s | stdev s | non-OK/failed |
|---|---|---|---|---|---|---|---|---|
| authgo-stock | 7 | 7 | 4.060 | 4.073 | 3.974 | 4.126 | 0.059 | 0 |
| authgo-mine | 7 | 7 | 1.647 | 1.633 | 1.587 | 1.732 | 0.052 | 0 |

ratio authgo-stock/authgo-mine: mean 2.46x, median 2.49x

all cycles (warm-up flagged):

| service | cycle | warmup | HTTP | curl_exit | time_total s | idle_wait s | bytes | sha256/16 | vm boot (vhive log) | note |
|---|---|---|---|---|---|---|---|---|---|---|
| authgo-stock | 1 | 1 | OK | 0 | 4.006074 | 91 | 341 | 65cddb0024a32feb |  |  |
| authgo-mine | 1 | 1 | OK | 0 | 1.810972 | 105 | 341 | 65cddb0024a32feb | firecrackerVM=fcweb1 launchToHTTP200=139.382831ms probe=tcp |  |
| authgo-stock | 2 | 0 | OK | 0 | 4.021111 | 102 | 341 | 65cddb0024a32feb |  |  |
| authgo-mine | 2 | 0 | OK | 0 | 1.732027 | 106 | 341 | 65cddb0024a32feb | firecrackerVM=fcweb1 launchToHTTP200=133.784871ms probe=tcp |  |
| authgo-stock | 3 | 0 | OK | 0 | 4.086027 | 102 | 341 | 65cddb0024a32feb |  |  |
| authgo-mine | 3 | 0 | OK | 0 | 1.679667 | 106 | 341 | 65cddb0024a32feb | firecrackerVM=fcweb1 launchToHTTP200=133.526229ms probe=tcp |  |
| authgo-stock | 4 | 0 | OK | 0 | 4.072671 | 101 | 341 | 65cddb0024a32feb |  |  |
| authgo-mine | 4 | 0 | OK | 0 | 1.621273 | 108 | 341 | 65cddb0024a32feb | firecrackerVM=fcweb1 launchToHTTP200=137.015458ms probe=tcp |  |
| authgo-stock | 5 | 0 | OK | 0 | 4.126124 | 101 | 341 | 65cddb0024a32feb |  |  |
| authgo-mine | 5 | 0 | OK | 0 | 1.586618 | 108 | 341 | 65cddb0024a32feb | firecrackerVM=fcweb1 launchToHTTP200=131.511341ms probe=tcp |  |
| authgo-stock | 6 | 0 | OK | 0 | 4.126496 | 102 | 341 | 65cddb0024a32feb |  |  |
| authgo-mine | 6 | 0 | OK | 0 | 1.597995 | 108 | 341 | 65cddb0024a32feb | firecrackerVM=fcweb1 launchToHTTP200=134.410003ms probe=tcp |  |
| authgo-stock | 7 | 0 | OK | 0 | 3.973820 | 102 | 341 | 65cddb0024a32feb |  |  |
| authgo-mine | 7 | 0 | OK | 0 | 1.632869 | 125 | 341 | 65cddb0024a32feb | firecrackerVM=fcweb1 launchToHTTP200=137.315866ms probe=tcp |  |
| authgo-stock | 8 | 0 | OK | 0 | 4.012768 | 101 | 341 | 65cddb0024a32feb |  |  |
| authgo-mine | 8 | 0 | OK | 0 | 1.678640 | 108 | 341 | 65cddb0024a32feb | firecrackerVM=fcweb1 launchToHTTP200=139.810426ms probe=tcp |  |
