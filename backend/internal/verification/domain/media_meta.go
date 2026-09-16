package domain

import (
	"strconv"
	"strings"
)

// Media kinds a verification proof can declare. Anything else -- an `either` slot the register
// never judged, an attachment, a kind this module has not heard of -- is UNKNOWN (blank), and an
// unknown kind is answered from the proof register at read time rather than guessed.
const (
	MediaKindVideo = "video"
	MediaKindPhoto = "photo"
)

// NormalizeMediaKind maps a producer's kind onto video / photo / "" (unknown). "image" is a photo;
// "either", "photo_or_video" and every unrecognised value are unknown -- never a guessed video.
func NormalizeMediaKind(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "video":
		return MediaKindVideo
	case "photo", "image":
		return MediaKindPhoto
	default:
		return ""
	}
}

// MimeType is the declared mime for the meta's kind, blank when the kind is unknown.
func (m MediaMeta) MimeType() string {
	switch NormalizeMediaKind(m.Kind) {
	case MediaKindVideo:
		return "video/mp4"
	case MediaKindPhoto:
		return "image/jpeg"
	default:
		return ""
	}
}

// ProofCapture is one producer capture in the order its refs are handed to CreateItem: the
// authored title the verifier reads it under and the kind it was captured (or judged) as.
type ProofCapture struct {
	Title string
	Kind  string
}

// BuildMediaMeta is the one shared mapping every producer bridge uses to turn its captures into
// MediaMeta: POSITIONAL (capture i names MediaRefs[i]), titles trimmed, kinds normalized. No
// captures builds no meta, which reads as "this item names none of its proofs".
func BuildMediaMeta(captures []ProofCapture) []MediaMeta {
	if len(captures) == 0 {
		return nil
	}
	out := make([]MediaMeta, len(captures))
	for i, c := range captures {
		out[i] = MediaMeta{Label: strings.TrimSpace(c.Title), Kind: NormalizeMediaKind(c.Kind)}
	}
	return out
}

// ComposeMediaLabels tells apart proofs a producer titled identically: every non-blank title that
// appears more than once across ONE item becomes "Title k of N" in item order. Distinct titles
// and blanks are returned untouched (a blank is filled later from the category registry, whose own
// numbering stays "Video 2"). Composing an already-composed list changes nothing, because the
// numbered titles are no longer repeats.
func ComposeMediaLabels(labels []string) []string {
	out := make([]string, len(labels))
	copy(out, labels)
	counts := make(map[string]int, len(labels))
	for _, label := range labels {
		if key := strings.TrimSpace(label); key != "" {
			counts[key]++
		}
	}
	seen := make(map[string]int, len(counts))
	for i, label := range labels {
		key := strings.TrimSpace(label)
		if key == "" || counts[key] < 2 {
			continue
		}
		seen[key]++
		out[i] = key + " " + strconv.Itoa(seen[key]) + " of " + strconv.Itoa(counts[key])
	}
	return out
}
