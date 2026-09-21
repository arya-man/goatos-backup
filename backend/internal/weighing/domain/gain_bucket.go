package domain

import "strings"

// GainBucket names how the Time-wise reads cut a period into columns (maintainer request
// 2026-09-21, "weekly or monthly ... every 30 days, kitna tha average").
//
// TWO BUCKETS, AND THEY ARE NOT THE SAME SHAPE. A WEEK is a CALENDAR week: Monday to Sunday in
// Asia/Kolkata, which is what the farm's own week is and what every weekly series on this page
// has always meant. A MONTH here is NOT a calendar month -- it is a rolling 30-DAY BLOCK counted
// BACK FROM THE END OF THE SELECTED PERIOD: the last 30 days, then the 30 before that, and so on.
// The maintainer asked for it in those words, and it is the honest shape for growth: calendar
// months are 28 to 31 days long, so an animal gaining at a steady rate would read differently in
// February than in March purely because the bucket changed length.
//
// The anchor is the selected period's LAST day, never today, so the columns always sit inside the
// window the rest of the page reads. Anchoring at the window's START instead would put the short,
// partial block at the RECENT end -- the column a reader looks at first would be the one made of
// the fewest days.
const (
	GainBucketWeek  = "week"
	GainBucketMonth = "month"
)

// GainBucketDays is the length of a rolling month bucket. Thirty days, not a calendar month: see
// the type comment.
const GainBucketDays = 30

// NormalizeGainBucket resolves the caller's `bucket` parameter. Blank is the WEEK default, so every
// caller that predates this parameter -- Android's growth read included -- gets exactly the series
// it got before it existed. An unknown value is REFUSED rather than defaulted: silently falling
// back to weeks would put week columns under a heading the reader selected as months.
func NormalizeGainBucket(raw string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", GainBucketWeek:
		return GainBucketWeek, true
	case GainBucketMonth:
		return GainBucketMonth, true
	default:
		return "", false
	}
}

// TimeScope is what the Time-wise tab's own two controls ask the reads for: how wide a column is,
// and which pen the whole tab is about (maintainer correction 2026-09-21 -- "when I select pen, that
// pen only should be visible in whole page ... if it comes under any load, that load data only").
//
// The pen is a SCOPE, not a post-filter, because three of the tab's four sections are server-side
// aggregates: the overall chart is one number per bucket, the breed rows are grouped by breed, and
// the load rows are grouped by load. None of them carries a pen to filter on afterwards, so
// narrowing has to happen where the rows are still per-pen -- before the grouping.
//
// A pen is (location, partition), never a name: a pen name repeats across parks, and narrowing on
// one would silently merge CBE's Castro 1 with CPT's. An EMPTY PenLocationID is every pen, which is
// the query these reads ran before this scope existed.
type TimeScope struct {
	Bucket            string
	PenLocationID     string
	PenPartitionLabel string
}

// PenSelected reports whether the scope narrows to one pen.
func (s TimeScope) PenSelected() bool { return strings.TrimSpace(s.PenLocationID) != "" }

// CacheKey identifies the scope inside a read cache key.
//
// IT MUST BE PART OF EVERY CACHED READ THAT HONOURS IT. The weighing analytics reads memoise their
// answers per (tenant, parks, window, sex, origin, mode); a scope left out of that key hands a
// request for ONE PEN the whole farm's cached answer, or the reverse -- which is exactly what the
// first browser run of this feature showed.
func (s TimeScope) CacheKey() string {
	bucket := s.Bucket
	if bucket == "" {
		bucket = GainBucketWeek
	}
	if !s.PenSelected() {
		return bucket
	}
	return bucket + "|pen=" + strings.TrimSpace(s.PenLocationID) + "|" + s.PenPartitionLabel
}
