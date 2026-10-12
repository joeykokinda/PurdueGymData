package main

import (
	"database/sql"
	_ "embed"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	_ "time/tzdata"

	_ "modernc.org/sqlite"
)

const feedURL = "https://goboardapi.azurewebsites.net/api/FacilityCount/GetCountsByAccount?AccountAPIKey=aedeaf92-036d-4848-980b-7eb5526ea40c"

const pollInterval = 10 * time.Minute

var campus, _ = time.LoadLocation("America/Indiana/Indianapolis")

//go:embed index.html
var indexHTML []byte

// Timestamps are the feed's LastUpdatedDateAndTime, campus local time, truncated to seconds.
// Weekday and hour are read straight off that string, so no timezone handling is needed.
const schema = `
CREATE TABLE IF NOT EXISTS locations(
	id INTEGER PRIMARY KEY,
	name TEXT NOT NULL,
	seen TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS readings(
	location_id INTEGER NOT NULL,
	ts TEXT NOT NULL,
	count INTEGER NOT NULL,
	capacity INTEGER NOT NULL,
	closed INTEGER NOT NULL,
	source TEXT NOT NULL,
	PRIMARY KEY(location_id, ts)
) WITHOUT ROWID;
CREATE INDEX IF NOT EXISTS readings_ts ON readings(ts);`

// Semester date ranges from Purdue's academic calendar (first day of classes to end of finals).
// ponytail: breaks are not excluded, so spring/thanksgiving break slightly lowers those weeks.
var semesters = []struct {
	Name  string `json:"name"`
	Start string `json:"start"`
	End   string `json:"end"`
}{
	{"Fall 2026", "2026-08-24", "2026-12-12"},
	{"Summer 2026", "2026-05-18", "2026-08-08"},
	{"Spring 2026", "2026-01-12", "2026-05-02"},
	{"Fall 2025", "2025-08-25", "2025-12-13"},
	{"Summer 2025", "2025-05-19", "2025-08-09"},
	{"Spring 2025", "2025-01-13", "2025-05-03"},
	{"Fall 2024", "2024-08-26", "2024-12-14"},
	{"Fall 2022", "2022-08-22", "2022-12-17"},
}

type feedItem struct {
	LocationId             int
	LocationName           string
	TotalCapacity          int
	LastCount              int
	LastUpdatedDateAndTime string
	IsClosed               bool
}

type reading struct {
	locationID int
	name       string
	ts         string
	count      int
	capacity   int
	closed     bool
}

func openDB(path string) *sql.DB {
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		log.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		log.Fatal(err)
	}
	return db
}

// insertReadings dedupes on (location, ts) and keeps each location's most recently seen name.
func insertReadings(db *sql.DB, source string, readings []reading) (int64, error) {
	transaction, err := db.Begin()
	if err != nil {
		return 0, err
	}
	defer transaction.Rollback()
	var inserted int64
	for _, item := range readings {
		if len(item.ts) < 19 {
			continue
		}
		ts := item.ts[:19]
		if _, err := transaction.Exec(`INSERT INTO locations(id, name, seen) VALUES(?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET name = excluded.name, seen = excluded.seen WHERE excluded.seen > seen`,
			item.locationID, strings.TrimSpace(item.name), ts); err != nil {
			return 0, err
		}
		result, err := transaction.Exec(`INSERT OR IGNORE INTO readings VALUES(?, ?, ?, ?, ?, ?)`,
			item.locationID, ts, item.count, item.capacity, item.closed, source)
		if err != nil {
			return 0, err
		}
		affected, _ := result.RowsAffected()
		inserted += affected
	}
	return inserted, transaction.Commit()
}

func feedReadings(items []feedItem) []reading {
	readings := make([]reading, 0, len(items))
	for _, item := range items {
		readings = append(readings, reading{item.LocationId, item.LocationName, item.LastUpdatedDateAndTime,
			item.LastCount, item.TotalCapacity, item.IsClosed})
	}
	return readings
}

var httpClient = &http.Client{Timeout: 30 * time.Second}

