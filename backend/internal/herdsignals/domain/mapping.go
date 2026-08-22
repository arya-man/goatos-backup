package domain

import (
	"errors"
	"time"
)

// ErrMappingNotFound is the "the thing you named does not exist in your tenant" failure (an unknown
// goat_id or identifier_id), mapped to 404 by the HTTP layer. Kept separate from ErrValidation
// so a caller can tell "you sent nonsense" apart from "you sent a well-formed id for something
// that is not here".
var ErrMappingNotFound = errors.New("herdsignals: not found")

// ErrMappingConflict is the "this write would create a state the module forbids" failure, mapped to
// 409. The three states it guards are the whole point of the mapping writes:
//
//  1. a BLE tag value already claimed by a DIFFERENT animal,
//  2. an animal that would end up carrying TWO live smart tags at once,
//  3. a REPLACE on an animal that has no live smart tag to replace.
//
// Re-tagging is the real-world case (a tag falls off, a replacement goes on) and it must never
// leave an animal with two live tags or none -- so those states are refused loudly here rather
// than silently reconciled later by a read path.
var ErrMappingConflict = errors.New("herdsignals: conflict")

// SmartTagIdentifierTypes are the goat_identifiers.identifier_type values a smart tag may be
// bound to. Mirrors the goat_identifiers_type_check constraint (000001 + 000022); a value
// outside this set is rejected in Go before it reaches the database so the caller gets a 400
// naming the allowed set rather than a constraint-violation 500.
var SmartTagIdentifierTypes = []string{"animal_identifier_1", "animal_identifier_2", "temporary_tag"}

// DefaultSmartTagIdentifierType is the slot a BLE tag lands in when the caller does not pin one.
// animal_identifier_2 rather than _1: an animal's primary identity is its existing ear tag, and
// a smart tag is normally the SECOND thing it carries.
const DefaultSmartTagIdentifierType = "animal_identifier_2"

// SmartTagNormalizerVersion is the normalizer contract stamped on rows this module writes. It
// names the identity module's canonical normalizer (strings.ToUpper(strings.TrimSpace(v)), see
// backend/internal/identity/app/service.go), which is exactly what NormalizeTagIdentifier
// applies -- the column is NOT NULL and every existing row in the tenant carries this value.
const SmartTagNormalizerVersion = "identifier_normalizer_v1"

// SmartTagScopeKey is the scope every existing goat_identifiers row uses ('global'). The column
// is NOT NULL with a length > 0 check, so this is not optional.
const SmartTagScopeKey = "global"

// BindTagMappingRequest is the payload for POST /herd-signals/tag-mappings: bind a BLE tag to an
// animal. TagMAC is optional and, when present and different from TagID, gets its OWN identifier
// row -- the read path matches a packet by tag_id OR tag_mac, and a single identifier row can
// only carry one normalized_value, so a tag that reports both needs both claimed or half its
// packets would resolve to nothing.
type BindTagMappingRequest struct {
	GoatID         string `json:"goat_id"`
	TagID          string `json:"tag_id"`
	TagMAC         string `json:"tag_mac"`
	IdentifierType string `json:"identifier_type"`
}

// ReplaceTagMappingRequest is the payload for POST /herd-signals/tag-mappings/replace: unbind
// the animal's current smart tag and bind a new one, in ONE transaction.
type ReplaceTagMappingRequest struct {
	GoatID         string `json:"goat_id"`
	NewTagID       string `json:"new_tag_id"`
	NewTagMAC      string `json:"new_tag_mac"`
	IdentifierType string `json:"identifier_type"`
}

// SetSmartTagCapableRequest is the payload for POST
// /herd-signals/identifiers/{identifier_id}/smart-tag: mark or unmark an EXISTING identifier as
// smart-tag capable, for the case where the animal's ear tag value IS the BLE tag value and no
// new identifier row should be invented.
type SetSmartTagCapableRequest struct {
	SmartTagCapable bool `json:"smart_tag_capable"`
}

// TagMappingResponse is the shared response of all three mapping writes.
//
// MonitoringSince is the load-bearing field: it is the instant animal monitoring STARTS for this
// tag (goat_identifiers.smart_tag_mapped_at, denormalised onto
// herd_signal_tag_latest.animal_monitoring_since). Null means the tag is unmapped, in which case
// no animal-attributed value may be produced for it at all -- not a zero, not a default. On a
// REPLACE this is a NEW instant for the new tag: the old tag's monitoring period ended and the
// new tag's begins now, so nothing the old tag emitted, and nothing the new tag emitted while it
// sat on a bench, can reach this animal's baseline.
type TagMappingResponse struct {
	GoatID          string     `json:"goat_id"`
	TagID           string     `json:"tag_id"`
	TagMAC          *string    `json:"tag_mac"`
	IdentifierIDs   []string   `json:"identifier_ids"`
	MappingState    string     `json:"mapping_state"`
	MonitoringSince *time.Time `json:"monitoring_since"`
	// UnboundIdentifierIDs are the identifiers whose smart-tag binding this write ENDED: the
	// replaced tag on a REPLACE, or the unmarked identifier on an unmark. Always present (never
	// null) so a client can render "what changed" without a second read.
	UnboundIdentifierIDs []string `json:"unbound_identifier_ids"`
}

// GatewayHeartbeatRequest is the payload for POST /herd-signals/heartbeats. The gateway emits
// pkt_type:"state" with data.state "sta_gw_hb" and a ticks_cnt about every 5 minutes and carries
// no device rows at all; these were previously DISCARDED, which made "gateway up but hearing no
// tags" indistinguishable from "gateway down".
type GatewayHeartbeatRequest struct {
	GatewayID string `json:"gateway_id"`
	// State is the gateway's own state name. Optional; when present it must be the heartbeat
	// state, so a future non-heartbeat state message is refused rather than silently counted as
	// proof of life.
	State string `json:"state"`
	// TicksCnt is the gateway's uptime tick counter. A value that goes BACKWARDS is a reboot,
	// handled exactly like a decreasing pkt_sn or a reset motion_count: re-anchor and count the
	// reboot, never record a negative.
	TicksCnt *int64 `json:"ticks_cnt"`
}

// GatewayHeartbeatState is the only data.state value POST /herd-signals/heartbeats accepts.
const GatewayHeartbeatState = "sta_gw_hb"

// GatewayHeartbeatResponse reports what the heartbeat did to the gateway's health record.
type GatewayHeartbeatResponse struct {
	GatewayID       string    `json:"gateway_id"`
	LastHeartbeatAt time.Time `json:"last_heartbeat_at"`
	// RebootDetected is true when ticks_cnt went backwards against the stored value.
	RebootDetected bool   `json:"reboot_detected"`
	TraceID        string `json:"trace_id"`
}
