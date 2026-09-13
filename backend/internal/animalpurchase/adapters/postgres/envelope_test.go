package postgres

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/vgoats/goatos/backend/internal/animalpurchase/domain"
	outboxapp "github.com/vgoats/goatos/backend/internal/outbox/app"
)

// captureTx records the outbox INSERT's envelope instead of writing it.
type captureTx struct {
	envelope []byte
}

func (c *captureTx) QueryRow(_ context.Context, _ string, _ ...any) pgx.Row { return uuidRow{} }

func (c *captureTx) Exec(_ context.Context, _ string, args ...any) (pgconn.CommandTag, error) {
	c.envelope = args[7].([]byte)
	return pgconn.CommandTag{}, nil
}

type uuidRow struct{}

func (uuidRow) Scan(dest ...any) error {
	*(dest[0].(*string)) = "9e1c2a44-1f3c-4a1f-9b1e-000000000001"
	return nil
}

// TestDecidedEnvelopeSatisfiesTheSharedSchema pins the outbox envelope against the shared
// domain-event schema the relay validates with. It exists because the first version of this
// emitter shipped with `tenant_id` at the top level and no `recorded_at`, and the relay marked
// every decision event `invalid_event_envelope` -- so no push ever left, silently.
func TestDecidedEnvelopeSatisfiesTheSharedSchema(t *testing.T) {
	validator, err := outboxapp.NewEnvelopeValidator("../../../../../contracts/jsonschema/domain-event-envelope.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	tx := &captureTx{}
	now := time.Now()
	c := domain.Candidate{CandidateID: "7a89f1c5-80f9-4e8b-8006-c9245c421299", LoadID: "4f2009e7-a56e-4092-9a0c-60c62188e676", LoadRef: "132", SeqNo: 1,
		Species: "goat", Sex: "female", Breed: "Sirohi", Decision: "accepted", DecidedByName: "Ravi", DecidedAt: &now, RecordedBy: "ad6198a2-24eb-5b8c-a885-c322a7a3de56"}
	load := domain.Load{LoadID: c.LoadID, LoadRef: "132", FarmLabel: "CPT", VendorName: "Ramesh Traders", Counts: domain.DecisionCounts{Total: 3, Pending: 2, Accepted: 1}}
	if err := emitDecided(context.Background(), tx, "00000000-0000-4000-8000-000000000001", "d1408eef-58e5-52e2-9a0f-6e38237627d1", "decide-1", c, load); err != nil {
		t.Fatal(err)
	}
	if err := validator.Validate(tx.envelope); err != nil {
		t.Fatalf("envelope rejected by the shared schema:\n%v\n%s", err, tx.envelope)
	}
	var env map[string]any
	_ = json.Unmarshal(tx.envelope, &env)
	if env["aggregate_type"] != "animal_purchase_candidate" || env["event_type"] != DecidedEventType {
		t.Fatalf("envelope = %s", tx.envelope)
	}
}
