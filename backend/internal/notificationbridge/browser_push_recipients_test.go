package notificationbridge

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/browserpush"
	workforcedomain "github.com/vgoats/goatos/backend/internal/workforce/domain"
)

// fakePhoneRecipients is the roster seam: phones only, exactly as the Android-only device
// registry answers today.
type fakePhoneRecipients struct {
	member    []workforcedomain.NotificationRecipient
	memberErr error
	position  []workforcedomain.NotificationRecipient
	duty      []workforcedomain.NotificationRecipient
	calls     int
}

func (f *fakePhoneRecipients) ResolveMemberRecipients(_ context.Context, _, _ string) ([]workforcedomain.NotificationRecipient, error) {
	f.calls++
	return f.member, f.memberErr
}

func (f *fakePhoneRecipients) ResolveModuleDutyRecipients(_ context.Context, _, _, _, _, _ string) ([]workforcedomain.NotificationRecipient, error) {
	return f.duty, nil
}

func (f *fakePhoneRecipients) ResolvePositionRecipients(_ context.Context, _, _, _, _ string) ([]workforcedomain.NotificationRecipient, error) {
	return f.position, nil
}

type fakeBrowserRecipients struct {
	recipients    []browserpush.Recipient
	position      []browserpush.Recipient
	duty          []browserpush.Recipient
	err           error
	calls         int
	positionCalls int
	dutyCalls     int
}

func (f *fakeBrowserRecipients) ResolveBrowserRecipients(_ context.Context, _, _ string) ([]browserpush.Recipient, error) {
	f.calls++
	return f.recipients, f.err
}

func (f *fakeBrowserRecipients) ResolveModuleDutyBrowserRecipients(context.Context, string, string, string, string, string, time.Time) ([]browserpush.Recipient, error) {
	f.dutyCalls++
	return f.duty, f.err
}

func (f *fakeBrowserRecipients) ResolvePositionBrowserRecipients(context.Context, string, string, string, string, time.Time) ([]browserpush.Recipient, error) {
	f.positionCalls++
	return f.position, f.err
}

// A browser is a delivery address beside the phone, not instead of it. This is the whole point of
// the change: the CEO's Chrome has to appear in the recipient list the consumer writes rows from.
func TestBrowsersAreReturnedBesideTheirOwnersPhones(t *testing.T) {
	phones := &fakePhoneRecipients{member: []workforcedomain.NotificationRecipient{
		{WorkforceMemberID: "member-1", DeviceID: "device-1", FCMToken: "phone-token"},
	}}
	browsers := &fakeBrowserRecipients{recipients: []browserpush.Recipient{
		{WorkforceMemberID: "member-1", BrowserRegistrationID: "reg-1", Token: "chrome-token"},
		{WorkforceMemberID: "member-1", BrowserRegistrationID: "reg-2", Token: "edge-token"},
	}}

	resolver := WithBrowserRecipients(phones, browsers, nil)
	got, err := resolver.ResolveMemberRecipients(context.Background(), "tenant-1", "member-1")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("want 3 recipients (1 phone + 2 browsers), got %d: %+v", len(got), got)
	}
	if got[0].FCMToken != "phone-token" {
		t.Fatalf("the phone must come first and unchanged, got %q", got[0].FCMToken)
	}
	// The registration id takes the place device_id holds on the phone path. That is what makes
	// the downstream idempotency key per-browser, so two browsers each get their own
	// notification_requests row instead of racing for one.
	if got[1].DeviceID != "reg-1" || got[2].DeviceID != "reg-2" {
		t.Fatalf("each browser must carry its own registration id, got %+v", got[1:])
	}
	if got[1].WorkforceMemberID != "member-1" {
		t.Fatalf("a browser row must stay attributable to its person, got %q", got[1].WorkforceMemberID)
	}
}

// FAIL OPEN. A phone notification must never be lost because the browser registry was
// unavailable -- the browser copy is the newer, lesser half of the delivery.
func TestABrowserLookupFailureNeverLosesThePhoneRecipients(t *testing.T) {
	phones := &fakePhoneRecipients{member: []workforcedomain.NotificationRecipient{
		{WorkforceMemberID: "member-1", DeviceID: "device-1", FCMToken: "phone-token"},
	}}
	browsers := &fakeBrowserRecipients{err: errors.New("registry unavailable")}

	resolver := WithBrowserRecipients(phones, browsers, nil)
	got, err := resolver.ResolveMemberRecipients(context.Background(), "tenant-1", "member-1")
	if err != nil {
		t.Fatalf("a browser failure must not surface as an error, got %v", err)
	}
	if len(got) != 1 || got[0].FCMToken != "phone-token" {
		t.Fatalf("want the phone recipients unchanged, got %+v", got)
	}
}

// The reverse is NOT fail-open: a phone lookup that failed produced no recipient list at all, and
// a browser copy cannot stand in for one nobody could read.
func TestAPhoneLookupFailureStillSurfaces(t *testing.T) {
	phones := &fakePhoneRecipients{memberErr: errors.New("roster unavailable")}
	browsers := &fakeBrowserRecipients{recipients: []browserpush.Recipient{
		{WorkforceMemberID: "member-1", BrowserRegistrationID: "reg-1", Token: "chrome-token"},
	}}

	resolver := WithBrowserRecipients(phones, browsers, nil)
	if _, err := resolver.ResolveMemberRecipients(context.Background(), "tenant-1", "member-1"); err == nil {
		t.Fatal("want the roster error to surface")
	}
	if browsers.calls != 0 {
		t.Fatalf("the browser registry must not be asked after the roster failed, got %d calls", browsers.calls)
	}
}

