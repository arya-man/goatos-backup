package app

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"strconv"

	"github.com/vgoats/goatos/backend/internal/herdsignals/domain"
	"github.com/vgoats/goatos/backend/internal/platform/csvutil"
)

const (
	// exportPageSize is how many rows are held in memory at once. The export is CHUNKED, not
	// materialised: each page is read, enriched, written to the response and dropped before the
	// next page is read, so peak memory is one page regardless of how many tags match.
	exportPageSize = 500
	// maxExportRows is the hard ceiling on one export. A file this size is already past what a
	// spreadsheet reader will do anything useful with, and an unbounded full-result read is the
	// exact thing the scale rules forbid. The last row of a capped file says the cap was hit --
	// a silently truncated export reads as complete data, which is the dangerous failure.
	maxExportRows = 100000
)

// ExportCSV streams the CURRENT LIVE VIEW as CSV, honouring every filter the list honours.
//
// It walks the SAME filtered, keyset-ordered result as GET /herd-signals/live (same repository
// filter builder, same page reads, same per-page enrichment), so what downloads is what the
// operator is looking at -- not a second query that agrees with the screen only by coincidence.
//
// BOUNDED: pages of exportPageSize rows, each written out and released before the next is read,
// capped at maxExportRows total. Nothing collects the whole result set.
func (s *Service) ExportCSV(ctx context.Context, actor domain.Actor, parkID, shedID, movementState, mappingState, pattern, q *string, w io.Writer) error {
	if actor.TenantID == "" {
		return fmt.Errorf("actor tenant_id required")
	}

	writer := csv.NewWriter(w)
	defer writer.Flush()

	// Column set mirrors the live table, in the table's own order. "Tag Temperature" is the
	// tag housing's own reading -- the tag has no animal-contact sensor, so there is no other
	// temperature this file could be carrying.
	header := []string{
		"Animal",
		"Tag ID",
		"Tag MAC",
		"Shed",
		"Gateway",
		"RSSI (dBm)",
		"Signal State",
		"Motion Count",
		"Motion Delta (15m)",
		"Motion Delta (1h)",
		"Delta Includes Reconnect Gap",
		"Movement State",
		"Pattern",
		"Battery (mV)",
		"Battery State",
		"Tag Temperature (C)",
		"Last Seen (UTC)",
		"Mapping State",
	}
	if err := writer.Write(header); err != nil {
		return err
	}

	cursor := ""
	written := 0
	for {
		tags, err := s.repo.ListTagsLatestPage(ctx, actor.TenantID, parkID, shedID, movementState, mappingState, pattern, q, cursor, exportPageSize)
		if err != nil {
			return fmt.Errorf("export page read failed: %w", err)
		}
		if len(tags) == 0 {
			break
		}

		// Same enrichment the live view applies (animal display id, shed/park names, battery
		// trend composition), batched per page -- so a cell in this file holds exactly what the
		// same cell on screen holds.
		items := s.enrichTagsBatch(ctx, actor.TenantID, tags)

		for _, item := range items {
			if err := csvutil.WriteSafeRow(writer, exportRow(item)); err != nil {
				return err
			}
			written++
			if written >= maxExportRows {
				// Say so IN the file. A reader who cannot see the cap was hit will read a
				// truncated export as the complete picture.
				if err := writer.Write([]string{fmt.Sprintf("EXPORT TRUNCATED at %d rows: narrow the filters and export again.", maxExportRows)}); err != nil {
					return err
				}
				writer.Flush()
				return writer.Error()
			}
		}

		// Flush each page so the download starts moving immediately and the buffer does not
		// grow with the result set.
		writer.Flush()
		if err := writer.Error(); err != nil {
			return err
		}

		if len(tags) < exportPageSize {
			break
		}
		cursor = tags[len(tags)-1].TagID
	}

	writer.Flush()
	return writer.Error()
}

func exportRow(item domain.LiveItem) []string {
	return []string{
		strFromPtr(item.DisplayID),
		item.TagID,
		item.TagMAC,
		strFromPtr(item.ShedName),
		strFromPtr(item.GatewayID),
		intFromPtr16(item.RSSIdbm),
		strFromPtr(item.SignalState),
		int64FromPtr(item.MotionCount),
		int64FromPtr(item.MotionDelta),
		int64FromPtr(item.MotionDelta1h),
		boolText(item.GapDelta),
		strFromPtr(item.MovementState),
		strFromPtr(item.PatternState),
		intFromPtr(item.BatteryMV),
		strFromPtr(item.BatteryState),
		floatFromPtr(item.TagTemperatureC),
		item.LastSeenAt,
		item.MappingState,
	}
}

// An absent value stays EMPTY in the file. It is never rendered as 0, "unknown", or "-": a
// blank cell reads as "not reported", which is the truth, while a zero reads as a measurement.
func strFromPtr(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func intFromPtr(v *int) string {
	if v == nil {
		return ""
	}
	return strconv.Itoa(*v)
}

func intFromPtr16(v *int16) string {
	if v == nil {
		return ""
	}
	return strconv.Itoa(int(*v))
}

func int64FromPtr(v *int64) string {
	if v == nil {
		return ""
	}
	return strconv.FormatInt(*v, 10)
}

func floatFromPtr(v *float64) string {
	if v == nil {
		return ""
	}
	return strconv.FormatFloat(*v, 'f', 1, 64)
}

func boolText(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}
