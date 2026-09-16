// Package domain holds the Alerts rule catalog, its configuration shape, the alert row
// every surface renders, and the pure detectors that turn frozen module rows into alerts.
package domain

import (
	"errors"
	"sort"
	"strings"
)

// RuleKey names one alert rule. Keys are config vocabulary and never user copy: every
// surface renders Rule.Label, and the copy firewall applies.
type RuleKey string

const (
	// RulePenFeedQuantityChange fires when a pen's feed sheet for the business day did NOT
	// follow its head count against the previous day (maintainer clarification 2026-09-16):
	// the count moved but the kg stayed, or the kg moved but the count stayed. Count and kg
	// moving together is the sheet doing its job and is silent -- whether a shifting was
	// recorded for the move is secondary and appears only as a note. Threshold: the minimum
	// head-count change that counts.
	RulePenFeedQuantityChange RuleKey = "pen_feed_quantity_change"
	// RuleFeedLowStock fires for every (farm, feed) whose stock lasts fewer than the
	// configured days at its recent daily draw. Threshold: days. The default is the Stock
	// tab's red card (feeddirection/domain.LowStockDays = 5).
	RuleFeedLowStock RuleKey = "feed_low_stock"
)

// Severity is the row's tone. Backend-owned so every surface colours the same alert the
// same way.
type Severity string

const (
	SeverityCritical Severity = "critical"
	SeverityWarning  Severity = "warning"
)

// Rule is one catalog entry: what the rule means and the one numeric knob it carries.
type Rule struct {
	Key         RuleKey `json:"key"`
	Label       string  `json:"label"`
	Description string  `json:"description"`
	// ThresholdLabel and ThresholdUnit name the knob for the Configure drawer; the client
	// renders them verbatim.
	ThresholdLabel   string `json:"threshold_label"`
	ThresholdUnit    string `json:"threshold_unit"`
	DefaultThreshold int    `json:"default_threshold"`
	MinThreshold     int    `json:"min_threshold"`
	MaxThreshold     int    `json:"max_threshold"`
	DefaultEnabled   bool   `json:"default_enabled"`
}

// Rules is the catalog, in the order the Configure drawer lists them. Adding a rule here
// is HALF of adding an alert; the other half is a detector the service runs for it.
func Rules() []Rule {
	return []Rule{
		{
			Key:              RulePenFeedQuantityChange,
			Label:            "Pen feed did not follow the head count",
			Description:      "Against yesterday's sheet, a pen's head count moved but its feed did not, or its feed moved while the head count stayed. Count and feed moving together is normal and stays quiet.",
			ThresholdLabel:   "Minimum head-count change",
			ThresholdUnit:    "animals",
			DefaultThreshold: 1,
			MinThreshold:     1,
			MaxThreshold:     1000,
			DefaultEnabled:   true,
		},
		{
			Key:              RuleFeedLowStock,
			Label:            "Feed stock running out",
			Description:      "A feed's store lasts fewer than the set number of days at its recent daily draw.",
			ThresholdLabel:   "Alert when fewer than",
			ThresholdUnit:    "days left",
			DefaultThreshold: 5,
			MinThreshold:     1,
			MaxThreshold:     90,
			DefaultEnabled:   true,
		},
	}
}

// RuleByKey looks a catalog rule up.
func RuleByKey(key RuleKey) (Rule, bool) {
	for _, r := range Rules() {
		if r.Key == key {
			return r, true
		}
	}
	return Rule{}, false
}

