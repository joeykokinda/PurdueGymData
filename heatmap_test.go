package main

import (
	"net/url"
	"strings"
	"testing"
)

func TestHeatmap(t *testing.T) {
	grid := heatmap([]sample{
		{1, "2026-10-12T17:05:00", 40, 100}, // Monday: two readings in one hour average to 50
		{1, "2026-10-12T17:45:00", 60, 100},
		{1, "2026-10-19T17:10:00", 30, 100}, // next Monday: two areas sum to 40 of 200
		{2, "2026-10-19T17:20:00", 10, 100},
		{2, "2026-10-19T18:20:00", 20, 100}, // area 1 skipped this hour, so 20 of 100 scales to 40 of 200
	})
	monday := grid[1][17]
	if monday == nil || monday.Pct != 35 || monday.Count != 45 || monday.Days != 2 {
		t.Fatalf("got %+v, want pct 35 count 45 days 2", monday)
	}
	if late := grid[1][18]; late == nil || late.Count != 40 || late.Pct != 20 {
		t.Fatalf("got %+v, want count 40 pct 20", late)
	}
	if grid[2][17] != nil {
		t.Fatal("tuesday should be empty")
	}
}

func TestParseParams(t *testing.T) {
	good := url.Values{"area": {"Lower Gym"}, "from": {"2026-08-24"}, "to": {"2026-12-12"}}
	if parsed, err := parseParams(good); err != nil || parsed.to != "2026-12-12T99" {
		t.Fatalf("got %+v, %v", parsed, err)
	}
	for _, bad := range []url.Values{
		{"from": {"2026-08-24' OR 1=1"}},
		{"to": {"9999"}},
		{"area": {"a", "b", "c", "d", "e", "f"}},
		{"area": {strings.Repeat("x", 65)}},
	} {
		if _, err := parseParams(bad); err == nil {
			t.Fatalf("accepted %v", bad)
		}
	}
}
