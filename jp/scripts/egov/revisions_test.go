package egov

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tggo/lex/internal/schema"
)

func TestParseRevisions_golden(t *testing.T) {
	revs, err := ParseRevisions(readFixture(t, "revisions.sample.json"))
	if err != nil {
		t.Fatalf("ParseRevisions: %v", err)
	}
	got, err := json.MarshalIndent(revs, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')

	golden := filepath.Join("testdata", "revisions.golden.json")
	if *update {
		if err := os.WriteFile(golden, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("read golden (run with -update first): %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("ParseRevisions mismatch.\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestParseRevisions_fields(t *testing.T) {
	revs, err := ParseRevisions(readFixture(t, "revisions.sample.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(revs) != 3 {
		t.Fatalf("got %d revisions, want 3", len(revs))
	}
	for _, r := range revs {
		if r.Status != schema.StatusInForce {
			t.Errorf("revision %v status = %v, want InForce", r.VersionDate, r.Status)
		}
		if r.ProducedBy == "" {
			t.Errorf("revision %v missing ProducedBy", r.VersionDate)
		}
		if r.IsRepeal {
			t.Errorf("revision %v should not be a repeal", r.VersionDate)
		}
	}
	if !revs[0].VersionDate.Equal(time.Date(2016, 10, 13, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("first revision date = %v", revs[0].VersionDate)
	}
}

func TestParseRevisions_statusAndDrops(t *testing.T) {
	raw := `{"law_info":{"law_id":"L"},"revisions":[
	  {"amendment_enforcement_date":"2001-01-01","current_revision_status":"PreviousEnforced","amendment_law_id":"A"},
	  {"amendment_enforcement_date":"2030-01-01","current_revision_status":"UnEnforced","amendment_law_id":"B"},
	  {"amendment_enforcement_date":"2010-01-01","current_revision_status":"Repeal","repeal_status":"Repeal","amendment_law_id":"C"},
	  {"amendment_enforcement_date":"","current_revision_status":"PreviousEnforced","amendment_law_id":"D"},
	  {"amendment_enforcement_date":"2005-01-01","current_revision_status":"CurrentEnforced","amendment_law_id":"L"}
	]}`
	revs, err := ParseRevisions([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if len(revs) != 4 { // the empty-date one is dropped
		t.Fatalf("got %d revisions, want 4", len(revs))
	}
	if revs[0].Status != schema.StatusInForce {
		t.Errorf("PreviousEnforced should be InForce, got %v", revs[0].Status)
	}
	if revs[1].Status != schema.StatusUnknown {
		t.Errorf("UnEnforced should be Unknown, got %v", revs[1].Status)
	}
	if revs[2].Status != schema.StatusRepealed || !revs[2].IsRepeal {
		t.Errorf("Repeal should be Repealed+IsRepeal, got %v/%v", revs[2].Status, revs[2].IsRepeal)
	}
	if revs[3].ProducedBy != "" {
		t.Errorf("self-reference ProducedBy should be empty, got %q", revs[3].ProducedBy)
	}
}

func TestParseRevisions_invalidJSON(t *testing.T) {
	if _, err := ParseRevisions([]byte("{not json")); err == nil {
		t.Error("expected parse error")
	}
}
