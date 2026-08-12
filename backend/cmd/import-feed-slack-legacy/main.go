package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	feedpostgres "github.com/vgoats/goatos/backend/internal/feeddirection/adapters/postgres"
	verificationbridge "github.com/vgoats/goatos/backend/internal/feeddirection/adapters/verificationbridge"
	feeddirectionapp "github.com/vgoats/goatos/backend/internal/feeddirection/app"
	feeddirectiondomain "github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	feedports "github.com/vgoats/goatos/backend/internal/feeddirection/ports"
	platformoutbox "github.com/vgoats/goatos/backend/internal/platform/outbox"
	verificationpostgres "github.com/vgoats/goatos/backend/internal/verification/adapters/postgres"
	verificationapp "github.com/vgoats/goatos/backend/internal/verification/app"
	verificationdomain "github.com/vgoats/goatos/backend/internal/verification/domain"
)

const tenantID = "00000000-0000-4000-8000-000000000001"

type manifest struct {
	Source struct {
		ChannelID    string `json:"channel_id"`
		BusinessDate string `json:"business_date"`
		ParkName     string `json:"park_name"`
		Workflow     string `json:"workflow"`
	} `json:"source"`
	Records []record `json:"records"`
}

type record struct {
	ThreadTS       string `json:"thread_ts"`
	SlackShed      string `json:"slack_shed"`
	BaseShed       string `json:"base_shed"`
	PartitionLabel string `json:"partition_label"`
	SessionNo      int32  `json:"session_no"`
	WeightPhoto    file   `json:"weight_photo"`
	Distribution   file   `json:"distribution_video"`
	Water          file   `json:"water_video"`
}

type file struct {
	FileName        string `json:"file_name"`
	FileID          string `json:"file_id"`
	MimeType        string `json:"mime_type"`
	UploaderSlackID string `json:"uploader_slack_id"`
	UploaderName    string `json:"uploader_name"`
	UploaderEmail   string `json:"uploader_email"`
	UploadedAt      string `json:"uploaded_at"`
}

type preparedProof struct {
	ID, ObjectKey, LocalPath, Hash string
	Size                           int64
	File                           file
}

