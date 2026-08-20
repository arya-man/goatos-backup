package http

import (
	"encoding/json"
	"net/http"
	"testing"

	countsapp "github.com/vgoats/goatos/backend/internal/counts/app"
	"github.com/vgoats/goatos/backend/internal/counts/domain"
)

// Newborn placement must resolve to a K0 pen (maintainer decision 2026-08-20).
//
// These tests exercise the PRODUCTION handler path -- the same RecordBirthEvent / ListShiftingDestinations
// the phone calls -- and assert on what reaches the identity validator and the approval queue, so a
// rejection that still queued the birth would fail.

const (
	testK0ShedID    = "66666666-6666-4666-8666-666666666666"
	testBuckShedID  = "77777777-7777-4777-8777-777777777777"
	testOtherParkID = "88888888-8888-4888-8888-888888888888"
	testOtherK0Shed = "99999999-9999-4999-8999-999999999999"
)

func strptr(s string) *string { return &s }

// oneK0PenCatalog is a park holding exactly one kid pen ("Yashoda 5") next to a non-kid pen
// ("Castro 1", tagged Buck). The kid pen is deliberately a PARTITION so the partition half of the
// contract is exercised, and the buck pen is a real alternative the operator could pick today.
func oneK0PenCatalog() domain.ShiftingDestinationCatalog {
	return domain.ShiftingDestinationCatalog{
		ManagementStages: []string{"K0", "Buck", "F2"},
		Parks: []domain.ShiftingDestinationPark{{
			ParkID: testParkID,
			Name:   "Coimbatore",
			Sheds: []domain.ShiftingDestinationShed{
				{
					ShedID: testK0ShedID, Name: "Yashoda",
					PartitionLabel: strptr("5"), Display: "Yashoda 5",
					ConfiguredStage: "K0",
				},
				{
					ShedID: testBuckShedID, Name: "Castro",
					PartitionLabel: strptr("1"), Display: "Castro 1",
					ConfiguredStage: "Buck",
				},
			},
		}},
	}
}

func twoK0PenCatalog() domain.ShiftingDestinationCatalog {
	catalog := oneK0PenCatalog()
	catalog.Parks[0].Sheds = append(catalog.Parks[0].Sheds, domain.ShiftingDestinationShed{
		ShedID: testK0ShedID, Name: "Yashoda",
		PartitionLabel: strptr("6"), Display: "Yashoda 6",
		ConfiguredStage: "K0",
	})
	return catalog
}

func birthBodyInto(identifier, parkID, shedID string, partition *string) map[string]any {
	body := birthBody(identifier)
	body["park_id"] = parkID
	body["shed_id"] = shedID
	if partition != nil {
		body["partition_label"] = *partition
	}
	return body
}

// THE DEFECT. Before this change a birth naming a Buck pen was accepted verbatim: the handler
// required park_id only (to derive the provisional tag prefix) and never looked at the shed. The
// kid was pinned K0 and filed with the bucks.
func TestRecordBirthEventRejectsANonKidPenWhenTheParkHasOne(t *testing.T) {
	validator := newFakeGoatValidator()
	approvals := newFakeApprovalWorkflow()
	repo := newFakeShiftingRepo()
	repo.destinations = oneK0PenCatalog()
	mux := newTestServer(t, countsapp.NewService(repo), approvals, validator)

	rec := post(t, mux, appBirthEventRoute, "birth-k0-buck-01",
		birthBodyInto("KID-BUCK", testParkID, testBuckShedID, strptr("1")))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s, want 400 (a kid may not be recorded into a Buck pen)", rec.Code, rec.Body.String())
	}
	if validator.prepareCreates != 0 || approvals.submits != 0 {
		t.Fatalf("prepareCreates=%d submits=%d, want 0/0 (a refused placement must not reach the queue)",
			validator.prepareCreates, approvals.submits)
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("error body is not JSON: %v", err)
	}
	if payload["code"] != "invalid_newborn_placement" {
		t.Fatalf("code=%v, want invalid_newborn_placement", payload["code"])
	}
}

// The kid pen itself is accepted, so the rejection above is about the PEN and not about the new
// check refusing everything.
func TestRecordBirthEventAcceptsTheParksKidPen(t *testing.T) {
	validator := newFakeGoatValidator()
	approvals := newFakeApprovalWorkflow()
	repo := newFakeShiftingRepo()
	repo.destinations = oneK0PenCatalog()
	mux := newTestServer(t, countsapp.NewService(repo), approvals, validator)

	rec := post(t, mux, appBirthEventRoute, "birth-k0-ok-01",
		birthBodyInto("KID-OK", testParkID, testK0ShedID, strptr("5")))

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s, want 202", rec.Code, rec.Body.String())
	}
	if approvals.submits != 1 {
		t.Fatalf("submits=%d, want 1", approvals.submits)
	}
}

