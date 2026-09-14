package http

import (
	"time"

	"github.com/vgoats/goatos/backend/internal/animalpurchase/domain"
	"github.com/vgoats/goatos/backend/internal/animalpurchase/ports"
)

type countsPayload struct {
	Total    int `json:"total"`
	Pending  int `json:"pending"`
	Accepted int `json:"accepted"`
	Rejected int `json:"rejected"`
}

type loadPayload struct {
	LoadID        string        `json:"load_id"`
	LoadRef       string        `json:"load_ref"`
	Title         string        `json:"title"`
	VendorID      string        `json:"vendor_id"`
	VendorName    string        `json:"vendor_name"`
	Farm          string        `json:"farm"`
	ExpectedCount int           `json:"expected_count"`
	Notes         string        `json:"notes"`
	Status        string        `json:"status"`
	Counts        countsPayload `json:"counts"`
	// Summary is the backend-owned one-liner under the title: "12 animals · 3 awaiting decision".
	Summary    string    `json:"summary"`
	RecordedBy string    `json:"recorded_by,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
	RowVersion int       `json:"row_version"`
	// The load form's answers as recorded and the SOP version they were answered on; the answer
	// rows render the extra questions under their own text (identity questions are the typed
	// fields above).
	QuestionnaireVersion int                `json:"questionnaire_version"`
	Answers              domain.Answers     `json:"answers"`
	AnswerRows           []answerRowPayload `json:"answer_rows"`
}

type loadPagePayload struct {
	Loads      []loadPayload `json:"loads"`
	NextCursor string        `json:"next_cursor,omitempty"`
	// CanRecord says whether THIS caller may add loads and animals (the write permission,
	// resolved per request from the caller's own grants). The read routes are open to the CEO,
	// who watches but never records, so the phone must be told rather than offer a form the
	// write route then refuses.
	CanRecord bool `json:"can_record"`
}

type candidatePayload struct {
	CandidateID    string   `json:"candidate_id"`
	LoadID         string   `json:"load_id"`
	LoadRef        string   `json:"load_ref"`
	SeqNo          int      `json:"seq_no"`
	Title          string   `json:"title"`
	Species        string   `json:"species"`
	SpeciesLabel   string   `json:"species_label"`
	Sex            string   `json:"sex"`
	SexLabel       string   `json:"sex_label"`
	Breed          string   `json:"breed"`
	AgeMonths      *int     `json:"age_months,omitempty"`
	WeightKg       *float64 `json:"weight_kg,omitempty"`
	Condition      string   `json:"condition"`
	ConditionLabel string   `json:"condition_label"`
	TempTag        string   `json:"temp_tag"`
	Notes          string   `json:"notes"`
	VideoProofRef  string   `json:"video_proof_ref"`
	MediaURL       string   `json:"media_url,omitempty"`
	MediaMime      string   `json:"media_mime,omitempty"`
	// The SOP questionnaire (2026-09-13): the answers as recorded, the same answers rendered
	// for display under their question text, the captures per slot with stable backend download routes,
	// and the inspector's own verdict chip.
	QuestionnaireVersion int                `json:"questionnaire_version"`
	Answers              domain.Answers     `json:"answers"`
	AnswerRows           []answerRowPayload `json:"answer_rows"`
	MediaSlots           []mediaSlotPayload `json:"media_slots"`
	FieldVerdict         string             `json:"field_verdict"`
	FieldVerdictLabel    string             `json:"field_verdict_label"`
	HeightCm             *float64           `json:"height_cm,omitempty"`
	RectalTempC          *float64           `json:"rectal_temp_c,omitempty"`
	Decision             string             `json:"decision"`
	DecisionLabel        string             `json:"decision_label"`
	DecisionTone         string             `json:"decision_tone"`
	DecidedByName        string             `json:"decided_by_name"`
	DecidedAt            *time.Time         `json:"decided_at,omitempty"`
	DecisionNote         string             `json:"decision_note"`
	RecordedBy           string             `json:"recorded_by,omitempty"`
	CreatedAt            time.Time          `json:"created_at"`
	UpdatedAt            time.Time          `json:"updated_at"`
	RowVersion           int                `json:"row_version"`
}

// answerRowPayload is one answered question rendered for a screen: the section it sits in,
// the question text, and the answer label. Unanswered non-required questions are omitted.
type answerRowPayload struct {
	Section    string `json:"section"`
	QuestionID string `json:"question_id"`
	Question   string `json:"question"`
	Answer     string `json:"answer"`
	// Attention flags an answer the SOP treats as a reject signal (a "yes" to a problem, an
	// "other" area, a low teeth count), so the reviewer's eye lands on it.
	Attention bool `json:"attention"`
}

// mediaSlotPayload is one media question's captures with stable backend download routes.
type mediaSlotPayload struct {
	Slot  string             `json:"slot"`
	Title string             `json:"title"`
	Items []mediaItemPayload `json:"items"`
}

type mediaItemPayload struct {
	ProofRef  string `json:"proof_ref"`
	MediaURL  string `json:"media_url,omitempty"`
	MediaMime string `json:"media_mime,omitempty"`
}

type candidatePagePayload struct {
	Animals    []candidatePayload `json:"animals"`
	NextCursor string             `json:"next_cursor,omitempty"`
	// Counts are WHOLE-FILTER decision counts, never page sums.
	Counts countsPayload `json:"counts"`
}

type loadDetailPayload struct {
	Load       loadPayload        `json:"load"`
	Animals    []candidatePayload `json:"animals"`
	NextCursor string             `json:"next_cursor,omitempty"`
	CanRecord  bool               `json:"can_record"`
}

type reviewPagePayload struct {
	Animals    []candidatePayload `json:"animals"`
	NextCursor string             `json:"next_cursor,omitempty"`
	Counts     countsPayload      `json:"counts"`
	// Filters are the review page's decision chips, backend-owned labels with whole-filter counts.
	Filters []filterPayload `json:"filters"`
}

type filterPayload struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Count    int    `json:"count"`
	Selected bool   `json:"selected"`
}

type optionsPayload struct {
	Species          []domain.Option `json:"species"`
	Sexes            []domain.Option `json:"sexes"`
	Conditions       []domain.Option `json:"conditions"`
	Farms            []domain.Option `json:"farms"`
	BreedSuggestions []string        `json:"breed_suggestions"`
	// The SOP questionnaire the phone renders in order, and its version stamped on answers.
	Questionnaire []domain.Question `json:"questionnaire"`
	// LoadForm is the load's own questions of the same version (PROCUREMENT SOP).
	LoadForm             []domain.Question `json:"load_form"`
	QuestionnaireVersion int               `json:"questionnaire_version"`
	// Copy the phone form renders verbatim.
	Copy map[string]string `json:"copy"`
}

type createLoadBody struct {
	LoadRef       string `json:"load_ref"`
	VendorID      string `json:"vendor_id"`
	Farm          string `json:"farm"`
	ExpectedCount int    `json:"expected_count"`
	Notes         string `json:"notes"`
	// PROCUREMENT SOP: the load form's answers by question id (the identity questions mirror the
	// typed fields above; extra authored questions live only here) and the SOP version rendered.
	QuestionnaireVersion int            `json:"questionnaire_version"`
	Answers              domain.Answers `json:"answers"`
}

type addAnimalBody struct {
	// The SOP version the phone rendered (from /options); 0 = the published one. The write is
	// validated against THAT version, so a publish never breaks a form already open.
	QuestionnaireVersion int `json:"questionnaire_version"`
	// {question_id: answer}, per the questionnaire served on /options.
	Answers domain.Answers `json:"answers"`
	// {slot: [proof refs in position order]}.
	Media domain.MediaRefs `json:"media"`
}

type decisionBody struct {
	Decision   string `json:"decision"`
	Note       string `json:"note"`
	RowVersion int    `json:"row_version"`
}

func toCounts(c domain.DecisionCounts) countsPayload {
	return countsPayload{Total: c.Total, Pending: c.Pending, Accepted: c.Accepted, Rejected: c.Rejected}
}

func toLoadPayload(l domain.Load, cat domain.Catalog) loadPayload {
	answers := l.Answers
	if answers == nil {
		answers = domain.Answers{}
	}
	return loadPayload{
		LoadID: l.LoadID, LoadRef: l.LoadRef, Title: domain.LoadTitle(l), VendorID: l.VendorID, VendorName: l.VendorName,
		Farm: l.FarmLabel, ExpectedCount: l.ExpectedCount, Notes: l.Notes, Status: l.Status, Counts: toCounts(l.Counts),
		Summary: loadSummary(l), RecordedBy: l.RecordedBy, CreatedAt: l.CreatedAt, UpdatedAt: l.UpdatedAt, RowVersion: l.RowVersion,
		QuestionnaireVersion: l.QuestionnaireVersion, Answers: answers, AnswerRows: loadAnswerRows(l, cat),
	}
}

// loadAnswerRows renders the load's EXTRA answers under their question text (the identity
// questions are typed fields the screens already show).
func loadAnswerRows(l domain.Load, cat domain.Catalog) []answerRowPayload {
	out := []answerRowPayload{}
	if l.QuestionnaireVersion == 0 || l.Answers == nil {
		return out
	}
	for _, q := range cat.LoadQuestions {
		switch q.ID {
		case "load_ref", "vendor", "farm", "expected_count", "notes":
			continue
		}
		if q.Kind == domain.KindVendor || !l.Answers.Applies(q) {
			continue
		}
		label := domain.AnswerLabel(q, l.Answers)
		if label == "" {
			continue
		}
		out = append(out, answerRowPayload{QuestionID: q.ID, Question: q.Title, Answer: label})
	}
	return out
}

// toCandidatePayload renders one row; cat is the SOP version the row was answered on (an empty
// catalog leaves the answers unlabelled rather than mislabelling them with another version).
func toCandidatePayload(c domain.Candidate, media map[string]ports.Media, cat domain.Catalog) candidatePayload {
	// Legacy single video (rows recorded before the questionnaire) keeps its top-level route.
	var mediaURL, mediaMime string
	if m, ok := media[c.VideoProofRef]; ok {
		mediaURL, mediaMime = m.URL, m.MimeType
	}
	slots := make([]mediaSlotPayload, 0, len(cat.MediaSlots()))
	for _, q := range cat.MediaSlots() {
		refs := c.Media[q.Slot]
		if len(refs) == 0 {
			continue
		}
		items := make([]mediaItemPayload, 0, len(refs))
		for _, ref := range refs {
			m := media[ref]
			items = append(items, mediaItemPayload{ProofRef: ref, MediaURL: m.URL, MediaMime: m.MimeType})
		}
		slots = append(slots, mediaSlotPayload{Slot: q.Slot, Title: q.Title, Items: items})
	}
	answers := c.Answers
	if answers == nil {
		answers = domain.Answers{}
	}
	return candidatePayload{
		QuestionnaireVersion: c.QuestionnaireVersion, Answers: answers, AnswerRows: answerRows(c, cat), MediaSlots: slots,
		FieldVerdict: c.FieldVerdict, FieldVerdictLabel: domain.FieldVerdictLabel(c.FieldVerdict),
		HeightCm: c.HeightCm, RectalTempC: c.RectalTempC,
		CandidateID: c.CandidateID, LoadID: c.LoadID, LoadRef: c.LoadRef, SeqNo: c.SeqNo, Title: domain.CandidateTitle(c),
		Species: c.Species, SpeciesLabel: domain.SpeciesLabel(c.Species), Sex: c.Sex, SexLabel: domain.SexLabel(c.Sex),
		Breed: c.Breed, AgeMonths: c.AgeMonths, WeightKg: c.WeightKg,
		Condition: c.Condition, ConditionLabel: domain.ConditionLabel(c.Condition), TempTag: c.TempTag, Notes: c.Notes,
		VideoProofRef: c.VideoProofRef, MediaURL: mediaURL, MediaMime: mediaMime,
		Decision: c.Decision, DecisionLabel: domain.DecisionLabel(c.Decision), DecisionTone: domain.DecisionTone(c.Decision),
		DecidedByName: c.DecidedByName, DecidedAt: c.DecidedAt, DecisionNote: c.DecisionNote, RecordedBy: c.RecordedBy,
		CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt, RowVersion: c.RowVersion,
	}
}

// answerRows renders the recorded answers under their questions, in SOP order, skipping
// unanswered optional questions, section rows and media (media has its own slots).
func answerRows(c domain.Candidate, cat domain.Catalog) []answerRowPayload {
	if c.QuestionnaireVersion == 0 || c.Answers == nil {
		return []answerRowPayload{}
	}
	out := make([]answerRowPayload, 0, 40)
	section := ""
	for _, q := range cat.Questions {
		switch q.Kind {
		case domain.KindSection:
			section = q.Title
			continue
		case domain.KindMedia:
			continue
		}
		if !c.Answers.Applies(q) {
			continue
		}
		label := domain.AnswerLabel(q, c.Answers)
		if label == "" {
			continue
		}
		out = append(out, answerRowPayload{
			Section: section, QuestionID: q.ID, Question: q.Title, Answer: label, Attention: attention(q, c.Answers),
		})
	}
	return out
}

// attention marks the answers the SOP reads as reject signals.
func attention(q domain.Question, a domain.Answers) bool {
	v := a.Choice(q.ID)
	switch q.ID {
	case "well_fed":
		return v == "no"
	case "teeth":
		n := a.Number(q.ID)
		return n != nil && *n == 0
	case "anaemic", "mouth_breathing", "acidosis", "diarrhea", "teat_discharge":
		return v == "yes"
	case "watery_eyes", "eye_colour", "nasal_discharge":
		return v != "" && v != "no"
	case "face_scabs", "ticks_hair_loss", "wounds", "body_scabs", "lumps", "arthritis":
		return v == "other"
	case "mastitis":
		return v == "positive"
	case "field_verdict":
		return v == domain.FieldVerdictOnHold
	case "udder_state":
		vals, _ := a.Multi(q.ID)
		for _, x := range vals {
			if x != "normal" {
				return true
			}
		}
	}
	return false
}