func main() {
	var manifestPath, mediaDir, databaseURL, bucket string
	var apply bool
	flag.StringVar(&manifestPath, "manifest", "", "legacy Slack feed manifest")
	flag.StringVar(&mediaDir, "media-dir", "", "directory containing <slack-file-id>.<ext>")
	flag.StringVar(&databaseURL, "database-url", os.Getenv("DATABASE_URL"), "staging Postgres URL")
	flag.StringVar(&bucket, "bucket", "goatos-stg-media", "GCS proof bucket")
	flag.BoolVar(&apply, "apply", false, "write proof, completion, audit, idempotency, verification, and outbox rows")
	flag.Parse()
	if manifestPath == "" || mediaDir == "" || databaseURL == "" {
		log.Fatal("--manifest, --media-dir, and --database-url/DATABASE_URL are required")
	}

	ctx := context.Background()
	m := readManifest(manifestPath)
	if len(m.Records) != 50 || m.Source.BusinessDate != "2026-08-12" || m.Source.ChannelID != "C0AD56LEGGN" || m.Source.ParkName != "Coimbatore" || m.Source.Workflow != "normal" {
		log.Fatalf("manifest scope mismatch: records=%d date=%q channel=%q park=%q workflow=%q", len(m.Records), m.Source.BusinessDate, m.Source.ChannelID, m.Source.ParkName, m.Source.Workflow)
	}
	proofs := prepareProofs(m, mediaDir)

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()
	parkID, sheds, members := resolveScope(ctx, pool, m)
	replays := validateNoCollisions(ctx, pool, m, parkID, sheds)
	if !apply {
		fmt.Printf("DRY RUN OK: 50 sessions, %d proofs, park=%s, bucket=%s, matching replays=%d, conflicting natural keys=0\n", len(proofs), parkID, bucket, replays)
		for _, p := range proofs {
			fmt.Printf("UPLOAD %s gs://%s/%s\n", p.LocalPath, bucket, p.ObjectKey)
		}
		return
	}

	insertProofs(ctx, pool, proofs, members, m.Source.ChannelID)
	feedRepo := feedpostgres.NewRepository(pool, 30*time.Second)
	verificationRepo := verificationpostgres.NewRepository(pool, 30*time.Second)
	verificationService := verificationapp.NewService(verificationRepo, nil)
	if err := verificationService.RegisterCategory(verificationdomain.CategoryDefinition{
		Vertical: feeddirectiondomain.VerificationVerticalFeed, Module: feeddirectiondomain.VerificationModuleFeed,
		Category:      feeddirectiondomain.VerificationCategoryFeed,
		ExpectedMedia: []string{"photo", "video", "video"},
		MediaLabels:   []string{"Feed weight photo", "Feed distribution video", "Water distribution video"},
	}); err != nil {
		log.Fatal(err)
	}
	enqueuer := verificationbridge.New(verificationService)
	date, _ := time.Parse("2006-01-02", m.Source.BusinessDate)

	for _, rec := range m.Records {
		weight := proofFor(proofs, rec.WeightPhoto.FileID)
		dist := proofFor(proofs, rec.Distribution.FileID)
		water := proofFor(proofs, rec.Water.FileID)
		completedBy := members[strings.ToLower(strings.TrimSpace(rec.WeightPhoto.UploaderEmail))]
		key := "legacy-slack-feed-distribution:" + m.Source.ChannelID + ":" + rec.ThreadTS
		result, err := feedRepo.CompleteDistribution(ctx, feedportsParams(rec, parkID, sheds[rec.BaseShed], date, weight.ID, dist.ID, water.ID, completedBy, key))
		if err != nil {
			log.Fatalf("complete %s session %d: %v", rec.SlackShed, rec.SessionNo, err)
		}
		// Always repair the deterministic queue item while the completion remains pending. This makes
		// an interrupted import resumable if the completion committed before verification enqueueing.
		if result.Status == "pending_verification" {
			if err := enqueuer.EnqueueFeedDistributionVerification(ctx, feeddirectionapp.FeedDistributionVerificationEnqueueRequest{
				TenantID: tenantID, CompletionID: result.CompletionID, ParkID: parkID, ShedID: sheds[rec.BaseShed],
				PartitionLabel: rec.PartitionLabel, SessionNo: rec.SessionNo, Workflow: m.Source.Workflow, TargetDate: date,
				FeedWeightProofRef: weight.ID, DistributionProofRef: dist.ID, WaterProofRef: water.ID,
				OperatorID: completedBy, CapturedAt: parseSlackTime(rec.Water.UploadedAt),
				IdempotencyKey: fmt.Sprintf("feed-distribution-verification:%s:%d", result.CompletionID, result.RowVersion),
			}); err != nil {
				log.Fatalf("enqueue %s session %d: %v", rec.SlackShed, rec.SessionNo, err)
			}
		}
	}
	fmt.Printf("APPLY OK: imported 50 pending-verification sessions and %d proof artifacts\n", len(proofs))
}

func feedportsParams(r record, parkID, shedID string, date time.Time, weight, dist, water, completedBy, key string) feedports.CompleteDistributionParams {
	return feedports.CompleteDistributionParams{TenantID: tenantID, ParkID: parkID, ShedID: shedID, PartitionLabel: r.PartitionLabel, SessionNo: r.SessionNo, TargetDate: date, Workflow: "normal", FeedWeightProofRef: weight, DistributionProofRef: dist, WaterProofRef: water, CompletedBy: completedBy, IdempotencyKey: key, ActorType: "legacy_import", TraceID: "slack:" + r.ThreadTS}
}