func fetchFeed(url string) ([]feedItem, error) {
	request, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", "corec-tracker (+https://corec.jaeger.lol)")
	response, err := httpClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", url, response.Status)
	}
	var items []feedItem
	return items, json.NewDecoder(response.Body).Decode(&items)
}

func pollForever(db *sql.DB) {
	for {
		items, err := fetchFeed(feedURL)
		if err == nil {
			var inserted int64
			inserted, err = insertReadings(db, "live", feedReadings(items))
			log.Printf("poll: %d locations, %d new readings", len(items), inserted)
		}
		if err != nil {
			log.Printf("poll: %v", err)
		}
		time.Sleep(pollInterval)
	}
}

type cell struct {
	Pct   float64 `json:"pct"`
	Count float64 `json:"count"`
	Low   float64 `json:"low"`  // 25th percentile count
	High  float64 `json:"high"` // 75th percentile count
	Days  int     `json:"days"`
}

type sample struct {
	locationID int
	ts         string
	count      int
	capacity   int
}

type total struct{ count, capacity float64 }

// quantile expects sorted values and interpolates linearly between neighbours.
func quantile(sorted []float64, q float64) float64 {
	position := q * float64(len(sorted)-1)
	lower := int(position)
	if lower+1 >= len(sorted) {
		return sorted[lower]
	}
	return sorted[lower] + (position-float64(lower))*(sorted[lower+1]-sorted[lower])
}

// hourlyTotals averages each location's readings within a calendar hour ("2006-01-02T15")
// and sums the locations, so a group of areas reads as one space.
func hourlyTotals(samples []sample) map[string]total {
	type locationHour struct {
		locationID int
		hour       string
	}
	type sums struct{ count, capacity, readings float64 }
	perLocation := map[locationHour]*sums{}
	for _, item := range samples {
		key := locationHour{item.locationID, item.ts[:13]}
		if perLocation[key] == nil {
			perLocation[key] = &sums{}
		}
		perLocation[key].count += float64(item.count)
		perLocation[key].capacity += float64(item.capacity)
		perLocation[key].readings++
	}
	totals := map[string]total{}
	for key, sum := range perLocation {
		hour := totals[key.hour]
		hour.count += sum.count / sum.readings
		hour.capacity += sum.capacity / sum.readings
		totals[key.hour] = hour
	}
	return totals
}

// heatmap returns a [weekday][hour] grid (Sunday = 0) of the median hourly total
// across every date with that weekday.
func heatmap(samples []sample) [7][24]*cell {
	var pcts, counts [7][24][]float64
	for hour, sum := range hourlyTotals(samples) {
		date, err := time.Parse("2006-01-02T15", hour)
		if err != nil || sum.capacity == 0 {
			continue
		}
		weekday, hourOfDay := date.Weekday(), date.Hour()
		pcts[weekday][hourOfDay] = append(pcts[weekday][hourOfDay], 100*sum.count/sum.capacity)
		counts[weekday][hourOfDay] = append(counts[weekday][hourOfDay], sum.count)
	}
	var grid [7][24]*cell
	for weekday := range grid {
		for hour := range grid[weekday] {
			pct, count := pcts[weekday][hour], counts[weekday][hour]
			if len(pct) == 0 {
				continue
			}
			sort.Float64s(pct)
			sort.Float64s(count)
			grid[weekday][hour] = &cell{quantile(pct, 0.5), quantile(count, 0.5),
				quantile(count, 0.25), quantile(count, 0.75), len(pct)}
		}
	}
	return grid
}

