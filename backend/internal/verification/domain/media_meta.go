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

// ComposeMediaLabels tells apart proofs a producer titled identically across ONE item, in item order.
// kinds is positional against labels and may be a kind ("video", "photo") or a mime ("video/mp4",
// "image/jpeg"); a missing or unrecognised entry is an unknown kind.
//
//   - A title repeated with the SAME kind (or with kinds nobody resolved) reads "Title k of N".
//   - A title repeated with DIFFERENT known kinds -- one step with a video and a photo -- reads
//     "Title · video" / "Title · photo", keeping "k of N" only among repeats of the same kind
//     ("Title · video 1 of 2"). Numbering a video and a photo as one series named two different
//     proofs as if they were the same thing twice.
//
// Distinct titles and blanks are returned untouched (a blank is filled later from the category
// registry, whose own numbering stays "Video 2"). Composing an already-composed list changes
// nothing, because the composed titles are no longer repeats.
func ComposeMediaLabels(labels []string, kinds []string) []string {
	out := make([]string, len(labels))
	copy(out, labels)
	kindAt := func(i int) string {
		if i >= len(kinds) {
			return ""
		}
		return mediaKindOf(kinds[i])
	}
	counts := make(map[string]int, len(labels))
	kindCounts := make(map[string]map[string]int, len(labels))
	for i, label := range labels {
		key := strings.TrimSpace(label)
		if key == "" {
			continue
		}
		counts[key]++
		if kindCounts[key] == nil {
			kindCounts[key] = map[string]int{}
		}
		kindCounts[key][kindAt(i)]++
	}
	seen := make(map[string]int, len(counts))
	for i, label := range labels {
		key := strings.TrimSpace(label)
		if key == "" || counts[key] < 2 {
			continue
		}
		byKind := kindCounts[key]
		known := 0
		for kind := range byKind {
			if kind != "" {
				known++
			}
		}
		if known < 2 {
			seen[key]++
			out[i] = key + " " + strconv.Itoa(seen[key]) + " of " + strconv.Itoa(counts[key])
			continue
		}
		kind := kindAt(i)
		title := key
		if kind != "" {
			title = key + " · " + kind
		}
		if byKind[kind] < 2 {
			out[i] = title
			continue
		}
		seenKey := key + "\x00" + kind
		seen[seenKey]++
		out[i] = title + " " + strconv.Itoa(seen[seenKey]) + " of " + strconv.Itoa(byKind[kind])
	}
	return out
}

// mediaKindOf reads a kind or a mime as video / photo / "" (unknown).
func mediaKindOf(kindOrMime string) string {
	v := strings.ToLower(strings.TrimSpace(kindOrMime))
	switch {
	case strings.HasPrefix(v, "video/"):
		return MediaKindVideo
	case strings.HasPrefix(v, "image/"):
		return MediaKindPhoto
	default:
		return NormalizeMediaKind(v)
	}
}
