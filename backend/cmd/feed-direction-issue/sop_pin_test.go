package main

import (
	"testing"
	"time"
)

// TestIssueServicePinsThePublishedFeedSOP: the scheduled STG job (infra cloud_run_jobs.tf
// goatos-stg-feed-direction-issue) issues the sheet at the dispatch clock, and IssueDirection
// stamps the sheet with the feed.direction / feed.packing card versions it reads from the SOP
// rules source. Built without that source it stamps version 0 -- the seed -- on every sheet, so
// a card published on /feed/sops never reached a crew whose sheet the job issued (2026-09-17 E2E).
func TestIssueServicePinsThePublishedFeedSOP(t *testing.T) {
	svc := newService(nil, time.Second, config{GeneratedBy: "test", AsOf: time.Now()})
	if !svc.PinsPublishedFeedSOP() {
		t.Fatal("feed-direction-issue composes the lifecycle service without the published feed SOP cards; every issued sheet would be pinned to the seed")
	}
}

// TestIssueServiceReopensPackingOnCorrection: `-action amend` is the 14:00 correction. It reopens
// every packed bag of a pen whose head count moved through the packing store; composed without it,
// the amend reopened nothing (2026-09-17 E2E).
func TestIssueServiceReopensPackingOnCorrection(t *testing.T) {
	svc := newService(nil, time.Second, config{GeneratedBy: "test", AsOf: time.Now()})
	if !svc.ReopensPackingOnCorrection() {
		t.Fatal("feed-direction-issue composes the lifecycle service without the packing store; the afternoon correction would never reopen a packed bag")
	}
}
