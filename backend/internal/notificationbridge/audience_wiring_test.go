package notificationbridge

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	audiencedomain "github.com/vgoats/goatos/backend/internal/notificationaudience/domain"
)

// Every verification module the consumer routes must have its three proof-lifecycle rows in the
// audience catalog. The consumer composes "<module>.proof_pending" from pendingModuleProfiles'
// key; a module with a profile but no catalog row makes the resolver refuse the key, and the
// leadership copy of that module's proofs silently reaches nobody.
func TestEveryVerificationModuleHasAnAudienceCatalogRow(t *testing.T) {
	for module, profile := range pendingModuleProfiles {
		if _, ok := audiencedomain.AlertByKey(profileAlertKey(module, audiencedomain.ProofReviewSuffix)); !ok {
			t.Errorf("verification module %q has no verifier proof_review catalog row", module)
		}
		for _, suffix := range []string{audiencedomain.ProofPendingSuffix, audiencedomain.ProofApprovedSuffix, audiencedomain.ProofReworkSuffix} {
			key := profileAlertKey(module, suffix)
			alert, ok := audiencedomain.AlertByKey(key)
			if !ok {
				t.Errorf("verification module %q has a routing profile but no audience catalog row %q", module, key)
				continue
			}
			// The catalog default must name the SAME director the profile routes to, or the
			// matrix would show one desk while the default push went to another.
			found := false
			for _, code := range alert.DefaultDesignations {
				if code == profile.leadershipPosition {
					found = true
				}
			}
			if !found {
				t.Errorf("%s defaults to %v but the profile routes to %s", key, alert.DefaultDesignations, profile.leadershipPosition)
			}
		}
	}
}

// upwardConstructors are the constructors whose consumers/notifiers route a push UPWARD by
// designation. Each builds a defaults-only resolver so unit tests keep their roster fakes; a
// PRODUCTION site must chain .WithAudience(...) or the stored override is never read and the
// Notifications matrix silently does nothing for that push.
var upwardConstructors = []string{
	"notificationbridge.NewVerificationEventConsumer(",
	"notificationbridge.NewWeighingSubmissionEventConsumer(",
	"notificationbridge.NewWeighingLifecycleEventConsumer(",
	"notificationbridge.NewObligationMissedNotifier(",
	"notificationbridge.NewFeedLowStockNotifier(",
	"notificationbridge.NewLoadAgeNotifier(",
	"notificationbridge.NewSaleFeedReduceNotifier(",
	"notificationbridge.NewLeadershipTaskNotifyConsumer(",
}

// productionWiringFiles are the non-test Go files that construct those consumers for a running
// process: the API's in-process bus, the durable buses, and the kernel stages.
func productionWiringFiles(t *testing.T) []string {
	t.Helper()
	root := filepath.Join("..", "..")
	var files []string
	for _, dir := range []string{
		filepath.Join(root, "internal", "bootstrap"),
		filepath.Join(root, "internal", "kernelstages"),
		filepath.Join(root, "internal", "domainconsumer"),
		filepath.Join(root, "cmd"),
	} {
		err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			files = append(files, path)
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", dir, err)
		}
	}
	return files
}

// statementAfter returns the source from the constructor call through every chained
// .Method(...) call that follows it, so the check reads the whole builder chain and not one line.
func statementAfter(src string, start int) string {
	depth := 0
	i := start
	for i < len(src) {
		switch src[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				// Consume a following ".Method(" chain, tolerating whitespace/newlines between.
				j := i + 1
				for j < len(src) && (src[j] == ' ' || src[j] == '\t' || src[j] == '\n' || src[j] == '\r') {
					j++
				}
				if j < len(src) && src[j] == '.' {
					i = j
					continue
				}
				return src[start : i+1]
			}
		}
		i++
	}
	return src[start:]
}

var reminderCadenceLeadershipRe = regexp.MustCompile(`leadership:\s*notificationbridge\.NewStoredAudience\(`)

// withAudienceRe tolerates a builder chain broken across lines after the dot (gofmt's shape).
var withAudienceRe = regexp.MustCompile(`\.\s*WithAudience\(`)

