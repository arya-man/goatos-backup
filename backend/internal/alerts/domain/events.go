package domain

import (
	"errors"
	"fmt"
	"strings"
)

// EventKind names a durable business event the farm can be alerted on. The catalog is code --
// each kind is one park-day SQL reader over the module that owns the fact -- but COMPOSING an
// alert from a kind is config (maintainer request 2026-09-16: "if a birth happens I want to add
// it as an alert, and anything like that in future"): the Configure drawer offers every kind
// here, and a rule row names one of them with a label, a severity and a switch. Adding a kind is
// one Rule entry plus one reader; adding an alert needs no release at all.
type EventKind string

const (
	EventBirthRecorded        EventKind = "birth_recorded"
	EventDeathRecorded        EventKind = "death_recorded"
	EventAnimalSold           EventKind = "animal_sold"
	EventAnimalAdded          EventKind = "animal_added"
	EventShiftingRaised       EventKind = "shifting_raised"
	EventShiftingApproved     EventKind = "shifting_approved"
	EventFeedPurchaseRecorded EventKind = "feed_purchase_recorded"
)

// EventKindInfo is the catalog entry the drawer renders. Copy is backend-owned.
type EventKindInfo struct {
	Key         EventKind `json:"key"`
	Label       string    `json:"label"`
	Description string    `json:"description"`
}

// EventKinds is the catalog, in drawer order.
func EventKinds() []EventKindInfo {
	return []EventKindInfo{
		{EventBirthRecorded, "Birth recorded", "A kid was recorded born in the park, with its pen and mother."},
		{EventDeathRecorded, "Death recorded", "An animal in the park was recorded dead."},
		{EventAnimalSold, "Animal sold", "An animal left the park as sold."},
		{EventAnimalAdded, "Animal added", "An animal joined the register in the park other than by birth (purchase, import, tagging)."},
		{EventShiftingRaised, "Shifting raised", "A pen movement was raised for the park and is awaiting approval."},
		{EventShiftingApproved, "Shifting approved", "A park head approved a pen movement for the park."},
		{EventFeedPurchaseRecorded, "Feed purchase recorded", "A feed load was recorded bought for the park."},
	}
}

// EventKindByKey looks a catalog kind up.
func EventKindByKey(key EventKind) (EventKindInfo, bool) {
	for _, k := range EventKinds() {
		if k.Key == key {
			return k, true
		}
	}
	return EventKindInfo{}, false
}

// EventRule is one user-defined alert: "tell me when <kind> happens", with the farm's own
// wording for it.
type EventRule struct {
	ID        string    `json:"id"`
	Label     string    `json:"label"`
	Kind      EventKind `json:"kind"`
	KindLabel string    `json:"kind_label"`
	Severity  Severity  `json:"severity"`
	Enabled   bool      `json:"enabled"`
	// CreatedBy / UpdatedBy are NAMES, never ids; UpdatedAt is a farm-readable IST label.
	CreatedBy string `json:"created_by,omitempty"`
	UpdatedBy string `json:"updated_by,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

// SetEventRule is the write for one event rule; a blank ID creates.
type SetEventRule struct {
	TenantID       string
	ActorID        string
	ID             string
	Label          string
	Kind           EventKind
	Severity       Severity
	Enabled        bool
	IdempotencyKey string
}

var (
	// ErrUnknownEventKind is a rule naming a kind the catalog does not carry.
	ErrUnknownEventKind = errors.New("alerts: unknown event kind")
	// ErrInvalidEventRule is a rule with no label or an unknown severity.
	ErrInvalidEventRule = errors.New("alerts: invalid event rule")
	// ErrEventRuleNotFound is an update or delete for a rule this tenant does not have.
	ErrEventRuleNotFound = errors.New("alerts: event rule not found")
)

// MaxEventRuleLabel bounds the farm's wording for a rule.
const MaxEventRuleLabel = 80

// Validate refuses a write the catalog cannot honour.
func (in SetEventRule) Validate() error {
	if _, ok := EventKindByKey(in.Kind); !ok {
		return ErrUnknownEventKind
	}
	label := strings.TrimSpace(in.Label)
	if label == "" || len(label) > MaxEventRuleLabel {
		return ErrInvalidEventRule
	}
	if in.Severity != SeverityCritical && in.Severity != SeverityWarning {
		return ErrInvalidEventRule
	}
	return nil
}

// Event is one durable business event a reader hands back for a park-day, already worded.
type Event struct {
	// Key is stable per event (the owning row's id), so a client can key rows.
	Key            string
	ParkID         string
	ShedID         string
	ShedName       string
	PartitionLabel string
	// Subject names the thing (an animal by its tag, a load by its feed, a movement by its
	// pens); Detail is the sentence under it. Both are composed by the reader.
	Subject string
	Detail  string
	// Href is where the row opens.
	Href string
}

// MaxEventRowsPerRule bounds one rule's rows per park-day; beyond it one summary row says how
// many more there were, so a bulk day (35 animals sold) cannot bury every other alert.
const MaxEventRowsPerRule = 25

// DetectEvents turns one rule's park-day events into alert rows.
func DetectEvents(businessDate string, parkLabel string, rule EventRule, events []Event) []Alert {
	out := make([]Alert, 0, len(events)+1)
	for i, e := range events {
		if i >= MaxEventRowsPerRule {
			out = append(out, Alert{
				Key:          fmt.Sprintf("event:%s:%s:%s:more", rule.ID, businessDate, e.ParkID),
				RuleKey:      RuleKey("event:" + rule.ID),
				RuleLabel:    rule.Label,
				Severity:     rule.Severity,
				Title:        fmt.Sprintf("%s: %d more today", rule.Label, len(events)-MaxEventRowsPerRule),
				Detail:       fmt.Sprintf("Only the first %d are listed. Open the module for the full day.", MaxEventRowsPerRule),
				ParkID:       e.ParkID,
				ParkLabel:    parkLabel,
				BusinessDate: businessDate,
				Href:         e.Href,
			})
			break
		}
		loc := ""
		if e.ShedName != "" {
			loc = penDisplay(e.ShedName, e.PartitionLabel)
		}
		out = append(out, Alert{
			Key:                        fmt.Sprintf("event:%s:%s:%s:%s", rule.ID, businessDate, e.ParkID, e.Key),
			RuleKey:                    RuleKey("event:" + rule.ID),
			RuleLabel:                  rule.Label,
			Severity:                   rule.Severity,
			Title:                      fmt.Sprintf("%s: %s", rule.Label, e.Subject),
			Detail:                     e.Detail,
			ParkID:                     e.ParkID,
			ParkLabel:                  parkLabel,
			ShedID:                     e.ShedID,
			ShedName:                   e.ShedName,
			PartitionLabel:             partitionForWire(e.PartitionLabel),
			OperationalLocationDisplay: loc,
			BusinessDate:               businessDate,
			Href:                       e.Href,
		})
	}
	return out
}