func querySamples(db *sql.DB, areas []string, from, to string) ([]sample, error) {
	statement := `SELECT location_id, ts, count, capacity FROM readings WHERE closed = 0 AND ts >= ? AND ts <= ?`
	arguments := []any{from, to}
	if len(areas) > 0 {
		statement += ` AND location_id IN (SELECT id FROM locations WHERE name IN (?` + strings.Repeat(`, ?`, len(areas)-1) + `))`
		for _, area := range areas {
			arguments = append(arguments, area)
		}
	}
	rows, err := db.Query(statement, arguments...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var samples []sample
	for rows.Next() {
		var item sample
		if err := rows.Scan(&item.locationID, &item.ts, &item.count, &item.capacity); err != nil {
			return nil, err
		}
		samples = append(samples, item)
	}
	return samples, rows.Err()
}

func writeJSON(writer http.ResponseWriter, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.Header().Set("Cache-Control", "public, max-age=300")
	json.NewEncoder(writer).Encode(value)
}

func serve(db *sql.DB, address string) {
	http.HandleFunc("GET /{$}", func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "text/html; charset=utf-8")
		writer.Write(indexHTML)
	})

	http.HandleFunc("GET /api/semesters", func(writer http.ResponseWriter, request *http.Request) {
		writeJSON(writer, semesters)
	})

	http.HandleFunc("GET /api/live", func(writer http.ResponseWriter, request *http.Request) {
		rows, err := db.Query(`SELECT l.name, r.count, r.capacity, r.closed, r.ts FROM locations l
			JOIN readings r ON r.location_id = l.id AND r.ts = (SELECT max(ts) FROM readings WHERE location_id = l.id)
			WHERE r.ts > ? ORDER BY l.name`, time.Now().AddDate(0, 0, -4).Format("2006-01-02"))
		if err != nil {
			http.Error(writer, err.Error(), 500)
			return
		}
		defer rows.Close()
		type live struct {
			Name     string `json:"name"`
			Count    int    `json:"count"`
			Capacity int    `json:"capacity"`
			Closed   bool   `json:"closed"`
			Updated  string `json:"updated"`
		}
		areas := []live{}
		for rows.Next() {
			var area live
			rows.Scan(&area.Name, &area.Count, &area.Capacity, &area.Closed, &area.Updated)
			areas = append(areas, area)
		}
		writer.Header().Set("Cache-Control", "public, max-age=60")
		json.NewEncoder(writer).Encode(areas)
	})

	http.HandleFunc("GET /api/areas", func(writer http.ResponseWriter, request *http.Request) {
		rows, err := db.Query(`SELECT name FROM locations GROUP BY name ORDER BY name`)
		if err != nil {
			http.Error(writer, err.Error(), 500)
			return
		}
		defer rows.Close()
		names := []string{}
		for rows.Next() {
			var name string
			rows.Scan(&name)
			names = append(names, name)
		}
		writeJSON(writer, names)
	})

	// /api/heatmap?area=Lower%20Gym&area=Upper%20Gym&from=2026-08-24&to=2026-12-12; no area means the whole building.
	http.HandleFunc("GET /api/heatmap", func(writer http.ResponseWriter, request *http.Request) {
		query := request.URL.Query()
		from, to := query.Get("from"), query.Get("to")
		if from == "" {
			from = "0000"
		}
		if to == "" {
			to = "9999"
		} else {
			to += "T99"
		}
		samples, err := querySamples(db, query["area"], from, to)
		if err != nil {
			http.Error(writer, err.Error(), 500)
			return
		}
		writeJSON(writer, heatmap(samples))
	})

	// /api/today?area=...: hourly totals so far for the current campus date, indexed by hour.
	http.HandleFunc("GET /api/today", func(writer http.ResponseWriter, request *http.Request) {
		today := time.Now().In(campus).Format("2006-01-02")
		samples, err := querySamples(db, request.URL.Query()["area"], today, today+"T99")
		if err != nil {
			http.Error(writer, err.Error(), 500)
			return
		}
		var hours [24]*float64
		for hour, sum := range hourlyTotals(samples) {
			count := sum.count
			hours[int(hour[11]-'0')*10+int(hour[12]-'0')] = &count
		}
		writer.Header().Set("Content-Type", "application/json")
		writer.Header().Set("Cache-Control", "public, max-age=60")
		json.NewEncoder(writer).Encode(map[string]any{"date": today, "hours": hours})
	})

	log.Printf("listening on %s", address)
	log.Fatal(http.ListenAndServe(address, nil))
}

func main() {
	databasePath := envOr("DB_PATH", "corec.db")
	db := openDB(databasePath)
	if len(os.Args) > 1 {
		runImport(db, os.Args[1:])
		return
	}
	go pollForever(db)
	serve(db, envOr("ADDR", ":8080"))
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
