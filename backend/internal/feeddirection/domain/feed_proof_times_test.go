package domain

import (
	"strings"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

func at(t *testing.T, clock string) time.Time {
	t.Helper()
	parsed, err := time.ParseInLocation("2006-01-02 15:04", clock, biztime.DefaultLocation())
	if err != nil {
		t.Fatalf("parse %q: %v", clock, err)
	}
	return parsed
}

func sampleReport(t *testing.T) FeedProofTimesReport {
	t.Helper()
	return FeedProofTimesReport{
		ParkID:   "park-cbe",
		ParkName: "Coimbatore",
		FeedDay:  "2026-09-05",
		Rows: []PenSessionProofTimes{
			{
				ShedID: "shed-castro", ShedName: "Castro", PartitionLabel: "1",
				SessionNo: 1, SessionLabel: "Morning", Workflow: "normal", Status: "completed",
				Weight:       FeedProofCapture{UploadedAt: at(t, "2026-09-05 09:12")},
				Distribution: FeedProofCapture{UploadedAt: at(t, "2026-09-05 09:31")},
				Water:        FeedProofCapture{UploadedAt: at(t, "2026-09-05 09:44")},
			},
			{
				ShedID: "shed-castro", ShedName: "Castro", PartitionLabel: "1",
				SessionNo: 2, SessionLabel: "Evening", Workflow: "normal", Status: "completed",
				Weight:       FeedProofCapture{UploadedAt: at(t, "2026-09-05 15:20")},
				Distribution: FeedProofCapture{UploadedAt: at(t, "2026-09-05 15:38")},
				Water:        FeedProofCapture{UploadedAt: at(t, "2026-09-05 15:51")},
			},
			// A SECOND pen whose evening never arrived -- the red row. Two pens are the minimum
			// that can prove the marker discriminates rather than being printed on every line.
			{
				ShedID: "shed-gandhi", ShedName: "Gandhi", PartitionLabel: "2",
				SessionNo: 1, SessionLabel: "Morning", Workflow: "normal", Status: "completed",
				Weight:       FeedProofCapture{UploadedAt: at(t, "2026-09-05 08:51")},
				Distribution: FeedProofCapture{UploadedAt: at(t, "2026-09-05 08:58")},
				Water:        FeedProofCapture{UploadedAt: at(t, "2026-09-05 09:02")},
			},
			{
				ShedID: "shed-gandhi", ShedName: "Gandhi", PartitionLabel: "2",
				SessionNo: 2, SessionLabel: "Evening", Workflow: "normal", Status: "pending_verification",
				Weight: FeedProofCapture{UploadedAt: at(t, "2026-09-05 15:26")},
				// Distribution and water never arrived.
			},
		},
	}
}

// The whole point of the report's shape: Slack has no inline colour, so "missing" is expressed as a
// diff DELETION line inside a ```diff block, which Slack renders red. A complete row must start with
// a space so it stays plain. If either marker moves, the farm stops seeing red where work is owed.
func TestSlackBodyMarksAPenMissingACaptureAsARedDiffLine(t *testing.T) {
	body := FeedProofTimesSlackBody(sampleReport(t), biztime.DefaultLocation())

	if !strings.Contains(body, "```diff\n") {
		t.Fatalf("table must be a diff-fenced block, so Slack renders the missing rows red:\n%s", body)
	}
	var complete, missing string
	for _, line := range strings.Split(body, "\n") {
		switch {
		case strings.Contains(line, "Castro 1"):
			complete = line
		case strings.Contains(line, "Gandhi 2"):
			missing = line
		}
	}
	if complete == "" || missing == "" {
		t.Fatalf("both pens must appear in the table:\n%s", body)
	}
	if !strings.HasPrefix(missing, "-") {
		t.Errorf("a pen missing a capture must start with '-' so Slack renders it red, got %q", missing)
	}
	if !strings.HasPrefix(complete, " ") {
		t.Errorf("a complete pen must start with a space so it stays plain, got %q", complete)
	}
	if !strings.Contains(missing, "--") {
		t.Errorf("a capture that never arrived must render as '--', got %q", missing)
	}
	if !strings.Contains(complete, "09:12") || !strings.Contains(complete, "15:51") {
		t.Errorf("upload times must render in IST HH:MM, got %q", complete)
	}
}

// ONE ROW PER PEN, with both sessions on it under their own headings -- the layout the maintainer
// chose after seeing a row per pen-session on real data.
func TestSlackBodyPutsBothSessionsOnOnePenRow(t *testing.T) {
	body := FeedProofTimesSlackBody(sampleReport(t), biztime.DefaultLocation())

	morning := strings.Index(body, "MORNING")
	evening := strings.Index(body, "EVENING")
	if morning < 0 || evening < 0 {
		t.Fatalf("both session headings must appear:\n%s", body)
	}
	if morning > evening {
		t.Errorf("morning must be the left column")
	}
	// The pen is named ONCE. Two rows per pen is exactly what this layout replaced.
	if got := strings.Count(body, "Castro 1"); got != 1 {
		t.Errorf("a pen must occupy one row, found its name %d times:\n%s", got, body)
	}
	for _, line := range strings.Split(body, "\n") {
		if strings.Contains(line, "Castro 1") {
			// That row carries the morning AND the evening times.
			if !strings.Contains(line, "09:12") || !strings.Contains(line, "15:20") {
				t.Errorf("the pen row must carry both sessions, got %q", line)
			}
		}
	}
	if !strings.Contains(body, "1 of 2 pens missing a capture.") {
		t.Errorf("the footer must count PENS, not pen-sessions:\n%s", body)
	}
}

// A park that runs a third feeding gets a third column, rather than silently losing it: the session
// set is read off the sheet, never assumed to be {morning, evening}.
func TestSlackBodyRendersAThirdSessionWhenTheParkRunsOne(t *testing.T) {
	report := sampleReport(t)
	report.Rows = append(report.Rows, PenSessionProofTimes{
		ShedName: "Castro", PartitionLabel: "1", SessionNo: 3, SessionLabel: "Night",
		Weight: FeedProofCapture{UploadedAt: at(t, "2026-09-05 21:04")},
	})
	body := FeedProofTimesSlackBody(report, biztime.DefaultLocation())
	if !strings.Contains(body, "NIGHT") {
		t.Errorf("a third session must get its own column:\n%s", body)
	}
	if !strings.Contains(body, "21:04") {
		t.Errorf("the third session's captures must render:\n%s", body)
	}
}

// The pen is named the way the farm names it, through the canonical helper: numeric partitions join
// with a space ("Castro 1"), never a dash, and never the raw shed name alone.
func TestPenIsNamedTheWayTheFarmNamesIt(t *testing.T) {
	cases := map[string]PenSessionProofTimes{
		"Castro 1":         {ShedName: "Castro", PartitionLabel: "1"},
		"Godel 1 - Part 3": {ShedName: "Godel 1", PartitionLabel: "Part 3"},
		"Yashoda":          {ShedName: "Yashoda", PartitionLabel: ""},
	}
	for want, row := range cases {
		if got := row.PenDisplay(); got != want {
			t.Errorf("pen display = %q, want %q", got, want)
		}
	}
}

// A park with no live sheet says so in words. An empty diff block reads as a broken report rather
// than as "nothing was planned".
func TestSlackBodyWithNoPlannedPensSaysSoInWords(t *testing.T) {
	body := FeedProofTimesSlackBody(FeedProofTimesReport{ParkName: "Channapatna", FeedDay: "2026-09-05"}, nil)
	if strings.Contains(body, "```") {
		t.Errorf("no pens must not render an empty table:\n%s", body)
	}
	if !strings.Contains(body, "No feed sheet was issued") {
		t.Errorf("no pens must be stated in farm words:\n%s", body)
	}
}

// A complete day is reported as complete, not as a table with a silent footer.
func TestSlackBodyReportsACompleteDay(t *testing.T) {
	report := sampleReport(t)
	report.Rows[3].Distribution = FeedProofCapture{UploadedAt: at(t, "2026-09-05 15:41")}
	report.Rows[3].Water = FeedProofCapture{UploadedAt: at(t, "2026-09-05 15:52")}

	body := FeedProofTimesSlackBody(report, biztime.DefaultLocation())
	if !strings.Contains(body, "All 2 pens complete.") {
		t.Errorf("a complete day must say so:\n%s", body)
	}
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---") {
			t.Errorf("no row may render red when nothing is missing, got %q", line)
		}
	}
}

// The Slack gateway posts `Title + "\n" + Body` for every request, so a body carrying its own
// heading would render the heading TWICE in the channel. The title belongs to the notification;
// this pins that the table does not restate it.
func TestSlackBodyDoesNotRepeatTheTitle(t *testing.T) {
	report := sampleReport(t)
	title := FeedProofTimesTitle(report)
	body := FeedProofTimesSlackBody(report, biztime.DefaultLocation())

	if strings.Contains(body, title) {
		t.Errorf("the body must not restate the title %q -- the gateway already prepends it, so it "+
			"would appear twice in Slack:\n%s", title, body)
	}
	// What the channel actually receives, composed the way the gateway composes it.
	composed := title + "\n" + body
	if strings.Count(composed, "feed proof times") != 1 {
		t.Errorf("the composed message must carry exactly one heading:\n%s", composed)
	}
}
