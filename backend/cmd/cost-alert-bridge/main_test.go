package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAuthorizedFailsClosedWithoutSharedToken(t *testing.T) {
	req := httptest.NewRequest("POST", "/monitoring?token=anything", nil)
	if (&server{}).authorized(req) {
		t.Fatal("empty shared token must not authorize a public alert bridge request")
	}
}

func TestAuthorizedAcceptsConfiguredSharedToken(t *testing.T) {
	req := httptest.NewRequest("POST", "/monitoring", nil)
	req.Header.Set("Authorization", "Bearer secret-token")
	if !(&server{token: "secret-token"}).authorized(req) {
		t.Fatal("configured shared token should authorize bearer requests")
	}
}

func TestFormatMonitoringIncludesInvestigationFields(t *testing.T) {
	s := &server{console: "https://console.example", queryHint: "billing query"}
	var payload monitoringIncident
	payload.Incident.PolicyName = "goatos-stg media bucket ReadObject egress critical"
	payload.Incident.ConditionName = "goatos-stg-media served more than 15 GiB in 1 hour"
	payload.Incident.Resource = map[string]string{"project_id": "goatos-stg"}
	payload.Incident.Metric = map[string]string{"metric.type": "storage.googleapis.com/network/sent_bytes_count", "method": "ReadObject"}
	payload.Incident.ObservedValue = "17 GiB"
	payload.Incident.ThresholdValue = "15 GiB"
	payload.Incident.URL = "https://incident.example"

	got := s.formatMonitoring(payload)
	for _, want := range []string{
		"Project: `goatos-stg`",
		"Service: `storage.googleapis.com/network/sent_bytes_count`",
		"Spend/usage: `17 GiB`",
		"Delta/threshold: `15 GiB`",
		"Top SKU/metric: `ReadObject`",
		"Console: https://incident.example",
		"First query:",
		"proof_download_redirect",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("formatted alert missing %q in:\n%s", want, got)
		}
	}
}

func TestFormatBudgetIncludesBillingFields(t *testing.T) {
	s := &server{console: "https://billing.example", queryHint: "group by SKU"}
	got := s.formatBudget(budgetNotification{
		BudgetDisplayName:       "GoatOS monthly forecast cost alerts",
		ForecastThreshold:       0.7777777778,
		CostAmount:              flexibleFloat(12345),
		BudgetAmount:            flexibleFloat(45000),
		CurrencyCode:            "INR",
		ForecastThresholdAmount: flexibleFloat(35000),
	})

	for _, want := range []string{
		"GoatOS monthly forecast cost alerts",
		"Project:",
		"Service: `all GCP services`",
		"Spend/usage: `INR 12345.00 current interval cost`",
		"forecast crossed 77.8%",
		"Top SKU/metric:",
		"Console: https://billing.example",
		"First query: `group by SKU`",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("formatted budget alert missing %q in:\n%s", want, got)
		}
	}
}

func TestDecodeBudgetNotificationAcceptsGooglePubSubShape(t *testing.T) {
	data := `{"budgetDisplayName":"GoatOS monthly forecast Slack alerts","costAmount":25001,"costIntervalStart":"2026-09-01T00:00:00Z","budgetAmount":45000,"budgetAmountType":"SPECIFIED_AMOUNT","forecastThresholdExceeded":0.5555555556,"currencyCode":"INR"}`
	payload, err := json.Marshal(pubsubPushForTest(data))
	if err != nil {
		t.Fatal(err)
	}

	got, err := decodeBudgetNotification(payload)
	if err != nil {
		t.Fatalf("decodeBudgetNotification returned error: %v", err)
	}
	if got.BudgetDisplayName != "GoatOS monthly forecast Slack alerts" {
		t.Fatalf("unexpected budget name: %q", got.BudgetDisplayName)
	}
	if got.ForecastThreshold != flexibleFloat(0.5555555556) {
		t.Fatalf("unexpected forecast threshold: %v", got.ForecastThreshold)
	}
	if got.CostAmount != flexibleFloat(25001) || got.BudgetAmount != flexibleFloat(45000) {
		t.Fatalf("unexpected amounts: cost=%v budget=%v", got.CostAmount, got.BudgetAmount)
	}
}

func TestDecodeBudgetNotificationAcceptsStringNumbers(t *testing.T) {
	data := `{"budgetDisplayName":"GoatOS","costAmount":"25001.50","budgetAmount":"45000","forecastThresholdExceeded":"0.7777777778","currencyCode":"INR"}`

	got, err := decodeBudgetNotification([]byte(data))
	if err != nil {
		t.Fatalf("decodeBudgetNotification returned error: %v", err)
	}
	if got.CostAmount != flexibleFloat(25001.50) {
		t.Fatalf("unexpected cost amount: %v", got.CostAmount)
	}
	if got.ForecastThreshold != flexibleFloat(0.7777777778) {
		t.Fatalf("unexpected forecast threshold: %v", got.ForecastThreshold)
	}
}

func pubsubPushForTest(data string) pubsubPush {
	var push pubsubPush
	push.Message.Data = base64.StdEncoding.EncodeToString([]byte(data))
	push.Message.MessageID = "budget-test"
	push.Subscription = "projects/goatos-stg/subscriptions/goatos-stg-cost-alert-budget-push"
	return push
}

func TestBillingExportNotReadyErrorsAreRecognized(t *testing.T) {
	for _, errText := range []string{
		"Not found: Table goatos-stg:goatos_billing_export.gcp_billing_export_v1_01FEDE_96BCB3_76D992",
		"no such table",
		"googleapi: Error 404: NotFound",
	} {
		if !isBillingExportNotReady(errors.New(errText)) {
			t.Fatalf("expected not-ready match for %q", errText)
		}
	}
	if isBillingExportNotReady(errors.New("permission denied")) {
		t.Fatal("permission errors must not be treated as export warmup")
	}
}

func TestBillingAnomalySQLOneToManyMultipleDimensions(t *testing.T) {
	sql := billingAnomalySQL("goatos-stg.goatos_billing_export.gcp_billing_export_v1_01FEDE_96BCB3_76D992")
	for _, want := range []string{
		"project.id IN UNNEST(@projects)",
		"service.description AS service",
		"sku.description AS sku",
		"SUM(net_cost) AS spend",
		"ARRAY_AGG(sku ORDER BY spend DESC LIMIT 1)",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("billing anomaly SQL missing %q in:\n%s", want, sql)
		}
	}
}

func TestBillingAnomalySQLPaginationPageBoundary(t *testing.T) {
	sql := billingAnomalySQL("billing.table")
	alertLimit := strings.Index(sql, "LIMIT 25")
	threshold := strings.Index(sql, "WHERE daily_reference.avg_spend > 0")
	if alertLimit == -1 || threshold == -1 {
		t.Fatalf("expected threshold and final alert cap in:\n%s", sql)
	}
	if alertLimit < threshold {
		t.Fatalf("final alert cap must be after threshold decisions:\n%s", sql)
	}
}

func TestBillingAnomalySQLStatusMatrixEveryStatusBuckets(t *testing.T) {
	sql := billingAnomalySQL("billing.table")
	for _, want := range []string{
		"'daily_spend_jump' AS alert_type",
		"'service_day_over_day' AS alert_type",
		"CONCAT('service_threshold_'",
		"Cloud Storage",
		"Cloud Run",
		"Cloud SQL",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("billing anomaly SQL missing status/alert bucket %q in:\n%s", want, sql)
		}
	}
}
