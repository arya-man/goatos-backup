package http

import (
	"strings"

	"github.com/vgoats/goatos/backend/internal/notificationcentre/domain"
)

// contextPayload is the routing envelope the phone/web client uses to open the right screen.
// Every field is optional: a notification that names no task carries no task_id, and the
// client must not receive an empty string it would then render or route on.
type contextPayload struct {
	TaskID     *string `json:"task_id,omitempty"`
	TaskNo     *string `json:"task_no,omitempty"`
	Screen     *string `json:"screen,omitempty"`
	GroupKey   *string `json:"group_key,omitempty"`
	Priority   *string `json:"priority,omitempty"`
	MessageKey *string `json:"message_key,omitempty"`
	Target     *string `json:"target,omitempty"`
	Status     *string `json:"status,omitempty"`
	LoadID     *string `json:"load_id,omitempty"`
}

// notificationPayload is one card in the notification centre.
type notificationPayload struct {
	NotificationRequestID string         `json:"notification_request_id"`
	NotificationType      string         `json:"notification_type"`
	Title                 string         `json:"title"`
	Body                  string         `json:"body"`
	Status                string         `json:"status"`
	RequestedAt           string         `json:"requested_at"`
	ReadAt                *string        `json:"read_at,omitempty"`
	ActorName             *string        `json:"actor_name,omitempty"`
	Context               contextPayload `json:"context"`
}

// notificationPagePayload is the read response.
type notificationPagePayload struct {
	Items       []notificationPayload `json:"items"`
	UnreadCount int                   `json:"unread_count"`
	TotalCount  int                   `json:"total_count"`
	NextCursor  *string               `json:"next_cursor,omitempty"`
	TraceID     string                `json:"trace_id"`
}

// markReadRequestPayload is the write body.
type markReadRequestPayload struct {
	NotificationRequestIDs []string `json:"notification_request_ids"`
}

// markReadResponsePayload is the write response.
type markReadResponsePayload struct {
	ReadCount int `json:"read_count"`
}

// optional renders an absent string as an absent JSON field.
func optional(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func toContextPayload(c domain.Context) contextPayload {
	return contextPayload{
		TaskID:     optional(c.TaskID),
		TaskNo:     optional(c.TaskNo),
		Screen:     optional(c.Screen),
		GroupKey:   optional(c.GroupKey),
		Priority:   optional(c.Priority),
		MessageKey: optional(c.MessageKey),
		Target:     optional(c.Target),
		Status:     optional(c.Status),
		LoadID:     optional(c.LoadID),
	}
}

func toNotificationPayload(n domain.Notification) notificationPayload {
	return notificationPayload{
		NotificationRequestID: n.NotificationRequestID,
		NotificationType:      n.NotificationType,
		Title:                 n.Title,
		Body:                  n.Body,
		Status:                n.Status,
		RequestedAt:           n.RequestedAt,
		ReadAt:                optional(n.ReadAt),
		ActorName:             optional(n.ActorName),
		Context:               toContextPayload(n.Context),
	}
}
