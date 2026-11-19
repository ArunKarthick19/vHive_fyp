| service | n (cycles 2+) | n timed | mean s | median s | min s | max s | stdev s | non-200/failed |
|---|---|---|---|---|---|---|---|---|
| bb-stock | 10 | 10 | 4.246 | 4.249 | 4.208 | 4.292 | 0.021 | 0 |
| bb-mine | 10 | 10 | 1.785 | 1.905 | 1.238 | 1.960 | 0.286 | 0 |

ratio bb-stock/bb-mine: mean 2.38x, median 2.23x

all cycles (warm-up flagged):

| service | cycle | warmup | HTTP | curl_exit | time_total s | idle_wait s | bytes | sha256/16 | vm boot (vhive log) | note |
|---|---|---|---|---|---|---|---|---|---|---|
| bb-stock | 1 | 1 | 200 | 0 | 4.389757 | 9 | 439 | d05318e5f8904725 |  |  |
| bb-mine | 1 | 1 | 200 | 0 | 1.952954 | 105 | 439 | d05318e5f8904725 | firecrackerVM=fcweb1 launchToHTTP200=126.590746ms probe=tcp |  |
| bb-stock | 2 | 0 | 200 | 0 | 4.291979 | 101 | 439 | d05318e5f8904725 |  |  |
| bb-mine | 2 | 0 | 200 | 0 | 1.943091 | 105 | 439 | d05318e5f8904725 | firecrackerVM=fcweb1 launchToHTTP200=119.084078ms probe=tcp |  |
| bb-stock | 3 | 0 | 200 | 0 | 4.207874 | 102 | 439 | d05318e5f8904725 |  |  |
| bb-mine | 3 | 0 | 200 | 0 | 1.874109 | 144 | 439 | d05318e5f8904725 | firecrackerVM=fcweb1 launchToHTTP200=123.584423ms probe=tcp |  |
| bb-stock | 4 | 0 | 200 | 0 | 4.250332 | 101 | 439 | d05318e5f8904725 |  |  |
| bb-mine | 4 | 0 | 200 | 0 | 1.884136 | 106 | 439 | d05318e5f8904725 | firecrackerVM=fcweb1 launchToHTTP200=121.168405ms probe=tcp |  |
| bb-stock | 5 | 0 | 200 | 0 | 4.234982 | 102 | 439 | d05318e5f8904725 |  |  |
| bb-mine | 5 | 0 | 200 | 0 | 1.237908 | 155 | 439 | d05318e5f8904725 | firecrackerVM=fcweb1 launchToHTTP200=122.905081ms probe=tcp |  |
| bb-stock | 6 | 0 | 200 | 0 | 4.247836 | 101 | 439 | d05318e5f8904725 |  |  |
| bb-mine | 6 | 0 | 200 | 0 | 1.953671 | 105 | 439 | d05318e5f8904725 | firecrackerVM=fcweb1 launchToHTTP200=125.09455ms probe=tcp |  |
| bb-stock | 7 | 0 | 200 | 0 | 4.244006 | 102 | 439 | d05318e5f8904725 |  |  |
| bb-mine | 7 | 0 | 200 | 0 | 1.253150 | 155 | 439 | d05318e5f8904725 | firecrackerVM=fcweb1 launchToHTTP200=121.310819ms probe=tcp |  |
| bb-stock | 8 | 0 | 200 | 0 | 4.249928 | 101 | 439 | d05318e5f8904725 |  |  |
| bb-mine | 8 | 0 | 200 | 0 | 1.930812 | 105 | 439 | d05318e5f8904725 | firecrackerVM=fcweb1 launchToHTTP200=125.788394ms probe=tcp |  |
| bb-stock | 9 | 0 | 200 | 0 | 4.253166 | 101 | 439 | d05318e5f8904725 |  |  |
| bb-mine | 9 | 0 | 200 | 0 | 1.960419 | 106 | 439 | d05318e5f8904725 | firecrackerVM=fcweb1 launchToHTTP200=122.39031ms probe=tcp |  |
| bb-stock | 10 | 0 | 200 | 0 | 4.251134 | 102 | 439 | d05318e5f8904725 |  |  |
| bb-mine | 10 | 0 | 200 | 0 | 1.896748 | 106 | 439 | d05318e5f8904725 | firecrackerVM=fcweb1 launchToHTTP200=121.825588ms probe=tcp |  |
| bb-stock | 11 | 0 | 200 | 0 | 4.232350 | 102 | 439 | d05318e5f8904725 |  |  |
| bb-mine | 11 | 0 | 200 | 0 | 1.913861 | 106 | 439 | d05318e5f8904725 | firecrackerVM=fcweb1 launchToHTTP200=121.254609ms probe=tcp |  |
