package postgres

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/health/domain"
	"github.com/vgoats/goatos/backend/internal/health/ports"
	"github.com/vgoats/goatos/backend/internal/platform/audit"
	"github.com/vgoats/goatos/backend/internal/platform/oploc"
	platformoutbox "github.com/vgoats/goatos/backend/internal/platform/outbox"
)

const healthTopic = "health.events"

type Repository struct {
	pool    *pgxpool.Pool
	timeout time.Duration
	now     func() time.Time
}

func NewRepository(pool *pgxpool.Pool, timeout time.Duration) *Repository {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &Repository{pool: pool, timeout: timeout, now: time.Now}
}

var _ ports.Repository = (*Repository)(nil)

type protocolRow struct {
	id, diseaseKey, diseaseName, ageBand string
	duration                             int
	steps                                []domain.ProtocolStep
}

func (r *Repository) OpenCase(ctx context.Context, in domain.OpenCaseInput) (domain.OpenCaseResult, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return domain.OpenCaseResult{}, fmt.Errorf("health: begin open case: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	var lifecycle, goatAgeBand string
	var parkID, shedID, partitionLabel *string
	err = tx.QueryRow(ctx, `
SELECT g.lifecycle_status, coalesce(g.age_band,''), g.park_id::text, g.shed_id::text, gsp.partition_label
FROM goats g
LEFT JOIN goat_shed_partitions gsp ON gsp.tenant_id = g.tenant_id AND gsp.goat_id = g.goat_id AND gsp.shed_id = g.shed_id
WHERE g.tenant_id=$1::uuid AND g.goat_id=$2::uuid
FOR SHARE OF g`, in.TenantID, in.GoatID).Scan(&lifecycle, &goatAgeBand, &parkID, &shedID, &partitionLabel)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.OpenCaseResult{}, ports.ErrNotFound
	}
	if err != nil {
		return domain.OpenCaseResult{}, fmt.Errorf("health: read goat: %w", err)
	}
	if lifecycle != "alive" {
		return domain.OpenCaseResult{}, ports.ErrGoatNotAlive
	}
	if goatAgeBand != in.AgeBand {
		return domain.OpenCaseResult{}, ports.ErrAgeBandMismatch
	}

	var existing domain.OpenCaseResult
	var existingFingerprint string
	err = tx.QueryRow(ctx, `
SELECT hc.health_case_id::text, hc.duration_days, hc.request_fingerprint,
       COALESCE((SELECT hs.health_session_id::text FROM health_treatment_sessions hs
                 WHERE hs.health_case_id=hc.health_case_id ORDER BY hs.due_at,hs.health_session_id LIMIT 1),''),
       (SELECT count(*) FROM health_treatment_sessions hs WHERE hs.health_case_id=hc.health_case_id)
FROM health_cases hc WHERE hc.tenant_id=$1::uuid AND hc.idempotency_key=$2`,
		in.TenantID, in.IdempotencyKey).Scan(&existing.CaseID, &existing.DurationDays, &existingFingerprint, &existing.FirstSessionID, &existing.SessionCount)
	if err == nil {
		if existingFingerprint != in.RequestFingerprint {
			return domain.OpenCaseResult{}, ports.ErrConflict
		}
		existing.IdempotentReplay = true
		if err := tx.Commit(ctx); err != nil {
			return domain.OpenCaseResult{}, err
		}
		committed = true
		return existing, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return domain.OpenCaseResult{}, fmt.Errorf("health: check case idempotency: %w", err)
	}

	p, err := loadPublishedProtocol(ctx, tx, in.TenantID, in.DiseaseKey, in.AgeBand)
	if err != nil {
		return domain.OpenCaseResult{}, err
	}
	var caseID string
	err = tx.QueryRow(ctx, `
INSERT INTO health_cases (
 tenant_id,goat_id,health_protocol_version_id,disease_key,disease_name,age_band,start_date,
 duration_days,status,park_id,shed_id,partition_label,diagnosed_by,idempotency_key,request_fingerprint
) VALUES ($1::uuid,$2::uuid,$3::uuid,$4,$5,$6,$7::date,$8,'active',
 nullif($9,'')::uuid,nullif($10,'')::uuid,nullif($11,''),$12::uuid,$13,$14)
RETURNING health_case_id::text`, in.TenantID, in.GoatID, p.id, p.diseaseKey, p.diseaseName, p.ageBand,
		in.StartDate.Format("2006-01-02"), p.duration, valueOrEmpty(parkID), valueOrEmpty(shedID), valueOrEmpty(partitionLabel), in.ActorID, in.IdempotencyKey, in.RequestFingerprint).Scan(&caseID)
	if err != nil {
		return domain.OpenCaseResult{}, fmt.Errorf("health: insert case: %w", err)
	}

	steps := p.steps
	if len(steps) == 0 {
		for day := 1; day <= p.duration; day++ {
			instruction := "Follow the configured " + p.diseaseName + " treatment protocol."
			steps = append(steps, domain.ProtocolStep{DayNo: day, Session: domain.SessionUnscheduled, Seq: day, RecordType: "action", Instruction: &instruction})
		}
	}
	type sessionKey struct {
		day     int
		session string
	}
	grouped := map[sessionKey][]domain.ProtocolStep{}
	for _, step := range steps {
		grouped[sessionKey{step.DayNo, normalizeSession(step.Session)}] = append(grouped[sessionKey{step.DayNo, normalizeSession(step.Session)}], step)
	}
	keys := make([]sessionKey, 0, len(grouped))
	for k := range grouped {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].day != keys[j].day {
			return keys[i].day < keys[j].day
		}
		return sessionRank(keys[i].session) < sessionRank(keys[j].session)
	})
	loc, _ := time.LoadLocation("Asia/Kolkata")
	startY, startM, startD := in.StartDate.In(loc).Date()
	firstSession := ""
	var courseBatch pgx.Batch
	for _, key := range keys {
		date := time.Date(startY, startM, startD, 0, 0, 0, 0, loc).AddDate(0, 0, key.day-1)
		hour := sessionHour(key.session)
		due := time.Date(date.Year(), date.Month(), date.Day(), hour, 0, 0, 0, loc)
		status := "scheduled"
		if !due.After(r.now()) {
			status = "due"
		}
		sessionID := uuid.NewString()
		courseBatch.Queue(`
INSERT INTO health_treatment_sessions
 (health_session_id,tenant_id,health_case_id,goat_id,day_no,business_date,session,due_at,status)
VALUES ($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5,$6::date,$7,$8,$9)`, sessionID, in.TenantID, caseID, in.GoatID, key.day, date.Format("2006-01-02"), key.session, due, status)
		if firstSession == "" {
			firstSession = sessionID
		}
		for _, step := range grouped[key] {
			stepStatus := "pending"
			if step.RecordType == "critical_action" {
				stepStatus = "guarded"
			}
			courseBatch.Queue(`
INSERT INTO health_session_steps (
 tenant_id,health_session_id,source_protocol_step_id,seq,record_type,medicine_name,dosage_text,
 dosage_denominator,medicine_route,instruction,critical_action_type,status
) VALUES ($1::uuid,$2::uuid,nullif($3,'')::uuid,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
				in.TenantID, sessionID, step.StepID, step.Seq, step.RecordType, step.MedicineName, step.DosageText,
				step.DosageDenominator, step.MedicineRoute, step.Instruction, step.CriticalActionType, stepStatus)
		}
	}
	if err := tx.SendBatch(ctx, &courseBatch).Close(); err != nil {
		return domain.OpenCaseResult{}, fmt.Errorf("health: snapshot treatment course: %w", err)
	}
	if err := audit.NewTxRecorder(tx).Record(ctx, audit.Event{TenantID: in.TenantID, ActorID: in.ActorID, ActorType: "operator",
		Action: "health.case.opened", ResourceType: "health_case", ResourceID: caseID, ScopeType: "goat", ScopeID: in.GoatID,
		AfterState: map[string]any{"disease_key": p.diseaseKey, "age_band": p.ageBand, "duration_days": p.duration, "session_count": len(keys)}, TraceID: in.TraceID}); err != nil {
		return domain.OpenCaseResult{}, err
	}
	if err := insertHealthOutbox(ctx, tx, in.TenantID, in.ActorID, "health.case.opened", caseID, in.GoatID, in.TraceID,
		map[string]any{"case_id": caseID, "goat_id": in.GoatID, "disease_key": p.diseaseKey, "age_band": p.ageBand, "duration_days": p.duration, "session_count": len(keys)}); err != nil {
		return domain.OpenCaseResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.OpenCaseResult{}, fmt.Errorf("health: commit case: %w", err)
	}
	committed = true
	return domain.OpenCaseResult{CaseID: caseID, FirstSessionID: firstSession, SessionCount: len(keys), DurationDays: p.duration}, nil
}

func loadPublishedProtocol(ctx context.Context, tx pgx.Tx, tenantID, diseaseKey, ageBand string) (protocolRow, error) {
	var p protocolRow
	err := tx.QueryRow(ctx, `SELECT health_protocol_version_id::text,disease_key,display_name,age_band,duration_days
FROM health_protocol_versions WHERE tenant_id=$1::uuid AND disease_key=$2 AND age_band=$3 AND status='published'`,
		tenantID, diseaseKey, ageBand).Scan(&p.id, &p.diseaseKey, &p.diseaseName, &p.ageBand, &p.duration)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, ports.ErrProtocolNotPublished
	}
	if err != nil {
		return p, fmt.Errorf("health: load protocol: %w", err)
	}
	rows, err := tx.Query(ctx, `SELECT health_protocol_step_id::text,day_no,session,seq,record_type,medicine_name,dosage_text,dosage_denominator,medicine_route,instruction,critical_action_type
FROM health_protocol_steps WHERE health_protocol_version_id=$1::uuid ORDER BY day_no,session,seq`, p.id)
	if err != nil {
		return p, fmt.Errorf("health: load protocol steps: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var s domain.ProtocolStep
		if err := rows.Scan(&s.StepID, &s.DayNo, &s.Session, &s.Seq, &s.RecordType, &s.MedicineName, &s.DosageText, &s.DosageDenominator, &s.MedicineRoute, &s.Instruction, &s.CriticalActionType); err != nil {
			return p, err
		}
		p.steps = append(p.steps, s)
	}
	return p, rows.Err()
}

// projection-review: membership=health_treatment_sessions, unique on health_session_id, joined 1:1 to its owning health_cases row; group_key=health_session_id -- step_counts groups on exactly that key and is joined back 1:1, so a session with many steps stays ONE page row; join_cardinality=health_cases, goats and both locations lookups are 1:1 on their tenant-scoped primary keys and only label the row; step_counts is pre-aggregated to one row per health_session_id BEFORE it is joined, which is what stops the steps fan-out; pagination=keyset on (due_at, health_session_id) with LIMIT n+1, and the summary is a separate whole-filter aggregate, never a rollup of the returned page; scope=park/shed, applied from the caller's clamped filters on health_cases
func (r *Repository) ListWorkItems(ctx context.Context, f domain.ListFilter) (domain.WorkItemPage, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	cursorAt, cursorID, err := decodeCursor(f.Cursor)
	if err != nil {
		return domain.WorkItemPage{}, ports.ErrConflict
	}
	args := []any{f.TenantID, f.Date, f.AgeBand, f.Status, f.DiseaseKey, f.ParkID, f.ShedID, f.Session, cursorAt, cursorID, f.Limit + 1}
	// scale-guard:ignore: one selected business-day + age-band keyset page, capped at 21 and covered by health_sessions_worklist_idx; CTE page first prevents the step join from widening the page.
	rows, err := r.pool.Query(ctx, `
WITH page AS (
 SELECT hs.health_session_id,hs.health_case_id,hs.goat_id,hs.day_no,hs.business_date,hs.session,hs.due_at,
        CASE WHEN hs.status='scheduled' AND hs.due_at<=now() THEN 'due' ELSE hs.status END AS effective_status
 FROM health_treatment_sessions hs JOIN health_cases hc ON hc.tenant_id=hs.tenant_id AND hc.health_case_id=hs.health_case_id
 WHERE hs.tenant_id=$1::uuid AND hs.business_date=$2::date AND hc.age_band=$3
   AND ($4='canceled_death' OR hs.status<>'canceled_death')
   AND ($4='' OR (CASE WHEN hs.status='scheduled' AND hs.due_at<=now() THEN 'due' ELSE hs.status END)=$4)
   AND ($5='' OR hc.disease_key=$5) AND ($6='' OR hc.park_id=nullif($6,'')::uuid)
   AND ($7='' OR hc.shed_id=nullif($7,'')::uuid) AND ($8='' OR hs.session=$8)
   AND ($9::timestamptz IS NULL OR (hs.due_at,hs.health_session_id) > ($9::timestamptz,nullif($10,'')::uuid))
 ORDER BY hs.due_at,hs.health_session_id LIMIT $11
), step_counts AS (
 SELECT ss.health_session_id,count(*)::int AS step_count,
        count(*) FILTER (WHERE ss.record_type='medication')::int AS medication_count,
        bool_or(ss.record_type='critical_action') AS has_critical
 FROM health_session_steps ss JOIN page p ON p.health_session_id=ss.health_session_id GROUP BY ss.health_session_id
)
SELECT p.health_session_id::text,p.health_case_id::text,p.goat_id::text,g.display_id,hc.disease_key,hc.disease_name,hc.age_band,
 p.day_no,hc.duration_days,p.business_date::text,p.session,p.due_at,p.effective_status,
 coalesce(hc.park_id::text,''),coalesce(pl.name,''),coalesce(hc.shed_id::text,''),coalesce(sl.name,''),coalesce(hc.partition_label,''),
 coalesce(sc.step_count,0),coalesce(sc.medication_count,0),coalesce(sc.has_critical,false)
FROM page p JOIN health_cases hc ON hc.health_case_id=p.health_case_id JOIN goats g ON g.goat_id=p.goat_id
LEFT JOIN locations pl ON pl.tenant_id=hc.tenant_id AND pl.location_id=hc.park_id
LEFT JOIN locations sl ON sl.tenant_id=hc.tenant_id AND sl.location_id=hc.shed_id
LEFT JOIN step_counts sc ON sc.health_session_id=p.health_session_id
ORDER BY p.due_at,p.health_session_id`, args...)
	if err != nil {
		return domain.WorkItemPage{}, fmt.Errorf("health: list work items: %w", err)
	}
	defer rows.Close()
	page := domain.WorkItemPage{Items: []domain.WorkItem{}, DateMarkers: []domain.DateMarker{}, FilterOptions: domain.FilterOptions{Diseases: []domain.FilterOption{}, Parks: []domain.FilterOption{}, Sheds: []domain.FilterOption{}}}
	for rows.Next() {
		var it domain.WorkItem
		var park, shed string
		if err := rows.Scan(&it.SessionID, &it.CaseID, &it.GoatID, &it.GoatDisplayID, &it.DiseaseKey, &it.DiseaseName, &it.AgeBand, &it.DayNo, &it.DurationDays, &it.BusinessDate, &it.Session, &it.DueAt, &it.Status, &park, &it.ParkLabel, &shed, &it.ShedLabel, &it.PartitionLabel, &it.StepCount, &it.MedicationCount, &it.HasCriticalStep); err != nil {
			return page, err
		}
		if park != "" {
			it.ParkID = &park
		}
		if shed != "" {
			it.ShedID = &shed
		}
		if it.Status == "held_death_review" {
			it.Status = "held"
		}
		if it.ShedLabel != "" {
			it.OperationalLocationDisplay = oploc.OperationalLocation{ShedID: shed, ShedName: it.ShedLabel, PartitionLabel: it.PartitionLabel}.Display()
		}
		page.Items = append(page.Items, it)
	}
	if err := rows.Err(); err != nil {
		return page, err
	}
	if len(page.Items) > f.Limit {
		last := page.Items[f.Limit-1]
		next := encodeCursor(last.DueAt, last.SessionID)
		page.NextCursor = &next
		page.Items = page.Items[:f.Limit]
	}
	if err := r.loadSummary(ctx, f, &page.Summary); err != nil {
		return page, err
	}
	if err := r.loadMarkers(ctx, f, &page.DateMarkers); err != nil {
		return page, err
	}
	if err := r.loadFilterOptions(ctx, f, &page.FilterOptions); err != nil {
		return page, err
	}
	return page, nil
}

// projection-review: membership=health_treatment_sessions for the filtered day, unique on health_session_id; group_key=health_session_id collapsed into disjoint status buckets by a single CASE, so a session lands in exactly one bucket and the buckets sum to Total; join_cardinality=health_cases is 1:1 on health_case_id and contributes only filter columns, so no session is counted twice; pagination=none by design -- this is a whole-filter aggregate computed independently of the page so paging can never change the counts; scope=park/shed from the caller's clamped filters, identical to the ones ListWorkItems applies
func (r *Repository) loadSummary(ctx context.Context, f domain.ListFilter, out *domain.Summary) error {
	return r.pool.QueryRow(ctx, `
SELECT count(*) FILTER(WHERE effective_status<>'canceled_death')::int,
 count(*) FILTER(WHERE effective_status='due')::int,count(*) FILTER(WHERE effective_status='scheduled')::int,
 count(*) FILTER(WHERE effective_status='in_progress')::int,count(*) FILTER(WHERE effective_status='completed')::int,
 count(*) FILTER(WHERE effective_status='rework')::int,count(*) FILTER(WHERE effective_status='held_death_review')::int,
 count(*) FILTER(WHERE effective_status='canceled_death')::int
FROM (SELECT CASE WHEN hs.status='scheduled' AND hs.due_at<=now() THEN 'due' ELSE hs.status END effective_status
 FROM health_treatment_sessions hs JOIN health_cases hc ON hc.tenant_id=hs.tenant_id AND hc.health_case_id=hs.health_case_id
 WHERE hs.tenant_id=$1::uuid AND hs.business_date=$2::date AND hc.age_band=$3
 AND ($4='' OR hc.disease_key=$4) AND ($5='' OR hc.park_id=nullif($5,'')::uuid)
 AND ($6='' OR hc.shed_id=nullif($6,'')::uuid) AND ($7='' OR hs.session=$7)) q`,
		f.TenantID, f.Date, f.AgeBand, f.DiseaseKey, f.ParkID, f.ShedID, f.Session).Scan(&out.Total, &out.Due, &out.Scheduled, &out.InProgress, &out.Completed, &out.Rework, &out.Held, &out.CanceledDeath)
}

// projection-review: membership=health_treatment_sessions in the requested calendar month, unique on health_session_id; group_key=business_date -- one marker per day, which is the calendar's grain; join_cardinality=health_cases is 1:1 on health_case_id and only supplies age_band, so it cannot multiply a day's count; pagination=none, the month is the bound; scope=tenant+age_band, matching the calendar the markers are drawn on
func (r *Repository) loadMarkers(ctx context.Context, f domain.ListFilter, out *[]domain.DateMarker) error {
	date, _ := time.Parse("2006-01-02", f.Date)
	from := time.Date(date.Year(), date.Month(), 1, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 1, 0)
	rows, err := r.pool.Query(ctx, `SELECT hs.business_date::text,count(*)::int FROM health_treatment_sessions hs
JOIN health_cases hc ON hc.tenant_id=hs.tenant_id AND hc.health_case_id=hs.health_case_id
WHERE hs.tenant_id=$1::uuid AND hc.age_band=$2 AND hs.business_date >= $3::date AND hs.business_date < $4::date
AND hs.status NOT IN ('completed','canceled_death') GROUP BY hs.business_date ORDER BY hs.business_date`, f.TenantID, f.AgeBand, from.Format("2006-01-02"), to.Format("2006-01-02"))
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var m domain.DateMarker
		if err := rows.Scan(&m.Date, &m.Count); err != nil {
			return err
		}
		*out = append(*out, m)
	}
	return rows.Err()
}
func (r *Repository) loadFilterOptions(ctx context.Context, f domain.ListFilter, out *domain.FilterOptions) error {
	// projection-review: membership=health_protocol_versions for the disease list (unique on (tenant_id,disease_key,age_band) WHERE status='published') and health_cases for the park/shed lists; group_key=(disease_key) for diseases and the canonical park_id / shed_id FK for locations, which is the exact grain each option list is keyed by; join_cardinality=locations joins 1:1 on (tenant_id,location_id) and only supplies a display name, so an option cannot appear twice; pagination=none, these are bounded option lists rendered whole; scope=tenant+age_band -- no ratio, numerator or denominator is compared here, these are distinct option sets
	rows, err := r.pool.Query(ctx, `
SELECT 'disease',hp.disease_key,hp.display_name FROM health_protocol_versions hp WHERE hp.tenant_id=$1::uuid AND hp.age_band=$2 AND hp.status='published'
UNION ALL SELECT 'park',coalesce(hc.park_id::text,''),coalesce(l.name,'') FROM health_cases hc LEFT JOIN locations l ON l.tenant_id=hc.tenant_id AND l.location_id=hc.park_id WHERE hc.tenant_id=$1::uuid AND hc.age_band=$2 AND hc.park_id IS NOT NULL GROUP BY hc.park_id,l.name
UNION ALL SELECT 'shed',coalesce(hc.shed_id::text,''),coalesce(l.name,'') FROM health_cases hc LEFT JOIN locations l ON l.tenant_id=hc.tenant_id AND l.location_id=hc.shed_id WHERE hc.tenant_id=$1::uuid AND hc.age_band=$2 AND hc.shed_id IS NOT NULL GROUP BY hc.shed_id,l.name
ORDER BY 1,3,2`, f.TenantID, f.AgeBand)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var kind string
		var o domain.FilterOption
		if err := rows.Scan(&kind, &o.Key, &o.Label); err != nil {
			return err
		}
		switch kind {
		case "disease":
			out.Diseases = append(out.Diseases, o)
		case "park":
			out.Parks = append(out.Parks, o)
		case "shed":
			out.Sheds = append(out.Sheds, o)
		}
	}
	return rows.Err()
}

func (r *Repository) GetWorkItem(ctx context.Context, tenantID, sessionID string) (domain.WorkItemDetail, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	var d domain.WorkItemDetail
	var park, shed string
	err := r.pool.QueryRow(ctx, `SELECT hs.health_session_id::text,hc.health_case_id::text,hs.goat_id::text,g.display_id,hc.disease_key,hc.disease_name,hc.age_band,hs.day_no,hc.duration_days,hs.business_date::text,hs.session,hs.due_at,
CASE WHEN hs.status='scheduled' AND hs.due_at<=now() THEN 'due' ELSE hs.status END,coalesce(hc.park_id::text,''),coalesce(pl.name,''),coalesce(hc.shed_id::text,''),coalesce(sl.name,''),coalesce(hc.partition_label,'')
FROM health_treatment_sessions hs JOIN health_cases hc ON hc.tenant_id=hs.tenant_id AND hc.health_case_id=hs.health_case_id JOIN goats g ON g.goat_id=hs.goat_id
LEFT JOIN locations pl ON pl.tenant_id=hc.tenant_id AND pl.location_id=hc.park_id LEFT JOIN locations sl ON sl.tenant_id=hc.tenant_id AND sl.location_id=hc.shed_id
WHERE hs.tenant_id=$1::uuid AND hs.health_session_id=$2::uuid`, tenantID, sessionID).Scan(&d.SessionID, &d.CaseID, &d.GoatID, &d.GoatDisplayID, &d.DiseaseKey, &d.DiseaseName, &d.AgeBand, &d.DayNo, &d.DurationDays, &d.BusinessDate, &d.Session, &d.DueAt, &d.Status, &park, &d.ParkLabel, &shed, &d.ShedLabel, &d.PartitionLabel)
	if errors.Is(err, pgx.ErrNoRows) {
		return d, ports.ErrNotFound
	}
	if err != nil {
		return d, err
	}
	if park != "" {
		d.ParkID = &park
	}
	if shed != "" {
		d.ShedID = &shed
	}
	if d.Status == "held_death_review" {
		d.Status = "held"
	}
	if d.ShedLabel != "" {
		d.OperationalLocationDisplay = oploc.OperationalLocation{ShedID: shed, ShedName: d.ShedLabel, PartitionLabel: d.PartitionLabel}.Display()
	}
	rows, err := r.pool.Query(ctx, `SELECT health_session_step_id::text,seq,record_type,medicine_name,dosage_text,dosage_denominator,medicine_route,instruction,critical_action_type,status FROM health_session_steps WHERE tenant_id=$1::uuid AND health_session_id=$2::uuid ORDER BY seq`, tenantID, sessionID)
	if err != nil {
		return d, err
	}
	defer rows.Close()
	d.Steps = []domain.ProtocolStep{}
	for rows.Next() {
		var s domain.ProtocolStep
		if err := rows.Scan(&s.StepID, &s.Seq, &s.RecordType, &s.MedicineName, &s.DosageText, &s.DosageDenominator, &s.MedicineRoute, &s.Instruction, &s.CriticalActionType, &s.Status); err != nil {
			return d, err
		}
		s.DayNo = d.DayNo
		s.Session = d.Session
		d.Steps = append(d.Steps, s)
		if s.RecordType == "medication" {
			d.MedicationCount++
		}
		if s.RecordType == "critical_action" {
			d.HasCriticalStep = true
		}
		d.StepCount++
	}
	return d, rows.Err()
}

func (r *Repository) CompleteWorkItem(ctx context.Context, in domain.CompleteInput) (domain.CompleteResult, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return domain.CompleteResult{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()
	var caseID, goatID, disease, status string
	var completedAt *time.Time
	var priorKey, priorFingerprint *string
	err = tx.QueryRow(ctx, `SELECT hs.health_case_id::text,hs.goat_id::text,hc.disease_key,hs.status,hs.completed_at,hs.completion_idempotency_key,hs.completion_fingerprint
FROM health_treatment_sessions hs JOIN health_cases hc ON hc.health_case_id=hs.health_case_id
WHERE hs.tenant_id=$1::uuid AND hs.health_session_id=$2::uuid FOR UPDATE`, in.TenantID, in.SessionID).Scan(&caseID, &goatID, &disease, &status, &completedAt, &priorKey, &priorFingerprint)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.CompleteResult{}, ports.ErrNotFound
	}
	if err != nil {
		return domain.CompleteResult{}, err
	}
	if status == "completed" {
		if priorKey != nil && *priorKey == in.IdempotencyKey && priorFingerprint != nil && *priorFingerprint == in.RequestFingerprint {
			var count int
			_ = tx.QueryRow(ctx, `SELECT count(*) FROM health_medicine_administrations WHERE tenant_id=$1::uuid AND health_session_id=$2::uuid`, in.TenantID, in.SessionID).Scan(&count)
			if err := tx.Commit(ctx); err != nil {
				return domain.CompleteResult{}, err
			}
			committed = true
			return domain.CompleteResult{SessionID: in.SessionID, Status: "completed", CompletedAt: *completedAt, MedicationCount: count, IdempotentReplay: true}, nil
		}
		return domain.CompleteResult{}, ports.ErrConflict
	}
	if status == "held_death_review" || status == "canceled_death" {
		return domain.CompleteResult{}, ports.ErrGoatNotAlive
	}
	now := r.now().UTC()
	tag, err := tx.Exec(ctx, `UPDATE health_treatment_sessions SET status='completed',completed_by=$3::uuid,completed_at=$4,proof_ref=nullif($5,''),completion_idempotency_key=$6,completion_fingerprint=$7,row_version=row_version+1,updated_at=now() WHERE tenant_id=$1::uuid AND health_session_id=$2::uuid`, in.TenantID, in.SessionID, in.ActorID, now, in.ProofRef, in.IdempotencyKey, in.RequestFingerprint)
	if err != nil {
		return domain.CompleteResult{}, fmt.Errorf("health: complete session: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return domain.CompleteResult{}, ports.ErrConflict
	}
	_, err = tx.Exec(ctx, `UPDATE health_session_steps SET status=CASE WHEN record_type='critical_action' THEN 'guarded' ELSE 'completed' END,completed_at=CASE WHEN record_type='critical_action' THEN NULL ELSE $3::timestamptz END WHERE tenant_id=$1::uuid AND health_session_id=$2::uuid`, in.TenantID, in.SessionID, now)
	if err != nil {
		return domain.CompleteResult{}, err
	}
	tag, err = tx.Exec(ctx, `INSERT INTO health_medicine_administrations (tenant_id,health_case_id,health_session_id,health_session_step_id,goat_id,disease_key,medicine_name,dosage_text,dosage_denominator,medicine_route,administered_by,administered_at,proof_ref)
SELECT ss.tenant_id,$3::uuid,ss.health_session_id,ss.health_session_step_id,$4::uuid,$5,ss.medicine_name,ss.dosage_text,ss.dosage_denominator,ss.medicine_route,$6::uuid,$7,nullif($8,'')
FROM health_session_steps ss WHERE ss.tenant_id=$1::uuid AND ss.health_session_id=$2::uuid AND ss.record_type='medication' ON CONFLICT DO NOTHING`, in.TenantID, in.SessionID, caseID, goatID, disease, in.ActorID, now, in.ProofRef)
	if err != nil {
		return domain.CompleteResult{}, fmt.Errorf("health: record medicines: %w", err)
	}
	medCount := int(tag.RowsAffected())
	if err := audit.NewTxRecorder(tx).Record(ctx, audit.Event{TenantID: in.TenantID, ActorID: in.ActorID, ActorType: "operator", Action: "health.treatment.completed", ResourceType: "health_case", ResourceID: caseID, ScopeType: "goat", ScopeID: goatID, AfterState: map[string]any{"health_session_id": in.SessionID, "medicine_count": medCount, "critical_actions": "guarded"}, TraceID: in.TraceID}); err != nil {
		return domain.CompleteResult{}, err
	}
	if err := insertHealthOutbox(ctx, tx, in.TenantID, in.ActorID, "health.treatment.completed", caseID, goatID, in.TraceID, map[string]any{"case_id": caseID, "health_session_id": in.SessionID, "goat_id": goatID, "disease_key": disease, "medicine_count": medCount}); err != nil {
		return domain.CompleteResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.CompleteResult{}, err
	}
	committed = true
	return domain.CompleteResult{SessionID: in.SessionID, Status: "completed", CompletedAt: now, MedicationCount: medCount}, nil
}

func (r *Repository) HoldForDeathReview(ctx context.Context, tenantID, goatID string) error {
	return r.applyDeathState(ctx, tenantID, goatID, "health.case.death_held")
}
func (r *Repository) ResumeAfterDeathRejected(ctx context.Context, tenantID, goatID string) error {
	return r.applyDeathState(ctx, tenantID, goatID, "health.case.death_resumed")
}
func (r *Repository) CloseForApprovedDeath(ctx context.Context, tenantID, goatID string) error {
	return r.applyDeathState(ctx, tenantID, goatID, "health.case.closed_dead")
}
func (r *Repository) applyDeathState(ctx context.Context, tenantID, goatID, eventType string) error {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()
	var caseSQL, sessionSQL string
	switch eventType {
	case "health.case.death_held":
		caseSQL = `UPDATE health_cases SET status_before_death_hold=status,status='held_death_review',row_version=row_version+1,updated_at=now() WHERE tenant_id=$1::uuid AND goat_id=$2::uuid AND status IN ('active','continued','referred') RETURNING health_case_id::text`
		sessionSQL = `UPDATE health_treatment_sessions SET status_before_death_hold=status,status='held_death_review',row_version=row_version+1,updated_at=now() WHERE tenant_id=$1::uuid AND goat_id=$2::uuid AND status IN ('scheduled','due','in_progress','rework')`
	case "health.case.death_resumed":
		caseSQL = `UPDATE health_cases SET status=coalesce(nullif(status_before_death_hold,''),'active'),status_before_death_hold=NULL,row_version=row_version+1,updated_at=now() WHERE tenant_id=$1::uuid AND goat_id=$2::uuid AND status='held_death_review' RETURNING health_case_id::text`
		sessionSQL = `UPDATE health_treatment_sessions SET status=CASE WHEN due_at<=now() THEN 'due' ELSE coalesce(nullif(status_before_death_hold,''),'scheduled') END,status_before_death_hold=NULL,row_version=row_version+1,updated_at=now() WHERE tenant_id=$1::uuid AND goat_id=$2::uuid AND status='held_death_review'`
	case "health.case.closed_dead":
		caseSQL = `UPDATE health_cases SET status='closed_dead',status_before_death_hold=NULL,row_version=row_version+1,updated_at=now() WHERE tenant_id=$1::uuid AND goat_id=$2::uuid AND status NOT IN ('closed_dead','recovered','canceled') RETURNING health_case_id::text`
		sessionSQL = `UPDATE health_treatment_sessions SET status='canceled_death',status_before_death_hold=NULL,row_version=row_version+1,updated_at=now() WHERE tenant_id=$1::uuid AND goat_id=$2::uuid AND status NOT IN ('completed','canceled_death')`
	default:
		return fmt.Errorf("health: unsupported death lifecycle event %q", eventType)
	}
	caseRows, err := tx.Query(ctx, caseSQL, tenantID, goatID)
	if err != nil {
		return err
	}
	ids := []string{}
	for caseRows.Next() {
		var id string
		if err := caseRows.Scan(&id); err != nil {
			return err
		}
		ids = append(ids, id)
	}
	if err := caseRows.Err(); err != nil {
		return err
	}
	caseRows.Close()
	if _, err := tx.Exec(ctx, sessionSQL, tenantID, goatID); err != nil {
		return err
	}
	var outboxBatch pgx.Batch
	for _, caseID := range ids {
		args, err := healthOutboxArgs(tenantID, "", eventType, caseID, goatID, "", map[string]any{"case_id": caseID, "goat_id": goatID})
		if err != nil {
			return err
		}
		outboxBatch.Queue(insertHealthOutboxSQL, args...)
	}
	if len(ids) > 0 {
		if err := tx.SendBatch(ctx, &outboxBatch).Close(); err != nil {
			return fmt.Errorf("health: insert death lifecycle outbox: %w", err)
		}
	}
	if len(ids) > 0 {
		if err := audit.NewTxRecorder(tx).Record(ctx, audit.Event{TenantID: tenantID, ActorType: "system_rule", Action: eventType, ResourceType: "goat", ResourceID: goatID, ScopeType: "goat", ScopeID: goatID, AfterState: map[string]any{"health_case_count": len(ids)}}); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	committed = true
	return nil
}

const insertHealthOutboxSQL = `INSERT INTO outbox_messages (tenant_id,event_id,event_type,schema_version,aggregate_type,aggregate_id,topic,payload,headers,idempotency_key,trace_id,status,next_attempt_at)
VALUES ($1::uuid,$2::uuid,$3,'1.0.0','health_case',$4::uuid,$5,$6::jsonb,$7::jsonb,$8,$9,'pending',now()) ON CONFLICT DO NOTHING`

func healthOutboxArgs(tenantID, actorID, eventType, caseID, goatID, traceID string, payload map[string]any) ([]any, error) {
	eventID := platformoutbox.DeterministicUUID(eventType + ":" + tenantID + ":" + caseID + ":" + fmt.Sprint(payload["health_session_id"]))
	now := time.Now().UTC()
	idem := eventType + ":" + caseID + ":" + fmt.Sprint(payload["health_session_id"])
	envelope := map[string]any{"event_id": eventID, "event_type": eventType, "schema_version": "1.0.0", "schema_ref": "contracts/jsonschema/domain-event-envelope.schema.json#" + eventType,
		"aggregate_type": "health_case", "aggregate_id": caseID, "occurred_at": now.Format(time.RFC3339Nano), "recorded_at": now.Format(time.RFC3339Nano),
		"producer": map[string]any{"module": "health", "service": "goatos-api"}, "idempotency_key": idem,
		"actor":        map[string]any{"actor_type": map[bool]string{true: "system_rule", false: "operator"}[actorID == ""], "actor_id": nil, "actor_ref": nil},
		"subject_type": "goat", "subject_id": goatID, "visibility_scope": map[string]any{"tenant_id": tenantID}, "evidence_refs": []any{}, "payload": payload, "trace_id": traceID}
	if actorID != "" {
		envelope["actor"].(map[string]any)["actor_id"] = actorID
	}
	body, err := json.Marshal(envelope)
	if err != nil {
		return nil, err
	}
	headers, _ := json.Marshal(map[string]any{"content_type": "application/json"})
	return []any{tenantID, eventID, eventType, caseID, healthTopic, body, headers, idem, traceID}, nil
}

func insertHealthOutbox(ctx context.Context, tx pgx.Tx, tenantID, actorID, eventType, caseID, goatID, traceID string, payload map[string]any) error {
	args, err := healthOutboxArgs(tenantID, actorID, eventType, caseID, goatID, traceID, payload)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, insertHealthOutboxSQL, args...)
	if err != nil {
		return fmt.Errorf("health: insert outbox: %w", err)
	}
	return nil
}
func normalizeSession(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "morning":
		return domain.SessionMorning
	case "afternoon":
		return domain.SessionAfternoon
	case "evening":
		return domain.SessionEvening
	default:
		return domain.SessionUnscheduled
	}
}
func sessionHour(v string) int {
	switch v {
	case domain.SessionAfternoon:
		return 13
	case domain.SessionEvening:
		return 17
	default:
		return 8
	}
}
func sessionRank(v string) int {
	switch v {
	case domain.SessionMorning:
		return 1
	case domain.SessionAfternoon:
		return 2
	case domain.SessionEvening:
		return 3
	default:
		return 4
	}
}
func valueOrEmpty(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}
func encodeCursor(at time.Time, id string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(at.UTC().Format(time.RFC3339Nano) + "|" + id))
}
func decodeCursor(raw string) (any, string, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, "", nil
	}
	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, "", err
	}
	parts := strings.SplitN(string(b), "|", 2)
	if len(parts) != 2 {
		return nil, "", errors.New("bad cursor")
	}
	at, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return nil, "", err
	}
	return at, parts[1], nil
}
