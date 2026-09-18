package notificationbridge

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
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
// delivered by outbox-relay / domain-event-consumer / the kernel stages -- phone-only (review of
// PR 295, 2026-09-18). Every `notificationbridge.New*Consumer|Notifier(` in a production
// composition site must NOT receive the raw roster service, at any argument depth, under any
// name.
//
// It walks the Go AST rather than grepping (Judge A, same day, produced two grep bypasses: an
// alias `roster := rosterService`, and a depth-2 argument list the regex skipped). Per file it
// collects every identifier bound to `workforceapp.NewRosterService(...)`, follows plain
// aliases (`x := y`, `var x = y`, `x = y`) to a fixed point, and then flags any of those names
// appearing ANYWHERE inside the argument list of a consumer/notifier constructor. A roster
// passed INTO `WithBrowserRecipients(` / `notifyRecipients(` / `NewStoredAudience(` is the
// wiring itself and is not flagged. Blind spots, stated: a roster reaching a constructor
// through a struct field or a function return, and a resolver built in another package -- the
// self-test below covers the two probes and the field shape is not used by any site today.
func TestEveryNotifierInAnAsyncProcessReachesBrowsers(t *testing.T) {
	sites := 0
	for _, path := range productionWiringFiles(t) {
		findings, n := bareRosterConsumerSites(t, path)
		sites += n
		for _, f := range findings {
			t.Errorf("%s: %s", path, f)
		}
	}
	if sites < 15 {
		t.Fatalf("found only %d consumer construction sites; the scan is broken, not the wiring", sites)
	}
}

// bareRosterConsumerSites returns one finding per notificationbridge consumer/notifier
// construction in the file whose arguments name a roster-service binding, and the number of
// construction sites seen.
func bareRosterConsumerSites(t *testing.T, path string) ([]string, int) {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	isCall := func(e ast.Node, pkg, fn string) bool {
		call, ok := e.(*ast.CallExpr)
		if !ok {
			return false
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return false
		}
		x, ok := sel.X.(*ast.Ident)
		return ok && x.Name == pkg && (fn == "" || sel.Sel.Name == fn)
	}
	// Names bound to the roster service, aliases followed to a fixed point.
	rosters := map[string]bool{}
	for changed := true; changed; {
		changed = false
		ast.Inspect(file, func(n ast.Node) bool {
			var lhs, rhs []ast.Expr
			switch st := n.(type) {
			case *ast.AssignStmt:
				lhs, rhs = st.Lhs, st.Rhs
			case *ast.ValueSpec:
				for _, id := range st.Names {
					lhs = append(lhs, id)
				}
				rhs = st.Values
			default:
				return true
			}
			if len(lhs) != len(rhs) {
				return true
			}
			for i := range lhs {
				id, ok := lhs[i].(*ast.Ident)
				if !ok || rosters[id.Name] {
					continue
				}
				src, isIdent := rhs[i].(*ast.Ident)
				if isCall(rhs[i], "workforceapp", "NewRosterService") || (isIdent && rosters[src.Name]) {
					rosters[id.Name] = true
					changed = true
				}
			}
			return true
		})
	}
	var findings []string
	sites := 0
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || !isCall(call, "notificationbridge", "") {
			return true
		}
		name := call.Fun.(*ast.SelectorExpr).Sel.Name
		if !strings.HasPrefix(name, "New") || (!strings.Contains(name, "Consumer") && !strings.Contains(name, "Notifier")) {
			return true
		}
		sites++
		for _, arg := range call.Args {
			ast.Inspect(arg, func(m ast.Node) bool {
				// Inside the decorator / audience call the roster is the wiring, not a bypass.
				if isCall(m, "notificationbridge", "WithBrowserRecipients") || isCall(m, "notificationbridge", "NewStoredAudience") {
					return false
				}
				if c, ok := m.(*ast.CallExpr); ok {
					if fn, ok := c.Fun.(*ast.Ident); ok && fn.Name == "notifyRecipients" {
						return false
					}
				}
				if id, ok := m.(*ast.Ident); ok && rosters[id.Name] {
					findings = append(findings, fmt.Sprintf("%s: notificationbridge.%s is built on the roster service (%q); its recipients must come from WithBrowserRecipients / kernelstages.notifyRecipients, or its pushes never reach Chrome", fset.Position(call.Pos()), name, id.Name))
				}
				return true
			})
		}
		return true
	})
	return findings, sites
}

// TestBareRosterScanCatchesTheTwoProbes is the adversarial self-test for the scan above: the
// alias and the depth-2 argument list that the earlier regex let through must both be findings,
// and the decorated shapes must not be.
func TestBareRosterScanCatchesTheTwoProbes(t *testing.T) {
	src := `package probe

func wire(pool any, logger any) {
	rosterService := workforceapp.NewRosterService(workforceRepo, workforceRepo)
	roster := rosterService
	var again = roster
	notifyRecipients := notificationbridge.WithBrowserRecipients(rosterService, browserpush.NewService(nil), logger)
	audience := notificationbridge.NewStoredAudience(rosterService, nil, logger)
	notificationbridge.NewLeaveRequestNotifyConsumer(roster, calendarService, logger).WithAudience(audience)                              // alias: finding
	notificationbridge.NewPenVisitDueNotifier(again, calendarapp.NewService(calendarpg.NewRepository(pool, cfg.QueryTimeout)), logger)    // alias of alias, depth 2: finding
	notificationbridge.NewFeedLowStockNotifier(feedRepo, rosterService, calendarService, logger)                                            // not first arg: finding
	notificationbridge.NewLeadershipTaskNotifyConsumer(notifyRecipients, calendarService, logger).WithAudience(audience)                    // wired: ok
	notificationbridge.NewPenRoutineDueNotifier(notifyRecipients(deps, rosterService, logger), calendarService, logger)                    // kernelstages helper: ok
	notificationbridge.NewObligationMissedNotifier(calendarService, notificationbridge.WithBrowserRecipients(rosterService, b, logger), calendarService, logger) // inline decorator: ok
}
`
	dir := t.TempDir()
	path := filepath.Join(dir, "probe.go")
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	findings, sites := bareRosterConsumerSites(t, path)
	if sites != 6 {
		t.Fatalf("sites = %d, want 6", sites)
	}
	if len(findings) != 3 {
		t.Fatalf("findings = %d, want 3 (alias, alias-of-alias at depth 2, non-first argument):\n%s", len(findings), strings.Join(findings, "\n"))
	}
	for _, want := range []string{`"roster"`, `"again"`, `"rosterService"`} {
		if !strings.Contains(strings.Join(findings, "\n"), want) {
			t.Errorf("no finding names %s:\n%s", want, strings.Join(findings, "\n"))
		}
	}
}
