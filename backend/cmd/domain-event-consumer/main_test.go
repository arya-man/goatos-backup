package main

import (
	"testing"
	"time"
)

func TestParseFlagsRequiresProjectAndSubscription(t *testing.T) {
	t.Setenv("GOATOS_PUBSUB_PROJECT_ID", "")
	t.Setenv("GOOGLE_CLOUD_PROJECT", "")
	t.Setenv("GOATOS_DOMAIN_EVENTS_SUBSCRIPTION_ID", "")
	t.Setenv("GOATOS_PUBSUB_SUBSCRIPTION_ID", "")
	if _, err := parseFlags(nil); err == nil {
		t.Fatal("expected missing project/subscription error")
	}
	cfg, err := parseFlags([]string{"--project-id", "goatos-dev", "--subscription", "goatos-dev-domain-events", "--timeout", "5s"})
	if err != nil {
		t.Fatalf("parse flags: %v", err)
	}
	if cfg.ProjectID != "goatos-dev" || cfg.SubscriptionID != "goatos-dev-domain-events" || cfg.Timeout != 5*time.Second {
		t.Fatalf("unexpected config: %#v", cfg)
	}
}

func TestParseFlagsReadsEnvironment(t *testing.T) {
	t.Setenv("GOATOS_PUBSUB_PROJECT_ID", "goatos-dev")
	t.Setenv("GOATOS_DOMAIN_EVENTS_SUBSCRIPTION_ID", "domain-events")
	cfg, err := parseFlags(nil)
	if err != nil {
		t.Fatalf("parse env: %v", err)
	}
	if cfg.ProjectID != "goatos-dev" || cfg.SubscriptionID != "domain-events" {
		t.Fatalf("unexpected env config: %#v", cfg)
	}
}

func TestFindDomainEventEnvelopeSchema(t *testing.T) {
	path, err := findDomainEventEnvelopeSchema()
	if err != nil {
		t.Fatalf("find schema: %v", err)
	}
	if path == "" {
		t.Fatal("schema path is empty")
	}
}