// A park with SEVERAL kid pens: every kid pen is accepted, and a non-kid pen is still refused. The
// operator chooses among the kid pens; the backend never ranks them.
func TestRecordBirthEventAcceptsAnyKidPenWhenTheParkHasSeveral(t *testing.T) {
	for _, tc := range []struct {
		name      string
		partition string
	}{{"first kid pen", "5"}, {"second kid pen", "6"}} {
		t.Run(tc.name, func(t *testing.T) {
			validator := newFakeGoatValidator()
			approvals := newFakeApprovalWorkflow()
			repo := newFakeShiftingRepo()
			repo.destinations = twoK0PenCatalog()
			mux := newTestServer(t, countsapp.NewService(repo), approvals, validator)

			rec := post(t, mux, appBirthEventRoute, "birth-k0-multi-"+tc.partition,
				birthBodyInto("KID-MULTI-"+tc.partition, testParkID, testK0ShedID, strptr(tc.partition)))
			if rec.Code != http.StatusAccepted {
				t.Fatalf("status=%d body=%s, want 202", rec.Code, rec.Body.String())
			}
		})
	}
}

// A kid pen in ANOTHER park is never a valid destination for this park's birth. Parks resolve
// independently; nothing reads across them.
func TestRecordBirthEventNeverAcceptsAKidPenFromAnotherPark(t *testing.T) {
	validator := newFakeGoatValidator()
	approvals := newFakeApprovalWorkflow()
	repo := newFakeShiftingRepo()
	catalog := oneK0PenCatalog()
	catalog.Parks = append(catalog.Parks, domain.ShiftingDestinationPark{
		ParkID: testOtherParkID, Name: "Channapatna",
		Sheds: []domain.ShiftingDestinationShed{{
			ShedID: testOtherK0Shed, Name: "Gandhi",
			PartitionLabel: strptr("2"), Display: "Gandhi 2", ConfiguredStage: "K0",
		}},
	})
	repo.destinations = catalog
	mux := newTestServer(t, countsapp.NewService(repo), approvals, validator)

	rec := post(t, mux, appBirthEventRoute, "birth-k0-crosspark-01",
		birthBodyInto("KID-CROSSPARK", testParkID, testOtherK0Shed, strptr("2")))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s, want 400 (another park's kid pen is not this park's placement)",
			rec.Code, rec.Body.String())
	}
}

// A park with NO kid pen configured must still record the birth. A birth is a real event that
// already happened; losing it over missing pen configuration would make the herd register lie.
func TestRecordBirthEventStillRecordsWhenTheParkHasNoKidPen(t *testing.T) {
	validator := newFakeGoatValidator()
	approvals := newFakeApprovalWorkflow()
	repo := newFakeShiftingRepo()
	catalog := oneK0PenCatalog()
	catalog.Parks[0].Sheds[0].ConfiguredStage = "F2" // the kid pen is no longer one
	repo.destinations = catalog
	mux := newTestServer(t, countsapp.NewService(repo), approvals, validator)

	rec := post(t, mux, appBirthEventRoute, "birth-k0-none-01",
		birthBodyInto("KID-NOPEN", testParkID, testBuckShedID, strptr("1")))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s, want 202 (a birth is never lost over missing pen setup)",
			rec.Code, rec.Body.String())
	}
	if approvals.submits != 1 {
		t.Fatalf("submits=%d, want 1", approvals.submits)
	}
}

// Every child of a litter lands in the same pen: the placement is validated once, from the one
// shed_id the request carries, and the litter fans out from that single body.
func TestRecordTwinBirthPlacesBothChildrenInTheSameKidPen(t *testing.T) {
	validator := newFakeGoatValidator()
	approvals := newFakeApprovalWorkflow()
	repo := newFakeShiftingRepo()
	repo.destinations = oneK0PenCatalog()
	mux := newTestServer(t, countsapp.NewService(repo), approvals, validator)

	body := birthBodyInto("unused", testParkID, testK0ShedID, strptr("5"))
	delete(body, "animal_identifier_1")
	body["litter_size"] = 2
	rec := post(t, mux, appBirthEventRoute, "birth-k0-twins-01", body)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s, want 202", rec.Code, rec.Body.String())
	}
	if validator.prepareCreates != 2 {
		t.Fatalf("prepareCreates=%d, want 2", validator.prepareCreates)
	}
	var stored map[string]any
	if err := json.Unmarshal(approvals.lastSubmission.Payload, &stored); err != nil {
		t.Fatalf("stored payload is not JSON: %v", err)
	}
	if stored["shed_id"] != testK0ShedID {
		t.Fatalf("stored shed_id=%v, want %s for every child", stored["shed_id"], testK0ShedID)
	}
}

