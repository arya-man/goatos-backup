package authored

import (
	"strconv"
	"strings"
)

// Evidence is the SNAPSHOT of what a capture form recorded (Add birth, Add death, Raise
// shifting): every proof under its authored title with the kind the register judged it as, every
// answer in farm words, and -- for a submission from an app older than the card -- the note
// naming what the card asked for and never received. It is taken ONCE at capture and rides the
// approver's row and the verifier's item verbatim; a later publish never relabels it.
//
// Wire shape (CountsApprovalCapture on the app API; workflow_instances.capture_evidence and
// counts_approval_requests.capture_evidence in the database). Owned by the herd-operations
// workstream; shifting populates the same shape for its raise form.
type Evidence struct {
	VersionLabel string          `json:"version_label,omitempty"`
	Media        []EvidenceMedia `json:"media,omitempty"`
	Rows         []EvidenceRow   `json:"rows,omitempty"`
	MissingNote  string          `json:"missing_note,omitempty"`
}

// EvidenceMedia is one captured proof: its register ref, its kind (video / photo; blank when the
// register could not say) and the authored slot title the reviewer reads it under.
type EvidenceMedia struct {
	// Key is the authored slot key the proof was captured under (verification per slot keys on
	// it; blank on a snapshot with no slots, e.g. shifting's legacy raise).
	Key   string `json:"key,omitempty"`
	Ref   string `json:"ref"`
	Kind  string `json:"kind"`
	Label string `json:"label"`
}

// EvidenceRow is one answer in farm words. Group names the section the row belongs to
// (the form, a step) so a reviewer sees answers under the work that produced them.
type EvidenceRow struct {
	Label string `json:"label"`
	Value string `json:"value"`
	Group string `json:"group,omitempty"`
}

// IsEmpty reports a snapshot that names nothing: no proof, no answer, no missing note.
func (e Evidence) IsEmpty() bool {
	return len(e.Media) == 0 && len(e.Rows) == 0 && strings.TrimSpace(e.MissingNote) == ""
}

// MissingNoteOlderApp is the note stamped for every compulsory slot or question a submission
// from an app older than the card did not send (maintainer decision 7, 2026-09-16). Farm copy,
// rendered verbatim to the approver and the verifier.
const MissingNoteOlderApp = "Not captured (older app)"

// OLDER APP CAPTURES (program decision 7: never force an update). An older app sends its fixed
// proof fields only. A capture with no authored slot to land on -- the card was re-authored to
// photos, or dropped the seeded slot -- is still ACCEPTED and kept under a RESERVED key per kind
// ("older_app_video", "older_app_video_2", "older_app_photo", ...), shown to the verifier as
// OlderAppLabel. ValidateProofSlots refuses every reserved key, so none can ever be authored.
const (
	OlderAppLabel     = "Recorded on an older app"
	olderAppKeyPrefix = "older_app_"
)

// OlderAppKey is the reserved key of the n-th (1-based) older-app capture of a kind.
func OlderAppKey(kind string, n int) string {
	key := olderAppKeyPrefix + kind
	if n > 1 {
		key += "_" + strconv.Itoa(n)
	}
	return key
}

// IsOlderAppKey reports whether a slot key is reserved for an older app's capture.
func IsOlderAppKey(key string) bool { return strings.HasPrefix(key, olderAppKeyPrefix) }

// OlderAppSlot is the pseudo slot a reserved key is judged and labelled by: never compulsory, of
// the kind its key names.
func OlderAppSlot(key string) ProofSlot {
	kind := KindVideo
	if strings.HasPrefix(key, olderAppKeyPrefix+KindPhoto) {
		kind = KindPhoto
	}
	return ProofSlot{Key: key, Title: OlderAppLabel, Kind: kind}
}
