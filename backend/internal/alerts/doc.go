// Package alerts serves the admin-web Alerts page (maintainer decision 2026-09-16): the
// page directly below the Work Board that lists what is OFF today across the parks a
// person may see.
//
// It owns no fact of its own. Every alert is DERIVED, per request, from rows another
// module already froze -- the feed-direction sheet, the shifting register, the feed
// purchase ledger -- and the only thing this package stores is the rule configuration
// (alert_rule_config): which rules run and at what threshold. The rule catalog, and the
// detector behind each rule, live in domain; a config row can switch a known rule on or
// off and move its number, never invent a new detector. Adding a rule is a code change
// in domain.Rules plus one detector, and the Configure drawer picks it up from the
// catalog without a client change.
//
// Two permissions gate it, both halves of the capability-gated lock: alerts.read opens
// the page and the rows; alerts.configure enables the top-right Configure control on the
// page contract AND GET/PUT /alerts/config, so the button appears only for the people
// HRMS ticks and the server refuses the same people it hides it from.
package alerts
