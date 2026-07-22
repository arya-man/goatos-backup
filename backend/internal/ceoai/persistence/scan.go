package persistence

import (
	"encoding/json"
	"strings"
	"time"
)

// scannable is satisfied by both pgx.Row (single-row QueryRow) and pgx.Rows (Next+Scan),
// so the column-mapping helpers work in both the point-read and the pagination paths.
type scannable interface {
	Scan(dest ...any) error
}

func scanConversation(row scannable) (Conversation, error) {
	var (
		c          Conversation
		title      *string
		archivedAt *time.Time
		retention  *time.Time
	)
	if err := row.Scan(
		&c.ID, &c.TenantID, &c.ActorID, &title,
		&c.CreatedAt, &c.UpdatedAt, &archivedAt, &retention,
	); err != nil {
		return Conversation{}, err
	}
	if title != nil {
		c.Title = *title
	}
	c.ArchivedAt = archivedAt
	c.RetentionExpiresAt = retention
	return c, nil
}

func scanMessage(row scannable) (Message, error) {
	var (
		m         Message
		role      string
		toolCalls []byte
		citations []byte
		source    *string
		mode      *string
		requestID *string
	)
	if err := row.Scan(
		&m.ID, &m.ConversationID, &m.TenantID,
		&role, &m.Content, &toolCalls, &citations,
		&source, &mode, &requestID, &m.CreatedAt,
	); err != nil {
		return Message{}, err
	}
	m.Role = Role(role)
	if len(toolCalls) > 0 {
		m.ToolCalls = json.RawMessage(toolCalls)
	}
	if len(citations) > 0 {
		m.Citations = json.RawMessage(citations)
	}
	m.Source = deref(source)
	m.Mode = deref(mode)
	m.RequestID = deref(requestID)
	return m, nil
}

func scanFeedback(row scannable) (Feedback, error) {
	var (
		f      Feedback
		rating int16
		reason *string
	)
	if err := row.Scan(
		&f.ID, &f.MessageID, &f.TenantID, &f.ActorID,
		&rating, &reason, &f.CreatedAt,
	); err != nil {
		return Feedback{}, err
	}
	f.Rating = Rating(rating)
	f.Reason = deref(reason)
	return f, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// jsonbArg passes a JSON payload to a $n::jsonb bind, mapping empty to the SQL default
// (the column is NOT NULL DEFAULT '[]'::jsonb, so a nil bind uses that default).
func jsonbArg(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	return []byte(raw)
}

func clampPageSize(n int) int {
	if n <= 0 || n > MaxPageSize {
		return MaxPageSize
	}
	return n
}

// titleArg maps a blank title to SQL NULL (the column is nullable and the backend derives
// a title later); a non-blank title is trimmed and rune-bounded.
func titleArg(title string) any {
	t := strings.TrimSpace(title)
	if t == "" {
		return nil
	}
	return truncateRunes(t, MaxTitleLen)
}

func clampReason(reason string) string {
	return truncateRunes(strings.TrimSpace(reason), MaxReasonLen)
}

// truncateRunes bounds a string by rune count without splitting a multibyte rune.
func truncateRunes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
}
