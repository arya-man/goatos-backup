// Package contract holds STATIC guards over the domain-event contract.
//
// These read SOURCE, not a database. That distinction is the point: a guard that
// queries outbox_messages only sees types some other test happened to produce, so a
// brand-new producer passes CI and then fails silently in production. The relay marks
// an unrecognised envelope 'failed' with last_error='invalid_event_envelope' on attempt
// 1, never retries it, never delivers it, and never alerts -- that is how 182 events
// were lost on stg before anyone noticed.
package contract

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// eventTypeShape matches the dotted lower-snake naming every domain event uses
// (goat.created, weighing.observation_accepted, obligation.reopened).
var eventTypeShape = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z0-9_]+)+$`)

// schemaRefShape excludes envelope/schema pointers, which share the dotted shape but are
// not event types (e.g. "calendar.notification.v1").
var schemaRefShape = regexp.MustCompile(`\.v[0-9]+$`)

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	return root
}

func acceptedEventTypes(t *testing.T) map[string]bool {
	t.Helper()
	path := filepath.Join(repoRoot(t), "contracts", "jsonschema", "domain-event-envelope.schema.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read envelope schema: %v", err)
	}
	var schema struct {
		Properties struct {
			EventType struct {
				Enum []string `json:"enum"`
			} `json:"event_type"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("parse envelope schema: %v", err)
	}
	if len(schema.Properties.EventType.Enum) == 0 {
		t.Fatal("envelope schema declares no event_type enum; this guard would pass vacuously")
	}
	accepted := make(map[string]bool, len(schema.Properties.EventType.Enum))
	for _, e := range schema.Properties.EventType.Enum {
		accepted[e] = true
	}
	return accepted
}

type emitted struct {
	eventType string
	where     string
}

