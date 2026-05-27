package egov

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/tggo/lex/internal/schema"
)

// revisionsResponse is the envelope of GET /api/2/law_revisions/{law_id}.
type revisionsResponse struct {
	LawInfo   lawInfo        `json:"law_info"`
	Revisions []revisionInfo `json:"revisions"`
}

// RevisionMeta is the metadata of one historical/scheduled revision of an act,
// before resolution into graph edges. ProducedBy is the amending-or-repealing
// law_id (empty for an initial enactment / self-reference); IsRepeal says which
// edge it becomes once resolved.
type RevisionMeta struct {
	VersionDate time.Time
	Status      schema.Status
	ProducedBy  string
	IsRepeal    bool
}

// revisionStatus maps a revision's e-Gov status into a lex status. Enforced
// revisions (current or previous) were in force at their time; a repeal is
// repealed; anything else (e.g. UnEnforced, scheduled) is unknown.
func revisionStatus(ri revisionInfo) schema.Status {
	if ri.RepealStatus != "" && ri.RepealStatus != "None" {
		return schema.StatusRepealed
	}
	switch ri.CurrentRevisionStatus {
	case "CurrentEnforced", "PreviousEnforced":
		return schema.StatusInForce
	default:
		return schema.StatusUnknown
	}
}

// ParseRevisions decodes a law_revisions response into one RevisionMeta per
// revision that has an enforcement (version) date. Revisions without one are
// dropped — the same as the as-of-date rule for the current expression.
func ParseRevisions(b []byte) ([]RevisionMeta, error) {
	var resp revisionsResponse
	if err := json.Unmarshal(b, &resp); err != nil {
		return nil, fmt.Errorf("egov: parse revisions: %w", err)
	}
	out := make([]RevisionMeta, 0, len(resp.Revisions))
	for _, ri := range resp.Revisions {
		vd, ok := parseDate(ri.EnforcementDate)
		if !ok {
			continue
		}
		producedBy := ri.AmendmentLawID
		if producedBy == resp.LawInfo.LawID {
			producedBy = ""
		}
		out = append(out, RevisionMeta{
			VersionDate: vd,
			Status:      revisionStatus(ri),
			ProducedBy:  producedBy,
			IsRepeal:    ri.RepealStatus != "" && ri.RepealStatus != "None",
		})
	}
	return out, nil
}
