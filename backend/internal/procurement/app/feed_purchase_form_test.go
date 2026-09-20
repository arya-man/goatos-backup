package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/procurement/domain"
	"github.com/vgoats/goatos/backend/internal/procurement/ports"
)

// formRepo is the ledger fake with its own vocabularies, so the form's catalog-backed questions
// have real choices to be judged against.
type formRepo struct {
	fakeFeedPurchaseRepo
}

func (r *formRepo) FeedPurchaseOptions(context.Context, string) (ports.FeedPurchaseOptions, error) {
	return ports.FeedPurchaseOptions{
		Farms:           domain.FeedFarms,
		FeedItems:       []ports.FeedItemOption{{Key: "maize", Label: "Maize"}},
		PaymentStatuses: domain.FeedPaymentStatuses,
	}, nil
}

// feedFormSource serves the seeded document plus one authored question as version 2.
type feedFormSource struct{ asked []int }

func withExtra() domain.VendorFormDSL {
	dsl := domain.SeededFeedPurchaseFormDSL()
	dsl.Pages = append(dsl.Pages, domain.VendorFormPage{Key: "extra", Title: "Extra", Questions: []domain.VendorQuestion{
		{ID: "lorry_number", Kind: domain.VendorQuestionText, Title: "Lorry number", Required: true},
	}})
	return dsl
}

func (s *feedFormSource) PublishedFeedPurchaseForm(_ context.Context, _ string, catalog []domain.VendorCatalogEntry) (domain.VendorForm, error) {
	return domain.CompileVendorForm(withExtra(), 2, catalog), nil
}

func (s *feedFormSource) FeedPurchaseFormVersion(_ context.Context, _ string, version int, catalog []domain.VendorCatalogEntry) (domain.VendorForm, error) {
	s.asked = append(s.asked, version)
	if version != 2 {
		return domain.VendorForm{}, ports.ErrVendorFormVersionUnknown
	}
	return domain.CompileVendorForm(withExtra(), 2, catalog), nil
}

func formService(repo *formRepo, src ports.FeedPurchaseFormSource) *FeedPurchaseService {
	svc := NewFeedPurchaseServiceWithClock(repo, func() time.Time { return time.Date(2026, 9, 20, 9, 0, 0, 0, time.UTC) })
	if src != nil {
		svc = svc.WithFormSource(src)
	}
	return svc
}

// TestFeedPurchaseFormFillsTheLedgerAndKeepsTheRest is the whole contract of authoring the entry
// form: the typed answers land in the ledger's own columns, the authored one is kept beside them
// with the version it was answered on, and the write is judged against THAT version.
func TestFeedPurchaseFormFillsTheLedgerAndKeepsTheRest(t *testing.T) {
	repo := &formRepo{}
	src := &feedFormSource{}
	svc := formService(repo, src)

	_, err := svc.CreateFeedPurchase(context.Background(), "t1", domain.FeedPurchaseWrite{
		QuestionnaireVersion: 2,
		SOPAnswers: map[string]string{
			"purchase_date":   "2026-09-18",
			"farm_label":      "CBE",
			"feed_item_label": "Maize",
			"quantity_kg":     "2000",
			"vendor":          "Agri Traders",
			"payment_status":  "Pending",
			"lorry_number":    "KA 05 AB 1234",
		},
	}, "actor", "idem-1")
	if err != nil {
		t.Fatal(err)
	}
	if repo.calls != 1 {
		t.Fatalf("create calls = %d", repo.calls)
	}
	if repo.created.FarmLabel != "CBE" || repo.created.FeedItemLabel != "Maize" || repo.created.QuantityKg != 2000 {
		t.Fatalf("typed answers did not reach the ledger: %+v", repo.created)
	}
	if repo.created.SOPAnswers["lorry_number"] != "KA 05 AB 1234" {
		t.Fatalf("authored answer lost: %v", repo.created.SOPAnswers)
	}
	if _, typed := repo.created.SOPAnswers["quantity_kg"]; typed {
		t.Fatal("a typed answer must not also be stored as an extra")
	}
	if repo.created.QuestionnaireVersion != 2 {
		t.Fatalf("version stamped = %d", repo.created.QuestionnaireVersion)
	}
	if len(src.asked) != 1 || src.asked[0] != 2 {
		t.Fatalf("service resolved versions %v, want the one the client rendered", src.asked)
	}
}

