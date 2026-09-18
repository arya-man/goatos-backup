package domain

import (
	"errors"
	"strings"
	"time"
)

// @-MENTIONS ON A TASK NOTE (maintainer instruction 2026-09-18).
//
// A note's body is what someone typed; WHO they meant is a decision, and it is taken once, at
// write time, from explicit user ids the client sends alongside the text -- never by reading
// "@Ravi" back out of the body. Two active people can carry the same display name, a person
// can be renamed, and a regex over free text cannot tell a mention from a quoted handle. The
// resolved targets are stored (migration 000346) and read back as chips.
//
// The rule that matters is the VISIBILITY one below: only the leadership population (and the
// task's own parties) may be mentioned at all, and a mention RECORDS the mentioned person as a
// participant, which is what entitles them to open the task the push deep-links to.

// MaxMentionsPerNote bounds one note's mention list. A note is a sentence to a colleague, not
// a broadcast; a longer list is a client defect, not a leader's intent.
const MaxMentionsPerNote = 20

var (
	ErrTooManyMentions   = errors.New("leadership task: too many mentions")
	ErrInvalidMention    = errors.New("leadership task: mention target is not a valid person")
	ErrMentionNotVisible = errors.New("leadership task: mention target cannot see this task")
)

// Mention is one stored, resolved mention on a note. Name is resolved in the same read as the
// note, so the phone can render a chip without a second lookup.
type Mention struct {
	MentionID string
	NoteID    string
	UserID    string
	Name      string
	// MentionedByUserID is the note's author -- the person who took the decision.
	MentionedByUserID string
	CreatedAt         time.Time
}

// MentionableUser is one person the `@` autocomplete may offer on ONE task (maintainer
// decision 2026-09-18).
//
// # WHO IS ON THIS LIST
//
// The task's own two PARTIES -- the raiser and the assignee -- plus the LEADERSHIP
// POPULATION: everyone whose own mobile Tasks row is ticked at Oversee AND who holds one of
// the eight active leadership grants (CEO/CXO, park head, the six directors). That is exactly
// the population the assignee picker reads (repository sqlListAssignees, narrowed by migration
// 000292), and mentioning is deliberately no wider: senior staff only, never an arbitrary
// tenant user and never an operator.
//
// The parties are unioned in because a director who raised a task is often NOT ticked at
// Oversee -- their own tick is Do or Configure -- and the person who asked for the work must
// always be nameable in the thread about it.
type MentionableUser struct {
	UserID string
	Name   string
	Title  string
	// Relation names WHY this person is on the list: "raiser", "assignee" or "leadership".
	// The picker groups by it, and a reviewer can read the decision straight off the response.
	Relation string
}

// Mention relations.
const (
	MentionRelationRaiser     = "raiser"
	MentionRelationAssignee   = "assignee"
	MentionRelationLeadership = "leadership"
)

// IsParticipant reports whether the actor was pulled onto this task by a mention (migration
// 000346's leadership_task_participants). A MENTION GRANTS READ, the Jira behaviour: the push
// names the task title, so tapping it must open the task rather than 404, and the recipient is
// legitimately entitled to what the push already told them.
func (t Task) IsParticipant(a Actor) bool {
	if a.UserID == "" {
		return false
	}
	for _, id := range t.ParticipantUserIDs {
		if id == a.UserID {
			return true
		}
	}
	return false
}

// CanRead is THE visibility rule of a task, in one place: the person who raised it, the person
// it is addressed to, someone holding monitor authority over the tenant's leadership desk, or
// someone a note on this task mentioned. app.GetTask refuses anything else as not-found.
//
// A participant gains DIRECT OPEN only. The list scopes are untouched: being mentioned once
// does not hand someone the Team progress dashboard, and it does not put the task on their
// "For me" tab.
func (t Task) CanRead(a Actor) bool {
	return t.IsRaiser(a) || t.IsAssignee(a) || t.CanMonitor(a) || t.IsParticipant(a)
}

// NormalizeMentionTargets cleans a client's mention list before it is resolved against the
// roster: trims, drops blanks, drops the AUTHOR (mentioning yourself is not news and must not
// notify you), and dedupes while keeping the order the note named people in. It bounds the
// count but does NOT decide validity or visibility -- those need the stored row and are
// checked under its lock in the repository.
func NormalizeMentionTargets(ids []string, authorID string) ([]string, error) {
	author := strings.TrimSpace(authorID)
	out := make([]string, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for _, raw := range ids {
		id := strings.TrimSpace(raw)
		if id == "" || id == author {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	if len(out) > MaxMentionsPerNote {
		return nil, ErrTooManyMentions
	}
	return out, nil
}
