package ogd

import (
	"testing"
	"time"
)

func TestParseHistory(t *testing.T) {
	// Real shape: "date:code:|date:code:" — order should come out ascending.
	evs := ParseHistory("20260530:1:|20260430:4:")
	if len(evs) != 2 {
		t.Fatalf("got %d events, want 2", len(evs))
	}
	if !evs[0].Date.Equal(time.Date(2026, 4, 30, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("first date = %v, want 2026-04-30 (sorted)", evs[0].Date)
	}
	if evs[0].Code != 4 || evs[1].Code != 1 {
		t.Errorf("codes = %d,%d want 4,1", evs[0].Code, evs[1].Code)
	}
}

func TestParseHistory_skipsJunk(t *testing.T) {
	if got := ParseHistory(""); got != nil {
		t.Errorf("empty -> %v, want nil", got)
	}
	// Bad/zero dates are skipped; a valid one survives.
	evs := ParseHistory("notadate:1:||00000000:2:|20200115:3:")
	if len(evs) != 1 || !evs[0].Date.Equal(time.Date(2020, 1, 15, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("got %+v, want one 2020-01-15 event", evs)
	}
}

func TestRevisionDates_uniqueSorted(t *testing.T) {
	dates := RevisionDates("20200115:1:|20200115:2:|20190101:3:")
	if len(dates) != 2 {
		t.Fatalf("got %d dates, want 2 (deduped)", len(dates))
	}
	if !dates[0].Equal(time.Date(2019, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("first = %v, want 2019-01-01", dates[0])
	}
}
