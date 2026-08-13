package http

import (
	"errors"
	"fmt"
	"strings"

	"github.com/vgoats/goatos/backend/internal/weighing/ports"
)

// maxNamedReworkTags caps how many identifiers the refusal spells out. Past a handful the
// message stops being a to-do list and starts being a wall, so the rest are counted.
const maxNamedReworkTags = 3

// reworkNotRecapturedMessage names the animals a verifier sent back. "Re-record that animal" is
// unactionable in a shed of forty -- the operator cannot tell which card to redo. When the error
// carries no tags (older call sites), it degrades to the original wording rather than lying.
func reworkNotRecapturedMessage(err error) string {
	var tagged *ports.ReworkNotRecapturedTags
	if !asReworkTags(err, &tagged) || len(tagged.Tags) == 0 {
		return "A video was sent back. Re-record that animal before submitting this shed again."
	}
	tags := tagged.Tags
	if len(tags) == 1 {
		return fmt.Sprintf("Video sent back for %s. Re-record it before submitting this shed again.", tags[0])
	}
	if len(tags) <= maxNamedReworkTags {
		return fmt.Sprintf("Videos sent back for %s. Re-record them before submitting this shed again.", strings.Join(tags, ", "))
	}
	return fmt.Sprintf(
		"Videos sent back for %s and %d more. Re-record them before submitting this shed again.",
		strings.Join(tags[:maxNamedReworkTags], ", "), len(tags)-maxNamedReworkTags,
	)
}

func asReworkTags(err error, target **ports.ReworkNotRecapturedTags) bool {
	return errors.As(err, target)
}