// collectEmittedEventTypes returns every string literal that reaches an outbox writer as
// its event type. BOTH shapes must be covered, because producers use both:
//
//  1. A *EventType string constant (obligation, weighing, feeddirection, verification).
//  2. A literal passed straight to a writer at the call site. calendar's
//     reminder_cadence.go does exactly this with "calendar.reminder.cadence.queued" -- a
//     guard that reads only constants misses it and reports green while that event is
//     being dropped in production. Covering constants alone is not a smaller version of
//     this check; it is a check that does not catch the bug that motivated it.
//
// For case 2 the parameter INDEX is resolved from each writer's own declaration rather
// than assumed, because the writers disagree (calendar's insertOutbox takes eventType
// 4th, obligation's insertObligationLifecycleOutbox 5th) and a fixed position reads
// schemaRef values like "calendar.notification.v1" as event types.
//
// An eventType parameter alone is NOT sufficient: weighing's
// idempotencyResource(ctx, tx, tenantID, eventType, idem, fingerprint) names its
// idempotency-key namespace "eventType" too, and those namespaces
// ("weighing.individual_scope_submitted", "weighing.scope_reopened") are not domain
// events and are correctly absent from the enum. Requiring the function BODY to write
// outbox_messages separates a real producer from a look-alike; without it this guard
// reports two findings that are pure noise, and a guard that cries wolf gets deleted.
func collectEmittedEventTypes(t *testing.T) []emitted {
	t.Helper()
	backend := filepath.Join(repoRoot(t), "backend")
	fset := token.NewFileSet()

	eventTypeParamIndex := map[string]int{}
	type parsed struct {
		path string
		file *ast.File
	}
	var files []parsed

	err := filepath.Walk(backend, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			base := info.Name()
			if base == "vendor" || base == "testdata" || strings.HasPrefix(base, ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			return nil
		}
		src, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil
		}
		files = append(files, parsed{path, f})

		ast.Inspect(f, func(n ast.Node) bool {
			fn, ok := n.(*ast.FuncDecl)
			if !ok || fn.Type.Params == nil || fn.Body == nil {
				return true
			}
			bodyStart := fset.Position(fn.Body.Pos()).Offset
			bodyEnd := fset.Position(fn.Body.End()).Offset
			if bodyStart < 0 || bodyEnd > len(src) || bodyStart >= bodyEnd {
				return true
			}
			if !strings.Contains(string(src[bodyStart:bodyEnd]), "outbox_messages") {
				return true
			}
			idx := 0
			for _, field := range fn.Type.Params.List {
				if len(field.Names) == 0 {
					idx++
					continue
				}
				for _, name := range field.Names {
					if strings.EqualFold(name.Name, "eventType") {
						eventTypeParamIndex[fn.Name.Name] = idx
					}
					idx++
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walk backend: %v", err)
	}
	if len(eventTypeParamIndex) == 0 {
		t.Fatal("found no outbox writer taking an eventType parameter; this guard would pass vacuously")
	}

	var found []emitted
	add := func(lit string, pos token.Pos) {
		if lit == "" || !eventTypeShape.MatchString(lit) || schemaRefShape.MatchString(lit) {
			return
		}
		p := fset.Position(pos)
		rel, rerr := filepath.Rel(repoRoot(t), p.Filename)
		if rerr != nil {
			rel = p.Filename
		}
		found = append(found, emitted{eventType: lit, where: rel + ":" + strconv.Itoa(p.Line)})
	}

	for _, entry := range files {
		ast.Inspect(entry.file, func(n ast.Node) bool {
			switch node := n.(type) {
			case *ast.CallExpr:
				var name string
				switch fn := node.Fun.(type) {
				case *ast.Ident:
					name = fn.Name
				case *ast.SelectorExpr:
					name = fn.Sel.Name
				}
				idx, ok := eventTypeParamIndex[name]
				if !ok || idx >= len(node.Args) {
					return true
				}
				if lit, ok := node.Args[idx].(*ast.BasicLit); ok && lit.Kind == token.STRING {
					if v, uerr := strconv.Unquote(lit.Value); uerr == nil {
						add(v, lit.Pos())
					}
				}
			case *ast.ValueSpec:
				// Two naming shapes reach an outbox writer:
				//   fooEventType = "a.b.c"   -- adapter-local constant
				//   EventFoo     = "a.b.c"   -- domain constant, which adapters alias as
				//                               `fooEventType = domain.EventFoo`
				// Matching only the first misses counts entirely: its adapter declares
				// projectionExceptionOpenedEventType = domain.EventProjectionExceptionOpened,
				// a SelectorExpr whose VALUE lives in another package, so there is no literal
				// to read at the alias. Catching the domain constant at its own definition
				// covers the alias without cross-package identifier resolution.
				for i, ident := range node.Names {
					named := strings.HasSuffix(ident.Name, "EventType") ||
						(strings.HasPrefix(ident.Name, "Event") && len(ident.Name) > 5 &&
							ident.Name[5] >= 'A' && ident.Name[5] <= 'Z')
					if !named || i >= len(node.Values) {
						continue
					}
					if lit, ok := node.Values[i].(*ast.BasicLit); ok && lit.Kind == token.STRING {
						if v, uerr := strconv.Unquote(lit.Value); uerr == nil {
							add(v, lit.Pos())
						}
					}
				}
			case *ast.CompositeLit:
				// Some writers take the event type on an OPTIONS STRUCT rather than as a named
				// parameter -- counts' insertProjectionExceptionOutbox(..., opts
				// projectionExceptionOutboxOptions) is the live example. The parameter-index
				// walk cannot see those, so read the struct field directly.
				for _, elt := range node.Elts {
					kv, ok := elt.(*ast.KeyValueExpr)
					if !ok {
						continue
					}
					key, ok := kv.Key.(*ast.Ident)
					if !ok || !strings.EqualFold(key.Name, "eventType") {
						continue
					}
					if lit, ok := kv.Value.(*ast.BasicLit); ok && lit.Kind == token.STRING {
						if v, uerr := strconv.Unquote(lit.Value); uerr == nil {
							add(v, lit.Pos())
						}
					}
				}
			}
			return true
		})
	}
	return found
}

// TestEveryProducedEventTypeIsAcceptedByTheEnvelopeContract fails the build when a
// producer emits an event_type the envelope contract does not accept. It is the check
// that would have caught obligation.reopened, calendar.reminder.cadence.queued and the
// three feed.* types before they became undeliverable events in production.
func TestEveryProducedEventTypeIsAcceptedByTheEnvelopeContract(t *testing.T) {
	accepted := acceptedEventTypes(t)
	produced := collectEmittedEventTypes(t)
	if len(produced) == 0 {
		t.Fatal("found no produced event types; this guard would pass vacuously")
	}

	unaccepted := map[string][]string{}
	for _, e := range produced {
		if !accepted[e.eventType] {
			unaccepted[e.eventType] = append(unaccepted[e.eventType], e.where)
		}
	}
	if len(unaccepted) == 0 {
		return
	}
	names := make([]string, 0, len(unaccepted))
	for name := range unaccepted {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		sort.Strings(unaccepted[name])
		t.Errorf("event_type %q is produced but NOT accepted by contracts/jsonschema/domain-event-envelope.schema.json.\n"+
			"  emitted at: %s\n"+
			"  The relay marks this envelope 'failed' (invalid_event_envelope) on attempt 1, never retries it, and\n"+
			"  never delivers it to any consumer. Add the type to the schema's event_type enum, or emit an\n"+
			"  already-accepted type.",
			name, strings.Join(unaccepted[name], ", "))
	}
}
