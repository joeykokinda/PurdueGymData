package main

import (
	"database/sql"
	"encoding/csv"
	"fmt"
	"log"
	"os"
	"strconv"
)

// One-time backfills from other people's logs of the same feed. All of them store the
// feed's raw LastUpdatedDateAndTime, so they dedupe against each other and against live data.
//
//	corec-tracker jake    corec_usage.db   github.com/Jake-Verburgt/corec-usage
//	corec-tracker kvijay  corec.db         github.com/im-kvijay/corec-tracker
//	corec-tracker gymscrape occupancy.csv  github.com/elliottbmiller/gymScrape
func runImport(db *sql.DB, arguments []string) {
	kind, path := arguments[0], ""
	if len(arguments) > 1 {
		path = arguments[1]
	}
	var readings []reading
	var err error
	switch kind {
	case "jake":
		readings, err = sqliteReadings(path, `SELECT LocationId, LocationName, LastUpdatedDateAndTime, LastCount, TotalCapacity, IsClosed FROM corec_usage`)
	case "kvijay":
		readings, err = sqliteReadings(path, `SELECT r.location_id, l.location_name, r.api_ts, r.last_count, r.capacity, r.is_closed
			FROM readings r JOIN locations l ON l.location_id = r.location_id`)
	case "gymscrape":
		readings, err = csvReadings(path)
	default:
		err = fmt.Errorf("unknown import %q", kind)
	}
	if err != nil {
		log.Fatal(err)
	}
	inserted, err := insertReadings(db, kind, readings)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("%s: %d readings read, %d new", kind, len(readings), inserted)
}

func sqliteReadings(path, query string) ([]reading, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}
	source, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		return nil, err
	}
	defer source.Close()
	rows, err := source.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var readings []reading
	for rows.Next() {
		var item reading
		if err := rows.Scan(&item.locationID, &item.name, &item.ts, &item.count, &item.capacity, &item.closed); err != nil {
			return nil, err
		}
		readings = append(readings, item)
	}
	return readings, rows.Err()
}

func csvReadings(path string) ([]reading, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	records, err := csv.NewReader(file).ReadAll()
	if err != nil {
		return nil, err
	}
	column := map[string]int{}
	for index, name := range records[0] {
		column[name] = index
	}
	var readings []reading
	for _, record := range records[1:] {
		locationID, errID := strconv.Atoi(record[column["location_id"]])
		count, errCount := strconv.Atoi(record[column["count"]])
		capacity, errCapacity := strconv.Atoi(record[column["capacity"]])
		if errID != nil || errCount != nil || errCapacity != nil {
			continue
		}
		readings = append(readings, reading{locationID, record[column["location_name"]], record[column["reading_time"]],
			count, capacity, record[column["is_closed"]] == "true"})
	}
	return readings, nil
}
