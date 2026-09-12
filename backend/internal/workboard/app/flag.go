package app

import (
	"context"
	"errors"
	"strings"
	"time"

	ltports "github.com/vgoats/goatos/backend/internal/leadershiptasks/ports"
	"github.com/vgoats/goatos/backend/internal/workboard/domain"
	"github.com/vgoats/goatos/backend/internal/workboard/ports"
)

// FlagService raises a flag on a board row to the park head.
type FlagService struct {
	rows   ports.RowFinder
	raiser ports.FlagRaiser
	heads  ports.ParkHeadResolver
}

// NewFlagService constructs the service. rows is the board itself: the flagged row is looked
// up on the caller's own board, never taken from the request.
func NewFlagService(rows ports.RowFinder, raiser ports.FlagRaiser, heads ports.ParkHeadResolver) *FlagService {
	return &FlagService{rows: rows, raiser: raiser, heads: heads}
}

// Errors the transport maps to stable codes.
var (
	ErrFlagRowRequired  = errors.New("workboard: flag needs a row")
	ErrFlagRowNotFound  = errors.New("workboard: flagged row is not on the caller's board")
	ErrFlagParkRequired = errors.New("workboard: flag needs a park")
	ErrFlagNoteTooLong  = errors.New("workboard: flag note too long")
)

const maxNoteRunes = 1000

// Flag resolves the row on the caller's board, composes the brief from what the board
// really served, and raises it. The TITLE is the row's title under "Check", so the park
// head's list reads as work; the BODY carries the pen, the clock and the director's note.
//
// Looking the row up is the whole safety of the route: a caller cannot flag work outside
// their park or module visibility, and cannot put words in the board's mouth, because the
// title, pen and clock come from the row the board found, not from the request.
func (s *FlagService) Flag(ctx context.Context, p ports.FlagParams) (ports.FlagResult, error) {
	p.RowKey = strings.TrimSpace(p.RowKey)
	p.Note = strings.TrimSpace(p.Note)
	if p.RowKey == "" {
		return ports.FlagResult{}, ErrFlagRowRequired
	}
	if strings.TrimSpace(p.Board.ParkID) == "" {
		return ports.FlagResult{}, ErrFlagParkRequired
	}
	if len([]rune(p.Note)) > maxNoteRunes {
		return ports.FlagResult{}, ErrFlagNoteTooLong
	}
	row, found, err := s.rows.FindRow(ctx, p.Board, p.RowKey)
	if err != nil {
		return ports.FlagResult{}, err
	}
	if !found {
		return ports.FlagResult{}, ErrFlagRowNotFound
	}
	head, err := s.heads.ParkHead(ctx, p.TenantID, p.Board.ParkID)
	if err != nil {
		return ports.FlagResult{}, err
	}
	task, err := s.raiser.Raise(ctx, ltports.RaiseParams{
		TenantID:         p.TenantID,
		ActorID:          p.ActorID,
		ActorDesignation: p.ActorDesignation,
		AssigneeUserID:   head.UserID,
		Title:            FlagTitle(row.Title),
		Body:             FlagBody(row, p.Note),
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

// FlagBody is the brief, in farm words only: the row's subtitle, its pen and clock, the
// director's note, and the day it was flagged from. No row key, no id, no module token: the
// park head reads it on a phone, and the copy firewall applies to a task body as much as to
// a screen.
func FlagBody(row domain.Row, note string) string {
	lines := []string{}
	if row.Subtitle != "" {
		lines = append(lines, row.Subtitle)
	}
	where := []string{}
	if row.Pen.Display != "" {
		where = append(where, row.Pen.Display)
	}
	if row.ClockLabel != "" {
		where = append(where, row.ClockLabel)
	}
	if len(where) > 0 {
		lines = append(lines, strings.Join(where, " · "))
	}
	if note != "" {
		lines = append(lines, "", note)
	}
	from := "Flagged from the Work Board"
	if day, err := time.Parse("2006-01-02", row.BusinessDate); err == nil {
		from += " · " + day.Format("02/01/2006")
	}
	lines = append(lines, "", from)
	return strings.Join(lines, "\n")
}
