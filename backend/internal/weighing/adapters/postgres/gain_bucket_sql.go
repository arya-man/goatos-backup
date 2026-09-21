package postgres

import (
	"fmt"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
)

// weightDemographicsAnchorParam is the placeholder the demographics query's month variant counts
// its 30-day blocks back from. It is the next number after that query's existing parameters, and it
// lives here beside the renderer so the two cannot drift apart.
const weightDemographicsAnchorParam = 34

// The two date shapes the Time-wise series bucket on: a scan's `accepted_at` (a timestamptz, read
// in Asia/Kolkata) and a pen weigh's `d` (already an IST date). Both live in the query templates as
// tokens so the WEEK and the MONTH variant are the same query with one expression swapped -- two
// hand-written copies would drift, and the page's headline and its grids must agree bucket for
// bucket.
const (
	bucketTokenAcceptedAt = "{{BUCKET_ACCEPTED_AT}}"
	bucketTokenDate       = "{{BUCKET_D}}"
)

// bucketedQuery renders a Time-wise query template for one bucket.
//
// WEEK is the calendar week, byte for byte the expression these queries carried before the bucket
// was selectable, so the weekly series is unchanged by this parameter existing.
//
// MONTH is a rolling 30-day block counted back from the window's last day, which arrives as
// $anchorParam. The arithmetic: days between the anchor and the row, integer-divided by 30, gives
// the block index (0 = the most recent 30 days); the block's START is the anchor less that many
// whole blocks and less the 29 days inside the block. A row can never sit after the anchor -- the
// window's own upper bound excludes it -- so the division is never negative.
//
// The WEEK variant does not mention $anchorParam at all, which is why the caller must not bind it:
// see growthWeeklyGain and GetWeightDemographics, where the anchor is appended only for MONTH.
func bucketedQuery(template, bucket string, anchorParam int) string {
	acceptedAt := "(date_trunc('week', accepted_at AT TIME ZONE 'Asia/Kolkata'))::date"
	date := "(date_trunc('week', d::timestamp))::date"
	if bucket == domain.GainBucketMonth {
		anchor := fmt.Sprintf("$%d::date", anchorParam)
		acceptedAt = rollingBucketExpr(anchor, "(accepted_at AT TIME ZONE 'Asia/Kolkata')::date")
		date = rollingBucketExpr(anchor, "d")
	}
	return strings.NewReplacer(bucketTokenAcceptedAt, acceptedAt, bucketTokenDate, date).Replace(template)
}

func rollingBucketExpr(anchor, dateExpr string) string {
	return fmt.Sprintf("(%s - %d - %d * ((%s - %s) / %d))",
		anchor, domain.GainBucketDays-1, domain.GainBucketDays, anchor, dateExpr, domain.GainBucketDays)
}

// gainBucketAnchor is the last business DATE inside the window, which the month variant counts its
// 30-day blocks back from. periodEnd is the window's EXCLUSIVE end (midnight IST of the day after
// the last one), so a second is taken off it before the business date is read; using periodEnd
// itself would anchor on a day the window does not contain and shift every block by one.
func gainBucketAnchor(periodEnd time.Time) string {
	return biztime.BusinessDate(periodEnd.Add(-time.Second))
}