func TestEveryUpwardNotifierIsWiredToTheStoredAudience(t *testing.T) {
	sites := 0
	for _, path := range productionWiringFiles(t) {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		src := string(raw)
		for _, ctor := range upwardConstructors {
			for offset := 0; ; {
				idx := strings.Index(src[offset:], ctor)
				if idx < 0 {
					break
				}
				start := offset + idx
				stmt := statementAfter(src, start)
				if !withAudienceRe.MatchString(stmt) {
					t.Errorf("%s: %s is constructed without .WithAudience(NewStoredAudience(...)); this process would serve catalog defaults and ignore the Notifications matrix:\n%s", path, strings.TrimSuffix(ctor, "("), stmt)
				}
				sites++
				offset = start + len(ctor)
			}
		}
		// The reminder cadence stage resolves its leadership rung itself rather than through a
		// bridge constructor; its constructor must wire the stored audience the same way.
		if strings.HasSuffix(path, filepath.Join("kernelstages", "reminder_cadence.go")) && !reminderCadenceLeadershipRe.MatchString(src) {
			t.Errorf("%s: NewReminderCadenceStage must set leadership: notificationbridge.NewStoredAudience(...)", path)
		}
	}
	if sites < 10 {
		t.Fatalf("found only %d upward construction sites; the scan is broken, not the wiring", sites)
	}
}

// TestUpwardPushesAskTheResolverNotAPositionCode pins the other half: inside this package no
// upward push may still resolve a leadership desk by hand. The verifier's duty read and the
// operator's member read are the only recipient reads left outside the resolver; a director or
// CEO position code resolved directly is the defect this decision removed.
func TestUpwardPushesAskTheResolverNotAPositionCode(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	direct := regexp.MustCompile(`ResolvePositionRecipients\([^)]*(positionCEOInternal|positionPCDirector|positionGrowthDirector|positionFeedDirector|positionHealthDirector|positionProcurementDirector|leadershipPosition|directorPosition)\)`)
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		raw, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if m := direct.FindString(string(raw)); m != "" {
			t.Errorf("%s resolves a leadership desk by position code (%s); route it through AudienceResolver with a catalog alert key", name, m)
		}
	}
}

// TestEveryNotifierInAnAsyncProcessReachesBrowsers pins the other seam a consumer is built on:
// its RECIPIENT resolver. `WithBrowserRecipients` is the ONLY place a person's subscribed
// browsers join their phones, and the API process wiring it alone (bootstrap/api.go) left every
// push that actually originates asynchronously -- a task comment, a mention, a status change,
// delivered by outbox-relay / domain-event-consumer -- phone-only (review of PR 295,
// 2026-09-18). Every `notificationbridge.New*(` construction in a production composition site
// must take a resolver that came out of WithBrowserRecipients, never the raw roster service.
func TestEveryNotifierInAnAsyncProcessReachesBrowsers(t *testing.T) {
	// A constructor call with its whole (possibly nested) argument list.
	ctorRe := regexp.MustCompile(`notificationbridge\.New(\w+)\(((?:[^()]|\([^()]*\))*)\)`)
	decoratedRe := regexp.MustCompile(`(?:notificationbridge\.WithBrowserRecipients|notifyRecipients)\([^()]*\)`)
	bareRoster := regexp.MustCompile(`(^|[\s(,])rosterService\s*[,)]`)
	sites := 0
	for _, path := range productionWiringFiles(t) {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		src := string(raw)
		// The API and the durable buses call the decorator; the kernel stages call the package's
		// notifyRecipients helper, which is the same decorator behind a nil-pool guard.
		wired := strings.Contains(src, "notificationbridge.WithBrowserRecipients(") || strings.Contains(src, "notifyRecipients(")
		for _, m := range ctorRe.FindAllStringSubmatch(src, -1) {
			name, args := m[1], m[2]
			if name == "StoredAudience" || (!strings.Contains(name, "Consumer") && !strings.Contains(name, "Notifier")) {
				continue
			}
			sites++
			// A roster INSIDE the decorator call is the wiring, not a bypass.
			stripped := decoratedRe.ReplaceAllString(args, "decorated")
			// A notifier that takes no roster at all (Slack-only) has nothing to decorate.
			if bareRoster.MatchString(stripped) || (!wired && strings.Contains(args, "rosterService")) {
				t.Errorf("%s: notificationbridge.New%s is built on the bare roster service; a consumer's recipients must come from WithBrowserRecipients (or kernelstages.notifyRecipients) in this process, or its pushes never reach Chrome:\n  New%s(%s)", path, name, name, args)
			}
		}
	}
	if sites < 15 {
		t.Fatalf("found only %d consumer construction sites; the scan is broken, not the wiring", sites)
	}
}