// TestFeedPurchaseFormRefusesWhatTheFormDoesNotAllow: a compulsory authored question left blank,
// an answer to a question the form does not carry, and a version the library never published are
// each refused BEFORE anything is written -- a load is money, and a half-checked one is worse
// than a rejected one.
func TestFeedPurchaseFormRefusesWhatTheFormDoesNotAllow(t *testing.T) {
	valid := map[string]string{
		"purchase_date": "2026-09-18", "farm_label": "CBE", "feed_item_label": "Maize",
		"quantity_kg": "2000", "vendor": "Agri Traders", "payment_status": "Pending", "lorry_number": "KA 05 AB 1234",
	}
	clone := func(mutate func(map[string]string)) map[string]string {
		out := map[string]string{}
		for k, v := range valid {
			out[k] = v
		}
		mutate(out)
		return out
	}

	cases := map[string]struct {
		version int
		answers map[string]string
		wantErr error
	}{
		"compulsory authored question blank": {2, clone(func(m map[string]string) { m["lorry_number"] = "" }), nil},
		"answer the form does not carry":     {2, clone(func(m map[string]string) { m["nonsense"] = "x" }), nil},
		"choice outside the catalog":         {2, clone(func(m map[string]string) { m["feed_item_label"] = "Gold" }), nil},
		"version never published":            {9, valid, ports.ErrVendorFormVersionUnknown},
		"no version at all":                  {0, valid, ErrFeedPurchaseFormVersionRequired},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			repo := &formRepo{}
			svc := formService(repo, &feedFormSource{})
			_, err := svc.CreateFeedPurchase(context.Background(), "t1", domain.FeedPurchaseWrite{
				QuestionnaireVersion: tc.version, SOPAnswers: tc.answers,
			}, "actor", "idem-1")
			if err == nil {
				t.Fatal("expected the write to be refused")
			}
			if tc.wantErr != nil && !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if repo.calls != 0 {
				t.Fatal("a refused write reached the ledger")
			}
		})
	}
}

// TestAnOlderClientWithNoAnswersStillRecords: the form is additive. A client that sends typed
// fields only -- an APK from before the form existed -- is accepted exactly as it was.
func TestAnOlderClientWithNoAnswersStillRecords(t *testing.T) {
	repo := &formRepo{}
	svc := formService(repo, &feedFormSource{})
	_, err := svc.CreateFeedPurchase(context.Background(), "t1", domain.FeedPurchaseWrite{
		PurchaseDate: "2026-09-18", FarmLabel: "CBE", FeedItemLabel: "Maize", QuantityKg: 2000, Vendor: "Agri Traders", PaymentStatus: "Pending",
	}, "actor", "idem-1")
	if err != nil {
		t.Fatal(err)
	}
	if repo.calls != 1 {
		t.Fatalf("create calls = %d", repo.calls)
	}
	if repo.created.QuestionnaireVersion != 0 || repo.created.SOPAnswers != nil {
		t.Fatalf("a typed-only write must not invent a form version: %+v", repo.created)
	}
}

type feedReadbackRepo struct {
	fakeFeedPurchaseRepo
	purchases []domain.FeedPurchase
}

func (r *feedReadbackRepo) ListFeedPurchases(context.Context, string, string, string, int, int) (ports.FeedPurchasePage, error) {
	return ports.FeedPurchasePage{Purchases: r.purchases}, nil
}
func TestFeedPurchaseReadbackUsesOriginalVersionOncePerPage(t *testing.T) {
	repo := &feedReadbackRepo{purchases: []domain.FeedPurchase{
		{QuestionnaireVersion: 2, SOPAnswers: map[string]string{"lorry_number": "TN42"}},
		{QuestionnaireVersion: 2, SOPAnswers: map[string]string{"lorry_number": "TN43"}},
		{QuestionnaireVersion: 99, SOPAnswers: map[string]string{"retired_question": "kept"}},
	}}
	src := &feedFormSource{}
	page, err := NewFeedPurchaseService(repo).WithFormSource(src).ListFeedPurchases(context.Background(), "tenant", FeedPurchaseListQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if len(src.asked) != 2 || src.asked[0] != 2 || src.asked[1] != 99 {
		t.Fatalf("historical lookup must be bounded by distinct versions: %v", src.asked)
	}
	for i, expected := range []string{"TN42", "TN43", "kept"} {
		rows := page.Purchases[i].AnswerRows
		if len(rows) != 1 || rows[0].Value != expected {
			t.Fatalf("purchase %d lost answers: %+v", i, rows)
		}
	}
	if page.Purchases[0].AnswerRows[0].Label != "Lorry number" {
		t.Fatalf("missing historical label: %+v", page.Purchases[0].AnswerRows)
	}
}