func readManifest(path string) manifest {
	b, err := os.ReadFile(path)
	if err != nil {
		log.Fatal(err)
	}
	var m manifest
	if err := json.Unmarshal(b, &m); err != nil {
		log.Fatal(err)
	}
	return m
}

func prepareProofs(m manifest, dir string) map[string]preparedProof {
	out := map[string]preparedProof{}
	for _, r := range m.Records {
		for _, f := range []file{r.WeightPhoto, r.Distribution, r.Water} {
			if _, ok := out[f.FileID]; ok {
				log.Fatalf("duplicate selected Slack file id %s", f.FileID)
			}
			ext := ".mp4"
			if f.MimeType == "image/jpeg" {
				ext = ".jpg"
			}
			path := filepath.Join(dir, f.FileID+ext)
			stat, err := os.Stat(path)
			if err != nil || stat.Size() == 0 {
				log.Fatalf("missing/empty %s", path)
			}
			fh, err := os.Open(path)
			if err != nil {
				log.Fatal(err)
			}
			h := sha256.New()
			if _, err := io.Copy(h, fh); err != nil {
				log.Fatal(err)
			}
			_ = fh.Close()
			id := platformoutbox.DeterministicUUID("legacy-slack-feed-proof:" + tenantID + ":" + f.FileID)
			out[f.FileID] = preparedProof{ID: id, ObjectKey: tenantID + "/legacy/slack/feed/2026/08/12/" + id + ext, LocalPath: path, Hash: hex.EncodeToString(h.Sum(nil)), Size: stat.Size(), File: f}
		}
	}
	if len(out) != 150 {
		log.Fatalf("proof count=%d, want 150", len(out))
	}
	return out
}

