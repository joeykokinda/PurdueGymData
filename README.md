# PurdueGymData

How busy each area of Purdue's CoRec is, live and historically. Live at **https://corec.jaeger.lol**.

## Data

`data/readings.csv.gz`: every headcount reading for every CoRec area, Aug 2022 to now (nothing exists for Jan 2023 to Sep 2024).

| column | meaning |
|---|---|
| `ts` | when RecWell updated the count, campus local time |
| `location_id`, `area` | the area (IDs changed in Apr 2026 for a few areas, so group by `area`) |
| `count`, `capacity` | people in the area, and its capacity at that time |
| `closed` | 1 if the area was closed |
| `source` | where the reading came from (see below) |

Sources: RecWell's public feed (`live`, polled every 10 min), plus earlier logs from
[Jake-Verburgt/corec-usage](https://github.com/Jake-Verburgt/corec-usage) (`jake`, Apache-2.0),
[im-kvijay/corec-tracker](https://github.com/im-kvijay/corec-tracker) (`kvijay`) and
[elliottbmiller/gymScrape](https://github.com/elliottbmiller/gymScrape) (`gymscrape`).

## Run

```sh
go build && ./corec-tracker          # polls the feed and serves the site on :8080
./corec-tracker jake corec_usage.db  # one-time imports: jake | kvijay | gymscrape
```

Not affiliated with Purdue.
