// Package postgres stores the configuration registers on the tables the rest of the product
// already reads: locations + farm/park/shed profiles + shed_partitions for places, the
// species/sex/stage lookups for animal types, item_categories + inventory_items (+ vaccines)
// for the catalogue, and feed_item_catalog read-only.
//
// One store per register (stores_*.go) owns that register's SQL; this file owns what every
// write shares -- the transaction, the idempotency reservation, the audit row and the
// row_version fence -- so a register cannot forget one of them.
package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/readcache"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/configuration/domain"
	"github.com/vgoats/goatos/backend/internal/configuration/ports"
	"github.com/vgoats/goatos/backend/internal/platform/audit"
)

const (
	auditDomain = "configuration"
	idemScope   = "configuration.register"
)

type querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// store is what one register's SQL must provide. Every method is tenant-scoped by argument.
type store interface {
	count(ctx context.Context, q querier, tenantID string) (int, error)
	list(ctx context.Context, q querier, tenantID string, p ports.ListParams) (ports.Page, error)
	get(ctx context.Context, q querier, tenantID, id string) (domain.Row, error)
	options(ctx context.Context, q querier, tenantID string) ([]ports.RefOption, error)
	usage(ctx context.Context, q querier, tenantID, id string) (domain.Usage, error)
	insert(ctx context.Context, tx pgx.Tx, tenantID string, fields map[string]any) (string, error)
	// update applies the changed fields; rowVersion is the fence the screen read (0 = none). It
	// returns the row's id afterwards, which a composite id (a partition's shed:label) may have
	// changed; "" means unchanged.
	update(ctx context.Context, tx pgx.Tx, tenantID, id string, fields map[string]any, rowVersion int) (string, error)
	setStatus(ctx context.Context, tx pgx.Tx, tenantID, id, status string, rowVersion int) error
	del(ctx context.Context, tx pgx.Tx, tenantID, id string, rowVersion int) error
}

// Repository implements ports.Repository over pgx.
type Repository struct {
	pool    *pgxpool.Pool
	timeout time.Duration
	stores  map[string]store
	now     func() time.Time
	// readInvalidator drops this process's cached analytics reads after a register write:
	// parks, pens, partitions, breeds and stages are all joined by the Weights / Growth reads.
	readInvalidator readcache.Invalidator
}

// WithReadCacheInvalidator wires the process-wide analytics read cache.
func (r *Repository) WithReadCacheInvalidator(inv readcache.Invalidator) *Repository {
	r.readInvalidator = inv
	return r
}

// NewRepository constructs the repository with one store per register.
func NewRepository(pool *pgxpool.Pool, timeout time.Duration) *Repository {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	r := &Repository{pool: pool, timeout: timeout, now: time.Now}
	r.stores = map[string]store{
		domain.RegParks:             parkStore{},
		domain.RegPenTypes:          penTypeStore,
		domain.RegPens:              penStore{},
		domain.RegPartitions:        partitionStore{},
		domain.RegSpecies:           codeLookupStore{register: domain.RegSpecies, table: "species_lookup", codeCol: "species_code", goatCol: "species", breedCol: "species"},
		domain.RegBreeds:            breedStore{},
		domain.RegSexes:             codeLookupStore{register: domain.RegSexes, table: "sex_lookup", codeCol: "sex_code", goatCol: "sex"},
		domain.RegStages:            stageStore{},
		domain.RegCategories:        categoryStore{},
		domain.RegItems:             itemStore{},
		domain.RegFeedItems:         feedItemStore{},
		domain.RegRoles:             roleStore{},
		domain.RegAnimals:           animalStore{},
		domain.RegStatusDefinitions: statusDefinitionStore{},
		domain.RegSOPCategories:     sopCategoryStore,
		domain.RegTaskTypes:         taskTypeStore,
		domain.RegReferenceLists:    referenceListStore,
	}
	for _, reg := range domain.Registers {
		if _, ok := r.stores[reg.Key]; !ok {
			panic("configuration: register without a store: " + reg.Key)
		}
	}
	return r
}

func (r *Repository) storeFor(register string) (store, error) {
	if domain.IsReferenceRegister(register) {
		return referenceEntryStore(domain.ReferenceListKey(register)), nil
	}
	s, ok := r.stores[register]
	if !ok {
		return nil, domain.ErrUnknownRegister
	}
	return s, nil
}