func resolveScope(ctx context.Context, pool *pgxpool.Pool, m manifest) (string, map[string]string, map[string]string) {
	var parkID string
	if err := pool.QueryRow(ctx, `SELECT location_id::text FROM locations WHERE tenant_id=$1::uuid AND location_type='park' AND name=$2 AND status='active'`, tenantID, m.Source.ParkName).Scan(&parkID); err != nil {
		log.Fatal(err)
	}
	sheds := map[string]string{}
	for _, r := range m.Records {
		if _, ok := sheds[r.BaseShed]; ok {
			continue
		}
		var id string
		if err := pool.QueryRow(ctx, `SELECT location_id::text FROM locations WHERE tenant_id=$1::uuid AND parent_location_id=$2::uuid AND location_type='shed' AND name=$3 AND status='active'`, tenantID, parkID, r.BaseShed).Scan(&id); err != nil {
			log.Fatalf("resolve shed %s: %v", r.BaseShed, err)
		}
		sheds[r.BaseShed] = id
		if r.PartitionLabel != "" {
			var exists bool
			if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM shed_partitions WHERE tenant_id=$1::uuid AND shed_id=$2::uuid AND status='active' AND lower(btrim(partition_label))=lower(btrim($3)))`, tenantID, id, r.PartitionLabel).Scan(&exists); err != nil || !exists {
				log.Fatalf("partition missing: %s %s", r.BaseShed, r.PartitionLabel)
			}
		}
	}
	members := map[string]string{}
	rows, err := pool.Query(ctx, `SELECT lower(metadata->>'email'),workforce_member_id::text FROM workforce_members WHERE tenant_id=$1::uuid AND status='active' AND metadata ? 'email'`, tenantID)
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var email, id string
		if err := rows.Scan(&email, &id); err != nil {
			log.Fatal(err)
		}
		members[email] = id
	}
	return parkID, sheds, members
}

func validateNoCollisions(ctx context.Context, pool *pgxpool.Pool, m manifest, parkID string, sheds map[string]string) int {
	replays := 0
	for _, r := range m.Records {
		var existingKey string
		err := pool.QueryRow(ctx, `SELECT idempotency_key FROM feed_distribution_completions WHERE tenant_id=$1::uuid AND park_id=$2::uuid AND shed_id=$3::uuid AND partition_key=CASE WHEN btrim($4)='' THEN 'whole' ELSE lower(btrim($4)) END AND session_no=$5 AND target_date=$6::date AND workflow=$7`, tenantID, parkID, sheds[r.BaseShed], r.PartitionLabel, r.SessionNo, m.Source.BusinessDate, m.Source.Workflow).Scan(&existingKey)
		if err != nil && err != pgx.ErrNoRows {
			log.Fatal(err)
		}
		if err == pgx.ErrNoRows {
			continue
		}
		expectedKey := "legacy-slack-feed-distribution:" + m.Source.ChannelID + ":" + r.ThreadTS
		if existingKey != expectedKey {
			log.Fatalf("natural-key collision: %s session %d", r.SlackShed, r.SessionNo)
		}
		replays++
	}
	return replays
}

func insertProofs(ctx context.Context, pool *pgxpool.Pool, proofs map[string]preparedProof, members map[string]string, channelID string) {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		log.Fatal(err)
	}
	defer tx.Rollback(ctx)
	for _, p := range proofs {
		meta, _ := json.Marshal(map[string]any{"capture_source": "legacy_slack_import", "source_system": "slack", "slack_channel_id": channelID, "slack_file_id": p.File.FileID, "slack_file_name": p.File.FileName, "slack_uploader_id": p.File.UploaderSlackID, "slack_uploader_name": p.File.UploaderName, "slack_uploader_email": p.File.UploaderEmail, "slack_uploaded_at": p.File.UploadedAt})
		uploadedBy := members[strings.ToLower(strings.TrimSpace(p.File.UploaderEmail))]
		tag, err := tx.Exec(ctx, `INSERT INTO proof_artifacts(proof_id,tenant_id,storage_provider,object_key,content_hash,mime_type,size_bytes,upload_state,scope_type,scope_id,subject_type,subject_id,proof_type,uploaded_by,metadata,uploaded_at,idempotency_key,request_fingerprint,retention_policy) VALUES($1::uuid,$2::uuid,'gcs',$3,$4,$5,$6,'completed','tenant',$2::uuid,'other',NULL,$7,nullif($8,'')::uuid,$9::jsonb,$10,$11,$4,'standard_1y') ON CONFLICT (tenant_id,idempotency_key) WHERE idempotency_key IS NOT NULL DO NOTHING`, p.ID, tenantID, p.ObjectKey, "sha256:"+p.Hash, p.File.MimeType, p.Size, proofType(p.File.MimeType), uploadedBy, string(meta), parseSlackTime(p.File.UploadedAt), "legacy-slack-proof:"+p.File.FileID)
		if err != nil {
			log.Fatal(err)
		}
		if tag.RowsAffected() == 0 {
			var hash, key string
			if err := tx.QueryRow(ctx, `SELECT content_hash,object_key FROM proof_artifacts WHERE tenant_id=$1::uuid AND idempotency_key=$2`, tenantID, "legacy-slack-proof:"+p.File.FileID).Scan(&hash, &key); err != nil || hash != "sha256:"+p.Hash || key != p.ObjectKey {
				log.Fatalf("proof replay conflict %s", p.File.FileID)
			}
		}
	}
	if err := tx.Commit(ctx); err != nil {
		log.Fatal(err)
	}
}

func proofFor(m map[string]preparedProof, id string) preparedProof {
	p, ok := m[id]
	if !ok {
		log.Fatalf("missing prepared proof %s", id)
	}
	return p
}
func proofType(mime string) string {
	if mime == "image/jpeg" {
		return "photo"
	}
	return "video"
}
func parseSlackTime(raw string) time.Time {
	t, err := time.Parse("2006-01-02 15:04:05 MST", raw)
	if err != nil {
		log.Fatalf("time %q: %v", raw, err)
	}
	loc, _ := time.LoadLocation("Asia/Kolkata")
	return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), 0, loc).UTC()
}