// The form contract the phone renders. Asserted on the SERIALIZED wire bytes, not on the Go struct,
// because a field that exists in Go and never reaches the wire is the exact contract-lie this
// project has shipped before: a declared-but-unpopulated field reads as done and renders as absent.
func TestShiftingDestinationsCarriesTheBirthPlacementContract(t *testing.T) {
	for _, tc := range []struct {
		name       string
		catalog    domain.ShiftingDestinationCatalog
		wantMode   string
		wantNotice string
		wantPens   int
	}{
		{
			name: "one kid pen places automatically and names the pen", catalog: oneK0PenCatalog(),
			wantMode: "automatic", wantNotice: "Kids born in this park go to Yashoda 5.", wantPens: 1,
		},
		{
			name: "several kid pens ask the operator to choose", catalog: twoK0PenCatalog(),
			wantMode: "choose", wantNotice: "Choose which kid pen this kid goes into.", wantPens: 2,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := newFakeShiftingRepo()
			repo.destinations = tc.catalog
			mux := newTestServer(t, countsapp.NewService(repo), newFakeApprovalWorkflow(), newFakeGoatValidator())

			rec := get(t, mux, appShiftingDestinationsRoute)
			if rec.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s, want 200", rec.Code, rec.Body.String())
			}
			var payload struct {
				Parks []struct {
					ParkID         string `json:"park_id"`
					BirthPlacement struct {
						Mode   string `json:"mode"`
						Notice string `json:"notice"`
						Pens   []struct {
							ShedID                     string  `json:"shed_id"`
							ShedName                   string  `json:"shed_name"`
							PartitionLabel             *string `json:"partition_label"`
							OperationalLocationDisplay string  `json:"operational_location_display"`
						} `json:"pens"`
					} `json:"birth_placement"`
				} `json:"parks"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
				t.Fatalf("response is not JSON: %v", err)
			}
			if len(payload.Parks) == 0 {
				t.Fatal("no parks in response")
			}
			got := payload.Parks[0].BirthPlacement
			if got.Mode != tc.wantMode {
				t.Fatalf("mode=%q, want %q", got.Mode, tc.wantMode)
			}
			if got.Notice != tc.wantNotice {
				t.Fatalf("notice=%q, want %q (backend owns this copy; the phone renders it verbatim)",
					got.Notice, tc.wantNotice)
			}
			if len(got.Pens) != tc.wantPens {
				t.Fatalf("pens=%d, want %d (only kid pens are offered)", len(got.Pens), tc.wantPens)
			}
			for _, pen := range got.Pens {
				// Every half of the operational location must survive to the wire. Dropping any one
				// of them renders a bare shed name end to end.
				if pen.ShedID == "" || pen.ShedName == "" || pen.PartitionLabel == nil ||
					pen.OperationalLocationDisplay == "" {
					t.Fatalf("pen is missing part of its operational location: %+v", pen)
				}
				// The HUMAN label, never the normalized matching key, and never a hand-rolled join.
				if pen.OperationalLocationDisplay != pen.ShedName+" "+*pen.PartitionLabel {
					t.Fatalf("display=%q, want the composed operational location for %q + %q",
						pen.OperationalLocationDisplay, pen.ShedName, *pen.PartitionLabel)
				}
			}
		})
	}
}

// A park with no kid pen reports record_later with NO pens, so the form falls back to the full
// cascade instead of showing an empty picker with nothing selectable.
func TestShiftingDestinationsReportsRecordLaterWhenNoKidPenExists(t *testing.T) {
	repo := newFakeShiftingRepo()
	catalog := oneK0PenCatalog()
	catalog.Parks[0].Sheds[0].ConfiguredStage = "F2"
	repo.destinations = catalog
	mux := newTestServer(t, countsapp.NewService(repo), newFakeApprovalWorkflow(), newFakeGoatValidator())

	rec := get(t, mux, appShiftingDestinationsRoute)
	var payload struct {
		Parks []struct {
			BirthPlacement struct {
				Mode   string `json:"mode"`
				Notice string `json:"notice"`
				Pens   []any  `json:"pens"`
			} `json:"birth_placement"`
		} `json:"parks"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("response is not JSON: %v", err)
	}
	got := payload.Parks[0].BirthPlacement
	if got.Mode != "record_later" {
		t.Fatalf("mode=%q, want record_later", got.Mode)
	}
	if len(got.Pens) != 0 {
		t.Fatalf("pens=%d, want 0", len(got.Pens))
	}
	if got.Notice == "" {
		t.Fatal("record_later must still carry a farm-worded notice; the operator is owed the reason")
	}
}
