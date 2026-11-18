| service | n (cycles 2+) | n timed | mean s | median s | min s | max s | stdev s | non-200/failed |
|---|---|---|---|---|---|---|---|---|
| bb-stock | 10 | 10 | 3.765 | 3.565 | 3.530 | 4.591 | 0.417 | 0 |
| bb-mine | 10 | 10 | 1.394 | 1.328 | 1.258 | 1.899 | 0.191 | 0 |

ratio bb-stock/bb-mine: mean 2.70x, median 2.68x

all cycles (warm-up flagged):

| service | cycle | warmup | HTTP | curl_exit | time_total s | idle_wait s | bytes | sha256/16 | vm boot (vhive log) | note |
|---|---|---|---|---|---|---|---|---|---|---|
| bb-stock | 1 | 1 | 200 | 0 | 3.923956 | 97 | 439 | d05318e5f8904725 |  |  |
| bb-mine | 1 | 1 | 200 | 0 | 1.265076 | 106 | 439 | d05318e5f8904725 | firecrackerVM=fcweb1 launchToHTTP200=121.124468ms probe=tcp |  |
| bb-stock | 2 | 0 | 200 | 0 | 4.590873 | 101 | 439 | d05318e5f8904725 |  |  |
| bb-mine | 2 | 0 | 200 | 0 | 1.360965 | 105 | 439 | d05318e5f8904725 | firecrackerVM=fcweb1 launchToHTTP200=133.691944ms probe=tcp |  |
| bb-stock | 3 | 0 | 200 | 0 | 3.658885 | 99 | 439 | d05318e5f8904725 |  |  |
| bb-mine | 3 | 0 | 200 | 0 | 1.272048 | 105 | 439 | d05318e5f8904725 | firecrackerVM=fcweb1 launchToHTTP200=121.395667ms probe=tcp |  |
| bb-stock | 4 | 0 | 200 | 0 | 3.558117 | 101 | 439 | d05318e5f8904725 |  |  |
| bb-mine | 4 | 0 | 200 | 0 | 1.898777 | 153 | 439 | d05318e5f8904725 | firecrackerVM=fcweb1 launchToHTTP200=124.579802ms probe=tcp |  |
| bb-stock | 5 | 0 | 200 | 0 | 3.550357 | 101 | 439 | d05318e5f8904725 |  |  |
| bb-mine | 5 | 0 | 200 | 0 | 1.315984 | 105 | 439 | d05318e5f8904725 | firecrackerVM=fcweb1 launchToHTTP200=121.511676ms probe=tcp |  |
| bb-stock | 6 | 0 | 200 | 0 | 3.562142 | 102 | 439 | d05318e5f8904725 |  |  |
| bb-mine | 6 | 0 | 200 | 0 | 1.488553 | 159 | 439 | d05318e5f8904725 | firecrackerVM=fcweb1 launchToHTTP200=124.329063ms probe=tcp |  |
| bb-stock | 7 | 0 | 200 | 0 | 3.568191 | 101 | 439 | d05318e5f8904725 |  |  |
| bb-mine | 7 | 0 | 200 | 0 | 1.340643 | 105 | 439 | d05318e5f8904725 | firecrackerVM=fcweb1 launchToHTTP200=122.502888ms probe=tcp |  |
| bb-stock | 8 | 0 | 200 | 0 | 3.535533 | 102 | 439 | d05318e5f8904725 |  |  |
| bb-mine | 8 | 0 | 200 | 0 | 1.415381 | 159 | 439 | d05318e5f8904725 | firecrackerVM=fcweb1 launchToHTTP200=121.397822ms probe=tcp |  |
| bb-stock | 9 | 0 | 200 | 0 | 3.576378 | 101 | 439 | d05318e5f8904725 |  |  |
| bb-mine | 9 | 0 | 200 | 0 | 1.307511 | 106 | 439 | d05318e5f8904725 | firecrackerVM=fcweb1 launchToHTTP200=122.164565ms probe=tcp |  |
| bb-stock | 10 | 0 | 200 | 0 | 4.515165 | 102 | 439 | d05318e5f8904725 |  |  |
| bb-mine | 10 | 0 | 200 | 0 | 1.279061 | 106 | 439 | d05318e5f8904725 | firecrackerVM=fcweb1 launchToHTTP200=130.315922ms probe=tcp |  |
| bb-stock | 11 | 0 | 200 | 0 | 3.529778 | 101 | 439 | d05318e5f8904725 |  |  |
| bb-mine | 11 | 0 | 200 | 0 | 1.257998 | 106 | 439 | d05318e5f8904725 | firecrackerVM=fcweb1 launchToHTTP200=121.427219ms probe=tcp |  |
