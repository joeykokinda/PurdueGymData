package main

import "testing"

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
