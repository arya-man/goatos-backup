package kernelstages

import (
	"context"
	"sort"
	"testing"
	"time"

	calendarports "github.com/vgoats/goatos/backend/internal/calendar/ports"
	workforcedomain "github.com/vgoats/goatos/backend/internal/workforce/domain"
)

// rosterSeatFake is a roster stand-in that answers BOTH audience reads from ONE set of seats, using
// the SAME vocabulary the real roster seeder writes:
//
//	vaccination_operator_amit  (pc.vaccination, duty 'execute')  <- the person who runs the drive
//	park_head                  (pc.vaccination, duty 'manage')
//	preventive_care_manager    (pc.vaccination, duty 'manage')
//
// Critically, there is NO seat called "operator" and none called "phc_manager" -- those two codes
// appear nowhere in the seeder, which is exactly why the old hardcoded audience list resolved to the
// park head alone.
type rosterSeatFake struct {
	// seat -> the one device that seat's holder carries.
	parkSeats   map[string]workforcedomain.NotificationRecipient
	parkDuties  map[string][]string // seat -> duty types it holds for pc.vaccination
	tenantSeats map[string]workforcedomain.NotificationRecipient

	moduleCalls int
}

func (f *rosterSeatFake) ResolveModuleDutyRecipientsBatch(_ context.Context, _, scopeType string, scopeIDs []string, moduleCode string, dutyTypes []string, _ time.Time) (map[string][]workforcedomain.NotificationRecipient, error) {
	f.moduleCalls++
	out := map[string][]workforcedomain.NotificationRecipient{}
	if scopeType != "center" || moduleCode != "pc.vaccination" {
		return out, nil
	}
	wanted := map[string]bool{}
	for _, duty := range dutyTypes {
		wanted[duty] = true
	}
	for _, scopeID := range scopeIDs {
		for seat, device := range f.parkSeats {
			for _, duty := range f.parkDuties[seat] {
				if wanted[duty] {
					out[scopeID+"|"+seat] = append(out[scopeID+"|"+seat], device)
					break
				}
			}
		}
	}
	return out, nil
}

func (f *rosterSeatFake) ResolvePositionRecipientsBatch(_ context.Context, _, scopeType string, scopeIDs, positionCodes []string, _ time.Time) (map[string][]workforcedomain.NotificationRecipient, error) {
	out := map[string][]workforcedomain.NotificationRecipient{}
	seats := f.parkSeats
	if scopeType == "tenant" {
		seats = f.tenantSeats
	}
	for _, scopeID := range scopeIDs {
		for _, code := range positionCodes {
			if device, ok := seats[code]; ok {
				out[scopeID+"|"+code] = append(out[scopeID+"|"+code], device)
			}
		}
	}
	return out, nil
}

func newRosterSeatFake() *rosterSeatFake {
	return &rosterSeatFake{
		parkSeats: map[string]workforcedomain.NotificationRecipient{
			"vaccination_operator_amit": {WorkforceMemberID: "m-operator", DeviceID: "d-operator", FCMToken: "fcm-operator"},
			"park_head":                 {WorkforceMemberID: "m-parkhead", DeviceID: "d-parkhead", FCMToken: "fcm-parkhead"},
			"preventive_care_manager":   {WorkforceMemberID: "m-manager", DeviceID: "d-manager", FCMToken: "fcm-manager"},
		},
		parkDuties: map[string][]string{
			"vaccination_operator_amit": {"execute"},
			"park_head":                 {"manage"},
			"preventive_care_manager":   {"manage"},
		},
		tenantSeats: map[string]workforcedomain.NotificationRecipient{
			"pc_director":  {WorkforceMemberID: "m-director", DeviceID: "d-director", FCMToken: "fcm-director"},
			"ceo_internal": {WorkforceMemberID: "m-ceo", DeviceID: "d-ceo", FCMToken: "fcm-ceo"},
		},
	}
}

// TestReminderCadenceAudienceResolvesEveryVaccinationDutyHolder is the BLOCKER-2 guard.
//
// The reminder ladder used to address a literal position-code list
// {"operator", "park_head", "phc_manager"}. Against the real seeded roster only park_head exists,
// so the operator who has to run the drive received NO T-7, NO 08:00, NO 13:00 and NO due-today
// reminder -- silently, because the stage happily queued the one recipient it did resolve.
//
// The audience is now resolved by MODULE DUTY, so this test asserts all three seats are reached and,
// just as importantly, that the resolution asks for the duty rather than for seat names.
func TestReminderCadenceAudienceResolvesEveryVaccinationDutyHolder(t *testing.T) {
	roster := newRosterSeatFake()
	stage := &ReminderCadenceStage{tenantID: "tenant-1", roster: roster}

	byPark, tenantRecipients, err := stage.resolveCadenceAudience(context.Background(), []string{"park-1"}, time.Now())
	if err != nil {
		t.Fatalf("resolve audience: %v", err)
	}

	gotTokens := tokensOfRecipients(byPark["park-1"])
	wantTokens := []string{"fcm-manager", "fcm-operator", "fcm-parkhead"}
	if !equalStrings(gotTokens, wantTokens) {
		t.Fatalf("park audience = %v, want every vaccination duty holder %v (the operator must be reachable)", gotTokens, wantTokens)
	}

	// The audience must come from ONE batched duty read, not from a seat-name list and not per park.
	if roster.moduleCalls != 1 {
		t.Fatalf("park audience resolved with %d module-duty reads, want exactly 1 batched duty read", roster.moduleCalls)
	}

	gotLeadership := tokensOfRecipients(tenantRecipients)
	wantLeadership := []string{"fcm-ceo", "fcm-director"}
	if !equalStrings(gotLeadership, wantLeadership) {
		t.Fatalf("leadership audience = %v, want %v", gotLeadership, wantLeadership)
	}
}

// TestReminderCadenceAudienceIgnoresSeatsWithoutTheDuty proves the duty is the membership rule, not
// the name: a seat that merely LOOKS like a vaccination seat but carries no pc.vaccination duty is
// not notified, and a seat holding the duty under any name is.
func TestReminderCadenceAudienceIgnoresSeatsWithoutTheDuty(t *testing.T) {
	roster := newRosterSeatFake()
	roster.parkSeats["feeding_operator"] = workforcedomain.NotificationRecipient{
		WorkforceMemberID: "m-feed", DeviceID: "d-feed", FCMToken: "fcm-feed",
	}
	roster.parkDuties["feeding_operator"] = []string{"execute-feed-only"}

	stage := &ReminderCadenceStage{tenantID: "tenant-1", roster: roster}
	byPark, _, err := stage.resolveCadenceAudience(context.Background(), []string{"park-1"}, time.Now())
	if err != nil {
		t.Fatalf("resolve audience: %v", err)
	}
	for _, token := range tokensOfRecipients(byPark["park-1"]) {
		if token == "fcm-feed" {
			t.Fatalf("a seat without the vaccination duty was notified: %v", tokensOfRecipients(byPark["park-1"]))
		}
	}
}

// tokensOfRecipients returns the sorted FCM tokens of a resolved audience.
func tokensOfRecipients(recipients []calendarports.NotificationRecipient) []string {
	tokens := make([]string, 0, len(recipients))
	for _, recipient := range recipients {
		tokens = append(tokens, recipient.FCMToken)
	}
	sort.Strings(tokens)
	return tokens
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
