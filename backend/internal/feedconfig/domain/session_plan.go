package domain

import (
	"errors"
	"fmt"
	"strings"
)

// WriteKindSessionTemplate covers authoring a park's feeding sessions: how many a day, what each is
// called, and the share of the day's quantity each carries.
const WriteKindSessionTemplate = "session_template"

// MaxSessionsPerPark bounds one park's session plan. Two a day is the farm's practice; the bound
// is a sanity cap on the write, not a business rule about how often animals eat.
const MaxSessionsPerPark = 8

// sessionSplitScale mirrors feed_session_templates.split_fraction numeric(6,4).
const sessionSplitScale = 4

var (
	// ErrSessionSplitNotWhole refuses a plan whose shares do not add up to the whole day. A park
	// whose sessions sum to less than 1 under-feeds every pen by the gap; more than 1 over-feeds.
	ErrSessionSplitNotWhole = errors.New("feedconfig: session shares must add up to the whole day")
	// ErrDuplicateSession refuses a plan naming one session number twice.
	ErrDuplicateSession = errors.New("feedconfig: session number appears more than once")
	// ErrTooManySessions bounds the write.
	ErrTooManySessions = errors.New("feedconfig: too many sessions in one plan")
)

// SessionPlanEntry is one feeding session as the author set it. SplitFraction is a canonical
// decimal string with four places ("0.5000").
type SessionPlanEntry struct {
	SessionNo     int32
	Label         string
	SplitFraction string
}

// SetSessionPlanCommand replaces a park's ACTIVE feeding sessions with exactly Sessions. A session
// that is listed is added or edited in place; an active session that is not listed is retired.
//
// This is the writer the baseline migration's feed_session_templates comment asks for ("must be
// re-validated by any future session-template editor"): the shares are checked to sum to the whole
// day here, before anything is written. Until it existed only a seed command could create sessions,
// so a park added on Configuration > Items & settings had no way to be fed.
type SetSessionPlanCommand struct {
	WriteIdentity
	ParkID   string
	Sessions []SessionPlanEntry
}

// ValidateSessionPlan canonicalises and checks a plan: at least one session, at most
// MaxSessionsPerPark, each number >= 1 and used once, each label present, each share in (0, 1] with
// at most four decimal places, and the shares adding up to exactly 1.
func ValidateSessionPlan(entries []SessionPlanEntry) ([]SessionPlanEntry, error) {
	if len(entries) == 0 {
		return nil, fieldErr("sessions", ErrMissingField, "at least one feeding session is required")
	}
	if len(entries) > MaxSessionsPerPark {
		return nil, fieldErr("sessions", ErrTooManySessions, fmt.Sprintf("at most %d sessions", MaxSessionsPerPark))
	}
	out := make([]SessionPlanEntry, 0, len(entries))
	seen := map[int32]bool{}
	var total int64
	for i, e := range entries {
		prefix := fmt.Sprintf("sessions[%d]", i)
		if e.SessionNo < 1 {
			return nil, fieldErr(prefix+".session_no", ErrValueOutOfRange, "must be 1 or more")
		}
		if seen[e.SessionNo] {
			return nil, fieldErr(prefix+".session_no", ErrDuplicateSession, fmt.Sprintf("session %d", e.SessionNo))
		}
		seen[e.SessionNo] = true
		label := strings.Join(strings.Fields(e.Label), " ")
		if label == "" {
			return nil, fieldErr(prefix+".session_label", ErrMissingField, "")
		}
		if len(label) > 60 {
			return nil, fieldErr(prefix+".session_label", ErrValueOutOfRange, "at most 60 characters")
		}
		split, err := NormalizeDecimal(prefix+".split_fraction", e.SplitFraction, sessionSplitScale, false)
		if err != nil {
			return nil, err
		}
		units, err := decimalUnits(split, sessionSplitScale)
		if err != nil {
			return nil, fieldErr(prefix+".split_fraction", ErrInvalidDecimal, split)
		}
		if units > 10000 {
			return nil, fieldErr(prefix+".split_fraction", ErrValueOutOfRange, "a session cannot carry more than the whole day")
		}
		total += units
		out = append(out, SessionPlanEntry{SessionNo: e.SessionNo, Label: label, SplitFraction: split})
	}
	if total != 10000 {
		return nil, fieldErr("sessions", ErrSessionSplitNotWhole,
			fmt.Sprintf("the shares add up to %d.%02d%%, not 100%%", total/100, total%100))
	}
	return out, nil
}