// A person with no browser subscribed must produce byte-for-byte what they produced before this
// change existed -- the decorator is additive or it is a regression.
func TestAPersonWithNoBrowserIsUnchanged(t *testing.T) {
	phones := &fakePhoneRecipients{member: []workforcedomain.NotificationRecipient{
		{WorkforceMemberID: "member-1", DeviceID: "device-1", FCMToken: "phone-token"},
	}}
	browsers := &fakeBrowserRecipients{}

	resolver := WithBrowserRecipients(phones, browsers, nil)
	got, err := resolver.ResolveMemberRecipients(context.Background(), "tenant-1", "member-1")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(got) != 1 || got[0] != phones.member[0] {
		t.Fatalf("want the phone recipients unchanged, got %+v", got)
	}
}

// A person reachable ONLY in the browser is still reachable. Before this change they received
// nothing at all, which is the defect.
func TestAPersonWithOnlyABrowserIsStillReachable(t *testing.T) {
	phones := &fakePhoneRecipients{}
	browsers := &fakeBrowserRecipients{recipients: []browserpush.Recipient{
		{WorkforceMemberID: "member-1", BrowserRegistrationID: "reg-1", Token: "chrome-token"},
	}}

	resolver := WithBrowserRecipients(phones, browsers, nil)
	got, err := resolver.ResolveMemberRecipients(context.Background(), "tenant-1", "member-1")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(got) != 1 || got[0].FCMToken != "chrome-token" {
		t.Fatalf("want the browser recipient, got %+v", got)
	}
}

// One person, one event, one notification. A token that somehow appears on both sides must not
// produce two rows.
func TestADuplicateTokenIsNotNotifiedTwice(t *testing.T) {
	phones := &fakePhoneRecipients{member: []workforcedomain.NotificationRecipient{
		{WorkforceMemberID: "member-1", DeviceID: "device-1", FCMToken: "shared-token"},
	}}
	browsers := &fakeBrowserRecipients{recipients: []browserpush.Recipient{
		{WorkforceMemberID: "member-1", BrowserRegistrationID: "reg-1", Token: "shared-token"},
		{WorkforceMemberID: "member-1", BrowserRegistrationID: "reg-2", Token: "shared-token"},
		{WorkforceMemberID: "member-1", BrowserRegistrationID: "reg-3", Token: ""},
	}}

	resolver := WithBrowserRecipients(phones, browsers, nil)
	got, err := resolver.ResolveMemberRecipients(context.Background(), "tenant-1", "member-1")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 deduped recipient, got %d: %+v", len(got), got)
	}
}

// The decorator is opt-in at the wiring site. A process that has not built the browser registry
// must get the resolver back untouched rather than a wrapper that silently does nothing.
func TestAMissingBrowserSourceLeavesTheResolverUntouched(t *testing.T) {
	phones := &fakePhoneRecipients{}
	if got := WithBrowserRecipients(phones, nil, nil); got != RecipientResolver(phones) {
		t.Fatalf("want the original resolver back, got %T", got)
	}
	if got := WithBrowserRecipients(nil, &fakeBrowserRecipients{}, nil); got != nil {
		t.Fatalf("want nil back for a nil resolver, got %T", got)
	}
}

func TestThePositionAndDutyPathsIncludeBrowsers(t *testing.T) {
	phones := &fakePhoneRecipients{
		position: []workforcedomain.NotificationRecipient{{WorkforceMemberID: "m", DeviceID: "d", FCMToken: "phone-a"}},
		duty:     []workforcedomain.NotificationRecipient{{WorkforceMemberID: "m", DeviceID: "d", FCMToken: "phone-b"}},
	}
	browsers := &fakeBrowserRecipients{
		position: []browserpush.Recipient{{WorkforceMemberID: "m", BrowserRegistrationID: "reg-position", Token: "chrome-position"}},
		duty:     []browserpush.Recipient{{WorkforceMemberID: "m", BrowserRegistrationID: "reg-duty", Token: "chrome-duty"}},
	}
	resolver := WithBrowserRecipients(phones, browsers, nil)

	position, err := resolver.ResolvePositionRecipients(context.Background(), "t", "park", "p1", "park_head")
	if err != nil {
		t.Fatalf("position: %v", err)
	}
	if len(position) != 2 || position[0].FCMToken != "phone-a" || position[1].FCMToken != "chrome-position" {
		t.Fatalf("want phone plus Chrome position recipients, got %+v", position)
	}

	duty, err := resolver.ResolveModuleDutyRecipients(context.Background(), "t", "park", "p1", "feed", "verify")
	if err != nil {
		t.Fatalf("duty: %v", err)
	}
	if len(duty) != 2 || duty[0].FCMToken != "phone-b" || duty[1].FCMToken != "chrome-duty" {
		t.Fatalf("want phone plus Chrome duty recipients, got %+v", duty)
	}

	if browsers.positionCalls != 1 || browsers.dutyCalls != 1 {
		t.Fatalf("browser registry calls position=%d duty=%d, want 1/1", browsers.positionCalls, browsers.dutyCalls)
	}
}
