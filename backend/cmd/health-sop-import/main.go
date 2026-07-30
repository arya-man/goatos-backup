package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	healthpg "github.com/vgoats/goatos/backend/internal/health/adapters/postgres"
	"github.com/vgoats/goatos/backend/internal/health/domain"
)

type snapshot struct {
	SchemaVersion int `json:"schema_version"`
	Source        struct {
		SpreadsheetID string `json:"spreadsheet_id"`
		AdultTab      string `json:"adult_tab"`
		KidTab        string `json:"kid_tab"`
		CapturedAt    string `json:"captured_at"`
	} `json:"source"`
	Rules struct {
		DefaultDurationDays int    `json:"default_duration_days"`
		CriticalActions     string `json:"critical_actions"`
	} `json:"rules"`
	Protocols []domain.SourceProtocol `json:"protocols"`
}

func main() {
	var tenantID, actorID, filePath, databaseURL string
	flag.StringVar(&tenantID, "tenant", "", "tenant UUID")
	flag.StringVar(&actorID, "actor", "", "admin actor UUID")
	flag.StringVar(&filePath, "file", "../context/source-findings/health-sop-v1.json", "normalized Health SOP snapshot")
	flag.StringVar(&databaseURL, "database-url", os.Getenv("DATABASE_URL"), "Postgres URL")
	flag.Parse()
	if tenantID == "" || actorID == "" || databaseURL == "" {
		log.Fatal("--tenant, --actor, and --database-url/DATABASE_URL are required")
	}
	body, err := os.ReadFile(filePath)
	if err != nil {
		log.Fatal(err)
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	var s snapshot
	if err := dec.Decode(&s); err != nil {
		log.Fatalf("decode snapshot: %v", err)
	}
	if s.SchemaVersion != 1 || s.Rules.DefaultDurationDays != domain.DefaultDurationDays || s.Rules.CriticalActions != "guarded_handoff_only" {
		log.Fatal("snapshot contract does not match Health importer")
	}
	sum := sha256.Sum256(body)
	hash := hex.EncodeToString(sum[:])
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()
	repo := healthpg.NewRepository(pool, 90*time.Second)
	sourceRef := fmt.Sprintf("google-sheet:%s:%s:%s@%s", s.Source.SpreadsheetID, s.Source.AdultTab, s.Source.KidTab, s.Source.CapturedAt)
	if err := repo.ReplacePublishedProtocols(ctx, tenantID, actorID, sourceRef, hash, s.Protocols); err != nil {
		log.Fatal(err)
	}
	log.Printf("published %d Health protocols (sha256=%s)", len(s.Protocols), hash)
}
