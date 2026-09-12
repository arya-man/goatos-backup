package domain

import "github.com/vgoats/goatos/backend/internal/platform/biztime"

// RollupChip is a ROUND card's one visit chip over its pens (maintainer decision 2026-09-12: the
// visit is the work's last step, so a round whose videos are all verified is not "Done" while
// its pens still owe their visits). visits are the pens' visit rows; owedWithoutRow counts pens
// whose videos are verified but whose visit row is not born yet (it arrives tomorrow morning).
// The most urgent pen names the card: another video owed > delayed > owed today > owed later
// > in review > verified. Both strings are backend copy the phone renders verbatim.
func RollupChip(visits []Task, owedWithoutRow int, today string) (chip, tone string) {
	var (
		rework, delayed, dueToday, dueLater, inReview, verified int
		delayedSince, later                                     string
	)
	for _, t := range visits {
		switch {
		case t.Status == StatusRework:
			rework++
		case t.Status == StatusPendingVerification:
			inReview++
		case t.Status == StatusCompleted || t.WorkState == WorkStateCompleted:
			verified++
		case t.WorkState == WorkStateCanceled:
		case t.WorkState == WorkStateDelayed:
			delayed++
			since := t.PlannedDate
			if t.DelayedSince != nil && *t.DelayedSince != "" {
				since = *t.DelayedSince
			}
			if delayedSince == "" || since < delayedSince {
				delayedSince = since
			}
		case t.DueDate == today:
			dueToday++
		default:
			dueLater++
			if later == "" || t.DueDate < later {
				later = t.DueDate
			}
		}
	}
	switch {
	case rework > 0:
		return "Visit needs another video", "danger"
	case delayed > 0:
		return "Visit delayed since " + biztime.FarmDateFromBusinessDate(delayedSince), "danger"
	case dueToday > 0:
		if dueToday == 1 {
			return "Visit pen today", "info"
		}
		return "Visit pens today", "info"
	case owedWithoutRow > 0:
		return "Pen visit tomorrow", "info"
	case dueLater > 0:
		return "Visit pen " + biztime.FarmDateFromBusinessDate(later), "info"
	case inReview > 0:
		return "Visit in review", "review"
	case verified > 0:
		return "Visit verified", "success"
	}
	return "", ""
}
