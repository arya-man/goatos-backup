package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	ltports "github.com/vgoats/goatos/backend/internal/leadershiptasks/ports"
	"github.com/vgoats/goatos/backend/internal/workboard/ports"
)

// FlagService raises a flag on a board row to the park head.
type FlagService struct {
	raiser ports.FlagRaiser
	heads  ports.ParkHeadResolver
}

// NewFlagService constructs the service.
func NewFlagService(raiser ports.FlagRaiser, heads ports.ParkHeadResolver) *FlagService {
	return &FlagService{raiser: raiser, heads: heads}
}

// Errors the transport maps to stable codes.
var (
	ErrFlagRowRequired  = errors.New("workboard: flag needs a row")
	ErrFlagParkRequired = errors.New("workboard: flag needs a park")
	ErrFlagNoteTooLong  = errors.New("workboard: flag note too long")
)

const maxNoteRunes = 1000

// Flag composes the brief and raises it. The TITLE is the row's title under "Check", so the
// park head's list reads as work, and the BODY carries the pen, the clock, the director's
// note and the row key, so the trail leads back to the board.
func (s *FlagService) Flag(ctx context.Context, p ports.FlagParams) (ports.FlagResult, error) {
	p.RowKey = strings.TrimSpace(p.RowKey)
	p.RowTitle = strings.TrimSpace(p.RowTitle)
	p.Note = strings.TrimSpace(p.Note)
	if p.RowKey == "" || p.RowTitle == "" {
		return ports.FlagResult{}, ErrFlagRowRequired
	}
	if strings.TrimSpace(p.ParkID) == "" {
		return ports.FlagResult{}, ErrFlagParkRequired
	}
	if len([]rune(p.Note)) > maxNoteRunes {
		return ports.FlagResult{}, ErrFlagNoteTooLong
	}
	head, err := s.heads.ParkHead(ctx, p.TenantID, p.ParkID)
	if err != nil {
		return ports.FlagResult{}, err
	}
	task, err := s.raiser.Raise(ctx, ltports.RaiseParams{
		TenantID:         p.TenantID,
		ActorID:          p.ActorID,
		ActorDesignation: p.ActorDesignation,
		AssigneeUserID:   head.UserID,
		Title:            FlagTitle(p.RowTitle),
		Body:             FlagBody(p),
		IdempotencyKey:   p.IdempotencyKey,
	})
	if err != nil {
		return ports.FlagResult{}, err
	}
	return ports.FlagResult{TaskID: task.TaskID, TaskNo: task.TaskNo, AssigneeName: head.Name}, nil
}

// FlagTitle is the park head's line: "Check · Weigh Godel 1 - Part 3". Bounded to the
// Leadership Tasks title limit so a long row title never refuses the flag.
func FlagTitle(rowTitle string) string {
	title := "Check · " + rowTitle
	runes := []rune(title)
	if len(runes) > 80 {
		title = string(runes[:77]) + "..."
	}
	return title
}

// FlagBody is the brief, in farm words, ending with the row key so the trail leads back.
func FlagBody(p ports.FlagParams) string {
	lines := []string{}
	if p.RowSubtitle != "" {
		lines = append(lines, p.RowSubtitle)
	}
	where := []string{}
	if p.PenDisplay != "" {
		where = append(where, p.PenDisplay)
	}
	if p.ClockLabel != "" {
		where = append(where, p.ClockLabel)
	}
	if len(where) > 0 {
		lines = append(lines, strings.Join(where, " · "))
	}
	if p.Note != "" {
		lines = append(lines, "", p.Note)
	}
	lines = append(lines, "", fmt.Sprintf("Flagged from the Work Board · %s", p.RowKey))
	return strings.Join(lines, "\n")
}