// RuleConfig is one rule's effective setting: the stored row when one exists, the
// catalog default otherwise. Every field a surface renders is here.
type RuleConfig struct {
	Rule
	Enabled   bool `json:"enabled"`
	Threshold int  `json:"threshold"`
	// UpdatedBy is the NAME of the person who last set the rule (never an id) and UpdatedAt a
	// farm-readable Asia/Kolkata label; both blank while the catalog default is in force.
	UpdatedBy string `json:"updated_by,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
	// Stored says a row exists for this rule; false means the catalog default is in force.
	Stored bool `json:"stored"`
}

// StoredRuleConfig is the shape the config store hands back for one row.
type StoredRuleConfig struct {
	Key       RuleKey
	Enabled   bool
	Threshold int
	UpdatedBy string
	UpdatedAt string
}

// EffectiveConfig merges stored rows onto the catalog. A stored row for an unknown key is
// dropped -- config never invents a detector -- and a stored threshold outside the rule's
// range is clamped rather than run at a value the catalog refuses.
func EffectiveConfig(stored []StoredRuleConfig) []RuleConfig {
	byKey := map[RuleKey]StoredRuleConfig{}
	for _, s := range stored {
		byKey[s.Key] = s
	}
	out := make([]RuleConfig, 0, len(Rules()))
	for _, r := range Rules() {
		cfg := RuleConfig{Rule: r, Enabled: r.DefaultEnabled, Threshold: r.DefaultThreshold}
		if s, ok := byKey[r.Key]; ok {
			cfg.Enabled = s.Enabled
			cfg.Threshold = clamp(s.Threshold, r.MinThreshold, r.MaxThreshold)
			cfg.UpdatedBy = s.UpdatedBy
			cfg.UpdatedAt = s.UpdatedAt
			cfg.Stored = true
		}
		out = append(out, cfg)
	}
	return out
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// SetRuleConfig is the write: one rule, on or off, at a threshold.
type SetRuleConfig struct {
	TenantID       string
	ActorID        string
	Key            RuleKey
	Enabled        bool
	Threshold      int
	IdempotencyKey string
}

var (
	// ErrUnknownRule is a write naming a rule the catalog does not carry.
	ErrUnknownRule = errors.New("alerts: unknown rule")
	// ErrThresholdOutOfRange is a threshold the rule's range refuses. Validate-or-reject,
	// never silently default: the author is told, not corrected.
	ErrThresholdOutOfRange = errors.New("alerts: threshold out of range")
)

// Validate refuses a write the catalog cannot honour.
func (in SetRuleConfig) Validate() error {
	r, ok := RuleByKey(in.Key)
	if !ok || strings.TrimSpace(string(in.Key)) == "" {
		return ErrUnknownRule
	}
	if in.Threshold < r.MinThreshold || in.Threshold > r.MaxThreshold {
		return ErrThresholdOutOfRange
	}
	return nil
}

// Alert is one row on the page. Grain: one row per (rule, subject) for one business day;
// rows from different rules are disjoint and may be counted together.
type Alert struct {
	// Key is stable for the (rule, subject, day) so a client can key rows and deep-link.
	Key       string   `json:"key"`
	RuleKey   RuleKey  `json:"rule_key"`
	RuleLabel string   `json:"rule_label"`
	Severity  Severity `json:"severity"`
	// Title and Detail are farm sentences composed here, rendered verbatim.
	Title  string `json:"title"`
	Detail string `json:"detail"`
	// Location. ShedID/partition are blank for a farm-grain alert (feed stock).
	ParkID                     string `json:"park_id"`
	ParkLabel                  string `json:"park_label"`
	ShedID                     string `json:"shed_id,omitempty"`
	ShedName                   string `json:"shed_name,omitempty"`
	PartitionLabel             string `json:"partition_label,omitempty"`
	OperationalLocationDisplay string `json:"operational_location_display,omitempty"`
	BusinessDate               string `json:"business_date"`
	// Href is where the row opens: the module page that owns the underlying fact.
	Href string `json:"href,omitempty"`
}

// SortAlerts orders critical first, then by park, location and title, so two reads of the
// same day list the same way.
func SortAlerts(rows []Alert) {
	rank := func(s Severity) int {
		if s == SeverityCritical {
			return 0
		}
		return 1
	}
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if rank(a.Severity) != rank(b.Severity) {
			return rank(a.Severity) < rank(b.Severity)
		}
		if a.ParkLabel != b.ParkLabel {
			return a.ParkLabel < b.ParkLabel
		}
		// Pen rows before farm-grain rows (and before an event rule's "N more" summary).
		if (a.OperationalLocationDisplay == "") != (b.OperationalLocationDisplay == "") {
			return a.OperationalLocationDisplay != ""
		}
		if a.OperationalLocationDisplay != b.OperationalLocationDisplay {
			return a.OperationalLocationDisplay < b.OperationalLocationDisplay
		}
		return a.Title < b.Title
	})
}