// Counts is the active-row count of every register, in one round trip per register.
func (r *Repository) Counts(ctx context.Context, tenantID string) (map[string]int, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	out := make(map[string]int, len(r.stores))
	for key, s := range r.stores {
		n, err := s.count(ctx, r.pool, tenantID)
		if err != nil {
			return nil, fmt.Errorf("configuration: count %s: %w", key, err)
		}
		out[key] = n
	}
	lists, err := r.ReferenceLists(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	for _, list := range lists {
		n, err := referenceEntryStore(list.Key).count(ctx, r.pool, tenantID)
		if err != nil {
			return nil, fmt.Errorf("configuration: count %s: %w", list.Key, err)
		}
		out[domain.RefPrefix+list.Key] = n
	}
	return out, nil
}

// List is one page of a register.
func (r *Repository) List(ctx context.Context, tenantID, register string, p ports.ListParams) (ports.Page, error) {
	s, err := r.storeFor(register)
	if err != nil {
		return ports.Page{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	page, err := s.list(ctx, r.pool, tenantID, p)
	if err != nil {
		return ports.Page{}, fmt.Errorf("configuration: list %s: %w", register, err)
	}
	for i := range page.Rows {
		page.Rows[i].Register = register
	}
	return page, nil
}

// Get is one row.
// CategoryKind is the effective kind of a category, read through the cheap recursive walk.
func (r *Repository) CategoryKind(ctx context.Context, tenantID, id string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	return categoryKind(ctx, r.pool, tenantID, id)
}

func (r *Repository) Get(ctx context.Context, tenantID, register, id string) (domain.Row, error) {
	s, err := r.storeFor(register)
	if err != nil {
		return domain.Row{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	row, err := s.get(ctx, r.pool, tenantID, id)
	if err != nil {
		return domain.Row{}, err
	}
	row.Register = register
	return row, nil
}

// Options is every active row as a ref choice.
func (r *Repository) Options(ctx context.Context, tenantID, register string) ([]ports.RefOption, error) {
	s, err := r.storeFor(register)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	return s.options(ctx, r.pool, tenantID)
}

// Usage is what still names the row.
func (r *Repository) Usage(ctx context.Context, tenantID, register, id string) (domain.Usage, error) {
	s, err := r.storeFor(register)
	if err != nil {
		return domain.Usage{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	return s.usage(ctx, r.pool, tenantID, id)
}

// Create inserts a row under the caller's idempotency key.
func (r *Repository) Create(ctx context.Context, w ports.WriteParams, register string, fields map[string]any) (domain.Row, error) {
	s, err := r.storeFor(register)
	if err != nil {
		return domain.Row{}, err
	}
	return r.write(ctx, w, register, "", fingerprint("create", register, fields, 0), func(ctx context.Context, tx pgx.Tx) (string, error) {
		id, err := s.insert(ctx, tx, w.TenantID, fields)
		if err != nil {
			return "", mapWriteError(err)
		}
		after, err := s.get(ctx, tx, w.TenantID, id)
		if err != nil {
			return "", err
		}
		return id, recordAudit(ctx, tx, w, register+".created", register, id, nil, after)
	})
}

// Update applies the changed fields, fenced on row_version.
func (r *Repository) Update(ctx context.Context, w ports.WriteParams, register, id string, fields map[string]any, rowVersion int) (domain.Row, error) {
	s, err := r.storeFor(register)
	if err != nil {
		return domain.Row{}, err
	}
	return r.write(ctx, w, register, id, fingerprint("update", register+"/"+id, fields, rowVersion), func(ctx context.Context, tx pgx.Tx) (string, error) {
		before, err := s.get(ctx, tx, w.TenantID, id)
		if err != nil {
			return "", err
		}
		newID, err := s.update(ctx, tx, w.TenantID, id, fields, rowVersion)
		if err != nil {
			return "", mapWriteError(err)
		}
		if newID != "" {
			id = newID
		}
		after, err := s.get(ctx, tx, w.TenantID, id)
		if err != nil {
			return "", err
		}
		return after.ID, recordAudit(ctx, tx, w, register+".updated", register, after.ID, before, after)
	})
}

// SetStatus archives or restores, refusing an archive while other rows still name this one.
func (r *Repository) SetStatus(ctx context.Context, w ports.WriteParams, register, id, status string, rowVersion int) (domain.Row, error) {
	s, err := r.storeFor(register)
	if err != nil {
		return domain.Row{}, err
	}
	return r.write(ctx, w, register, id, fingerprint("status:"+status, register+"/"+id, nil, rowVersion), func(ctx context.Context, tx pgx.Tx) (string, error) {
		before, err := s.get(ctx, tx, w.TenantID, id)
		if err != nil {
			return "", err
		}
		if status == domain.StatusArchived && before.IsBuiltin {
			// Re-checked inside the transaction: the service's check runs on a read taken before
			// it, and a sheet import reaches here without it.
			return "", domain.ErrBuiltin
		}
		if status == domain.StatusArchived {
			u, err := s.usage(ctx, tx, w.TenantID, id)
			if err != nil {
				return "", err
			}
			if u.Blocked {
				return "", &ports.InUseError{Usage: u}
			}
		}
		if err := s.setStatus(ctx, tx, w.TenantID, id, status, rowVersion); err != nil {
			return "", mapWriteError(err)
		}
		after, err := s.get(ctx, tx, w.TenantID, id)
		if err != nil {
			return "", err
		}
		return after.ID, recordAudit(ctx, tx, w, register+"."+status, register, after.ID, before, after)
	})
}

// Delete removes a row nothing names; the usage check runs inside the same transaction as the
// delete so a concurrent write cannot slip a dependent in between.
func (r *Repository) Delete(ctx context.Context, w ports.WriteParams, register, id string, rowVersion int) error {
	s, err := r.storeFor(register)
	if err != nil {
		return err
	}
	_, err = r.write(ctx, w, register, id, fingerprint("delete", register+"/"+id, nil, rowVersion), func(ctx context.Context, tx pgx.Tx) (string, error) {
		before, err := s.get(ctx, tx, w.TenantID, id)
		if err != nil {
			return "", err
		}
		if before.IsBuiltin {
			return "", domain.ErrBuiltin
		}
		u, err := s.usage(ctx, tx, w.TenantID, id)
		if err != nil {
			return "", err
		}
		if u.Blocked {
			return "", &ports.InUseError{Usage: u}
		}
		if err := s.del(ctx, tx, w.TenantID, id, rowVersion); err != nil {
			return "", mapWriteError(err)
		}
		return "", recordAudit(ctx, tx, w, register+".deleted", register, id, before, nil)
	})
	return err
}

// write runs one register write: reserve the idempotency key with the request fingerprint,
// run the body, complete the key, commit. An exact replay returns the stored result id and
// re-reads the row; a same-key different-payload replay is refused.
func (r *Repository) write(ctx context.Context, w ports.WriteParams, register, id, fp string, body func(context.Context, pgx.Tx) (string, error)) (domain.Row, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.Row{}, fmt.Errorf("configuration: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	res, err := reserveIdempotency(ctx, tx, w.TenantID, w.IdempotencyKey, fp)
	if err != nil {
		return domain.Row{}, err
	}
	if !res.proceed {
		if err := tx.Commit(ctx); err != nil {
			return domain.Row{}, err
		}
		if res.resultID == "" {
			return domain.Row{}, nil
		}
		return r.Get(ctx, w.TenantID, register, res.resultID)
	}
	resultID, err := body(ctx, tx)
	if err != nil {
		return domain.Row{}, err
	}
	if err := completeIdempotency(ctx, tx, w.TenantID, w.IdempotencyKey, register, resultID); err != nil {
		return domain.Row{}, err
	}
	// Tenant-wide: a place or animal-vocabulary edit can change any park's cached analytics.
	if err := readcache.CommitAndEvict(ctx, tx, r.readInvalidator, w.TenantID); err != nil {
		return domain.Row{}, fmt.Errorf("configuration: commit: %w", err)
	}
	if resultID == "" {
		return domain.Row{}, nil
	}
	return r.Get(ctx, w.TenantID, register, resultID)
}

func recordAudit(ctx context.Context, tx pgx.Tx, w ports.WriteParams, action, register, id string, before, after any) error {
	var beforeState, afterState any
	if b, ok := before.(domain.Row); ok {
		beforeState = auditState(b)
	}
	if a, ok := after.(domain.Row); ok {
		afterState = auditState(a)
	}
	if err := audit.NewTxRecorder(tx).Record(ctx, audit.Event{
		TenantID:     w.TenantID,
		ActorID:      w.ActorID,
		ActorType:    "human",
		Action:       "configuration." + action,
		ResourceType: "configuration_" + register,
		// A partition's id is shed_id:label, not a uuid; the audit column is uuid, so the row id
		// always rides metadata and the column carries only a real uuid.
		ResourceID:  uuidOrBlank(id),
		ScopeType:   "tenant",
		ScopeID:     w.TenantID,
		BeforeState: beforeState,
		AfterState:  afterState,
		TraceID:     w.TraceID,
		Metadata:    map[string]any{"domain": auditDomain, "module": auditDomain, "register": register, "row_id": id, "idempotency_key": w.IdempotencyKey, "operation_id": w.IdempotencyKey},
	}); err != nil {
		return fmt.Errorf("configuration: audit %s: %w", action, err)
	}
	return nil
}

func isUUID(s string) bool {
	return len(s) == 36 && strings.Count(s, "-") == 4
}

func uuidOrBlank(s string) string {
	if isUUID(s) {
		return s
	}
	return ""
}

func auditState(row domain.Row) map[string]any {
	return map[string]any{"id": row.ID, "display": row.Display, "status": row.Status, "row_version": row.RowVersion, "fields": row.Fields}
}

// fingerprint hashes what defines the write's effect: the verb, the target and the fields in
// key order, plus the version fence.
func fingerprint(verb, target string, fields map[string]any, rowVersion int) string {
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString(verb)
	b.WriteString("\x1f")
	b.WriteString(target)
	b.WriteString("\x1f")
	b.WriteString(fmt.Sprint(rowVersion))
	for _, k := range keys {
		b.WriteString("\x1f")
		b.WriteString(k)
		b.WriteString("=")
		enc, _ := json.Marshal(fields[k])
		b.Write(enc)
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

type idemReservation struct {
	proceed  bool
	resultID string
}

func reserveIdempotency(ctx context.Context, tx pgx.Tx, tenantID, key, fp string) (idemReservation, error) {
	scoped := tenantID + ":" + idemScope + ":" + strings.TrimSpace(key)
	var claimed string
	err := tx.QueryRow(ctx, sqlIdempotencyReserve, scoped, tenantID, idemScope, fp).Scan(&claimed)
	if err == nil {
		return idemReservation{proceed: true}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return idemReservation{}, err
	}
	var existingHash, status, resultType, resultID string
	if err := tx.QueryRow(ctx, sqlIdempotencyRead, scoped).Scan(&existingHash, &status, &resultType, &resultID); err != nil {
		return idemReservation{}, err
	}
	if existingHash != fp {
		return idemReservation{}, ports.ErrIdempotencyConflict
	}
	if resultID == "" {
		if _, rest, ok := strings.Cut(resultType, ":"); ok {
			resultID = rest
		}
	}
	return idemReservation{proceed: false, resultID: resultID}, nil
}

func completeIdempotency(ctx context.Context, tx pgx.Tx, tenantID, key, resultType, resultID string) error {
	scoped := tenantID + ":" + idemScope + ":" + strings.TrimSpace(key)
	// Composite ids (a partition is shed_id:label) are not uuids: result_type carries the
	// register and, for those, the whole id so a replay can still re-read the row.
	var stored *string
	if isUUID(resultID) {
		stored = &resultID
	} else if resultID != "" {
		resultType = resultType + ":" + resultID
	}
	_, err := tx.Exec(ctx, sqlIdempotencyComplete, scoped, resultType, stored)
	return err
}

// mapWriteError turns a unique violation into a duplicate the drawer can show on its field.
func mapWriteError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505":
			field, msg := duplicateFor(pgErr.ConstraintName)
			return &ports.DuplicateError{Field: field, Message: msg}
		case "23503":
			return &ports.RefError{Field: "", Label: "record"}
		case "23514":
			return &ports.DuplicateError{Field: "", Message: "That value is not allowed here."}
		}
	}
	return err
}

func duplicateFor(constraint string) (string, string) {
	switch {
	case strings.Contains(constraint, "code"):
		return "code", "That code is already used."
	case strings.Contains(constraint, "sibling_name"), strings.Contains(constraint, "name"):
		return "name", "A record with that name already exists."
	case strings.Contains(constraint, "shed_partitions_pkey"):
		return "label", "This pen already has that partition."
	}
	return "name", "A record like that already exists."
}

// SQL hoisted to package level so the scale guard and query-plan tests can reach it.
const (
	sqlIdempotencyReserve = `
INSERT INTO idempotency_keys (idempotency_key, tenant_id, scope, request_hash, status)
VALUES ($1, $2::uuid, $3, $4, 'started')
ON CONFLICT (idempotency_key) DO NOTHING
RETURNING idempotency_key`
	sqlIdempotencyRead = `
SELECT request_hash, status, COALESCE(result_type, ''), COALESCE(result_id::text, '')
FROM idempotency_keys
WHERE idempotency_key = $1`
	sqlIdempotencyComplete = `
UPDATE idempotency_keys
SET status = 'completed', result_type = $2, result_id = $3::uuid, completed_at = now()
WHERE idempotency_key = $1`
)
