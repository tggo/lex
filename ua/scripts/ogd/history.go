package ogd

import (
	"sort"
	"strconv"
	"strings"
	"time"
)

// RevisionEvent is one entry from a card's `history` field: a redaction date and
// its event-type code. The OGD `perv` dataset ships only the *current*
// consolidated text, so these give the act's redaction timeline (dates), not the
// historical text of each version. See ADR-0014.
type RevisionEvent struct {
	Date time.Time
	Code int
}

// ParseHistory parses a card `history` field of the form
// "YYYYMMDD:code:|YYYYMMDD:code:|…" into chronologically sorted events.
// Entries without a valid date are skipped.
func ParseHistory(field string) []RevisionEvent {
	var events []RevisionEvent
	for _, part := range strings.Split(field, "|") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		fields := strings.Split(part, ":")
		d, err := parseYmd(atoiSafe(fields[0]))
		if err != nil {
			continue
		}
		ev := RevisionEvent{Date: d}
		if len(fields) > 1 {
			ev.Code = atoiSafe(fields[1])
		}
		events = append(events, ev)
	}
	sort.Slice(events, func(i, j int) bool { return events[i].Date.Before(events[j].Date) })
	return events
}

// RevisionDates returns the unique redaction dates from a `history` field, sorted
// ascending.
func RevisionDates(field string) []time.Time {
	seen := map[string]bool{}
	var out []time.Time
	for _, ev := range ParseHistory(field) {
		key := ev.Date.Format("2006-01-02")
		if !seen[key] {
			seen[key] = true
			out = append(out, ev.Date)
		}
	}
	return out
}

func atoiSafe(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}
