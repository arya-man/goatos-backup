package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"cloud.google.com/go/bigquery"
	"github.com/vgoats/goatos/backend/internal/platform/observability"
	"google.golang.org/api/iterator"
)

const maxBodyBytes = 1 << 20

type server struct {
	log          *slog.Logger
	http         *http.Client
	bq           billingQuerier
	webhook      string
	slackBot     string
	channelID    string
	token        string
	console      string
	queryHint    string
	billingTable string
	projects     []string
	now          func() time.Time
}

type billingQuerier interface {
	Query(query string) *bigquery.Query
	Close() error
}

type monitoringIncident struct {
	Incident struct {
		PolicyName     string            `json:"policy_name"`
		ConditionName  string            `json:"condition_name"`
		State          string            `json:"state"`
		StartedAt      int64             `json:"started_at"`
		EndedAt        int64             `json:"ended_at"`
		Summary        string            `json:"summary"`
		URL            string            `json:"url"`
		Resource       map[string]string `json:"resource"`
		ResourceName   string            `json:"resource_name"`
		Metric         map[string]string `json:"metric"`
		ObservedValue  string            `json:"observed_value"`
		ThresholdValue string            `json:"threshold_value"`
	} `json:"incident"`
	Version string `json:"version"`
}

type pubsubPush struct {
	Message struct {
		Data       string            `json:"data"`
		Attributes map[string]string `json:"attributes"`
		MessageID  string            `json:"messageId"`
		PublishAt  string            `json:"publishTime"`
	} `json:"message"`
	Subscription string `json:"subscription"`
}

type budgetNotification struct {
	BudgetDisplayName       string  `json:"budgetDisplayName"`
	AlertThresholdExceeded  float64 `json:"alertThresholdExceeded"`
	CostAmount              float64 `json:"costAmount"`
	CostIntervalStart       string  `json:"costIntervalStart"`
	BudgetAmount            float64 `json:"budgetAmount"`
	BudgetAmountType        string  `json:"budgetAmountType"`
	CurrencyCode            string  `json:"currencyCode"`
	ForecastThresholdAmount float64 `json:"forecastThresholdAmount"`
}

type anomalyRow struct {
	AlertType      string               `bigquery:"alert_type"`
	ProjectID      string               `bigquery:"project_id"`
	Service        string               `bigquery:"service"`
	Spend          float64              `bigquery:"spend"`
	Delta          float64              `bigquery:"delta"`
	ReferenceSpend bigquery.NullFloat64 `bigquery:"reference_spend"`
	TopSKU         string               `bigquery:"top_sku"`
	UsageDate      time.Time            `bigquery:"usage_date"`
	CurrencyCode   string               `bigquery:"currency_code"`
}

func main() {
	if err := run(); err != nil {
		os.Exit(1)
	}
}

func run() error {
	log := observability.New(observability.Config{
		Service: "cost-alert-bridge",
		Level:   os.Getenv("GOATOS_LOG_LEVEL"),
	})
	webhook := strings.TrimSpace(os.Getenv("GOATOS_COST_ALERT_SLACK_WEBHOOK_URL"))
	if webhook == "unset" {
		webhook = ""
	}
	slackBot := strings.TrimSpace(os.Getenv("GOATOS_COST_ALERT_SLACK_BOT_TOKEN"))
	channelID := strings.TrimSpace(os.Getenv("GOATOS_COST_ALERT_SLACK_CHANNEL_ID"))
	if webhook == "" && (slackBot == "" || channelID == "") {
		return errors.New("set GOATOS_COST_ALERT_SLACK_WEBHOOK_URL or GOATOS_COST_ALERT_SLACK_BOT_TOKEN plus GOATOS_COST_ALERT_SLACK_CHANNEL_ID")
	}
	addr := strings.TrimSpace(os.Getenv("GOATOS_HTTP_ADDR"))
	if addr == "" {
		addr = ":8080"
	}
	s := &server{
		log:          log,
		http:         &http.Client{Timeout: 5 * time.Second},
		webhook:      webhook,
		slackBot:     slackBot,
		channelID:    channelID,
		token:        strings.TrimSpace(os.Getenv("GOATOS_COST_ALERT_SHARED_TOKEN")),
		console:      envDefault("GOATOS_COST_ALERT_CONSOLE_URL", "https://console.cloud.google.com/billing/01FEDE-96BCB3-76D992/reports?project=goatos-stg"),
		queryHint:    envDefault("GOATOS_COST_ALERT_FIRST_QUERY", "Open Billing Reports grouped by project, service, then SKU for today and yesterday; for media spikes also open Cloud Logging for proof_download_redirect in goatos-api-stg."),
		billingTable: strings.TrimSpace(os.Getenv("GOATOS_BILLING_EXPORT_TABLE")),
		projects:     splitCSV(envDefault("GOATOS_BILLING_MONITORED_PROJECTS", "goatos-stg,goatos-sheets,goatos-dev")),
		now:          time.Now,
	}
	if s.billingTable != "" {
		client, err := bigquery.NewClient(context.Background(), envDefault("GOATOS_BILLING_QUERY_PROJECT_ID", "goatos-stg"))
		if err != nil {
			return fmt.Errorf("create bigquery client: %w", err)
		}
		defer client.Close()
		s.bq = client
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/readyz", s.ready)
	mux.HandleFunc("/livez", s.ready)
	mux.HandleFunc("/monitoring-webhook", s.monitoringWebhook)
	mux.HandleFunc("/budget-pubsub", s.budgetPubsub)
	mux.HandleFunc("/billing-anomaly-check", s.billingAnomalyCheck)

	httpServer := &http.Server{Addr: addr, Handler: mux}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		log.Info("cost_alert_bridge_starting", slog.String("addr", addr))
		errCh <- httpServer.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("cost_alert_bridge_error", slog.String("error", err.Error()))
			return err
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return httpServer.Shutdown(shutdownCtx)
}

func (s *server) ready(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok\n"))
}

func (s *server) monitoringWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.authorized(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var incident monitoringIncident
	if err := decodeJSON(r.Body, &incident); err != nil {
		http.Error(w, "invalid monitoring payload", http.StatusBadRequest)
		return
	}
	msg := s.formatMonitoring(incident)
	if err := s.postSlack(r.Context(), msg); err != nil {
		s.log.Error("cost_alert_slack_post_failed", slog.String("error", err.Error()))
		http.Error(w, "slack post failed", http.StatusBadGateway)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) budgetPubsub(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.authorized(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var push pubsubPush
	if err := decodeJSON(r.Body, &push); err != nil {
		http.Error(w, "invalid pubsub payload", http.StatusBadRequest)
		return
	}
	data, err := base64.StdEncoding.DecodeString(push.Message.Data)
	if err != nil {
		http.Error(w, "invalid pubsub data", http.StatusBadRequest)
		return
	}
	var budget budgetNotification
	if err := json.Unmarshal(data, &budget); err != nil {
		http.Error(w, "invalid budget notification", http.StatusBadRequest)
		return
	}
	if err := s.postSlack(r.Context(), s.formatBudget(budget)); err != nil {
		s.log.Error("cost_alert_slack_post_failed", slog.String("error", err.Error()))
		http.Error(w, "slack post failed", http.StatusBadGateway)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) billingAnomalyCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.authorized(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	force := r.URL.Query().Get("force") == "1" || r.URL.Query().Get("force") == "true"
	if s.bq == nil || s.billingTable == "" {
		if force {
			_ = s.postSlack(r.Context(), s.formatBillingNotReady("billing export table is not configured"))
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	rows, err := s.queryBillingAnomalies(r.Context())
	if err != nil {
		if isBillingExportNotReady(err) {
			if force {
				_ = s.postSlack(r.Context(), s.formatBillingNotReady(err.Error()))
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if force {
			_ = s.postSlack(r.Context(), s.formatBillingNotReady(err.Error()))
			w.WriteHeader(http.StatusNoContent)
			return
		}
		s.log.Error("cost_alert_billing_query_failed", slog.String("error", err.Error()))
		http.Error(w, "billing query failed", http.StatusBadGateway)
		return
	}
	if len(rows) == 0 {
		if force {
			if err := s.postSlack(r.Context(), s.formatBillingNoAnomalies()); err != nil {
				http.Error(w, "slack post failed", http.StatusBadGateway)
				return
			}
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := s.postSlack(r.Context(), s.formatBillingAnomalies(rows)); err != nil {
		s.log.Error("cost_alert_slack_post_failed", slog.String("error", err.Error()))
		http.Error(w, "slack post failed", http.StatusBadGateway)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) queryBillingAnomalies(ctx context.Context) ([]anomalyRow, error) {
	query := s.bq.Query(billingAnomalySQL(s.billingTable))
	query.Parameters = []bigquery.QueryParameter{
		{Name: "projects", Value: s.projects},
		{Name: "as_of_date", Value: s.now().In(time.FixedZone("IST", 5*60*60+30*60)).Format("2006-01-02")},
	}
	iter, err := query.Read(ctx)
	if err != nil {
		return nil, err
	}
	var rows []anomalyRow
	for {
		var row anomalyRow
		err := iter.Next(&row)
		if errors.Is(err, iterator.Done) {
			break
		}
		if err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func (s *server) authorized(r *http.Request) bool {
	if s.token == "" {
		return true
	}
	return r.URL.Query().Get("token") == s.token || strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ") == s.token
}

func (s *server) formatMonitoring(payload monitoringIncident) string {
	inc := payload.Incident
	project := firstNonEmpty(inc.Resource["project_id"], inc.Resource["project"], "goatos-stg")
	service := firstNonEmpty(inc.Metric["service"], inc.Metric["metric.type"], "Cloud Monitoring metric")
	metric := firstNonEmpty(inc.Metric["method"], inc.Metric["metric.type"], inc.ConditionName)
	value := firstNonEmpty(inc.ObservedValue, "see incident")
	delta := firstNonEmpty(inc.ThresholdValue, "threshold crossed")
	link := firstNonEmpty(inc.URL, s.console)
	query := s.queryFor(inc.PolicyName, inc.ConditionName)

	return fmt.Sprintf("*GoatOS GCP cost/media alert: %s*\nProject: `%s`\nService: `%s`\nSpend/usage: `%s`\nDelta/threshold: `%s`\nTop SKU/metric: `%s`\nConsole: %s\nFirst query: `%s`",
		firstNonEmpty(inc.PolicyName, inc.ConditionName, "Cloud Monitoring alert"),
		project,
		service,
		value,
		delta,
		metric,
		link,
		query,
	)
}

func (s *server) formatBudget(payload budgetNotification) string {
	threshold := payload.AlertThresholdExceeded
	if threshold == 0 && payload.ForecastThresholdAmount > 0 && payload.BudgetAmount > 0 {
		threshold = payload.ForecastThresholdAmount / payload.BudgetAmount
	}
	spend := formatMoney(payload.CurrencyCode, payload.CostAmount)
	forecast := formatMoney(payload.CurrencyCode, payload.ForecastThresholdAmount)
	return fmt.Sprintf("*GoatOS GCP billing forecast alert: %s*\nProject: `billing account scoped: goatos-stg (display name GoatOS), goatos-sheets, goatos-dev if billing is re-enabled`\nService: `all GCP services`\nSpend/usage: `%s current interval cost`\nDelta/threshold: `forecast crossed %.1f%% (%s of %s budget)`\nTop SKU/metric: `Billing export: group by Service, SKU, Project for today and previous 7 days`\nConsole: %s\nFirst query: `%s`",
		firstNonEmpty(payload.BudgetDisplayName, "GoatOS monthly forecast cost alerts"),
		spend,
		threshold*100,
		firstNonEmpty(forecast, "configured forecast threshold"),
		formatMoney(payload.CurrencyCode, payload.BudgetAmount),
		s.console,
		s.queryHint,
	)
}

func (s *server) formatBillingAnomalies(rows []anomalyRow) string {
	var b strings.Builder
	fmt.Fprintf(&b, "*GoatOS GCP daily billing anomaly alert*\nConsole: %s\n", s.console)
	for _, row := range rows {
		ref := "n/a"
		if row.ReferenceSpend.Valid {
			ref = formatMoney(row.CurrencyCode, row.ReferenceSpend.Float64)
		}
		fmt.Fprintf(&b, "\nProject: `%s`\nService: `%s`\nSpend/usage: `%s on %s`\nDelta: `%s; reference %s`\nTop SKU/metric: `%s`\nFirst query: `%s`\n",
			firstNonEmpty(row.ProjectID, "billing-account"),
			firstNonEmpty(row.Service, "all services"),
			formatMoney(row.CurrencyCode, row.Spend),
			row.UsageDate.Format("2006-01-02"),
			row.deltaText(),
			ref,
			firstNonEmpty(row.TopSKU, "Billing export daily net cost"),
			billingFirstQuery(row.ProjectID, row.Service),
		)
	}
	return strings.TrimSpace(b.String())
}

func (s *server) formatBillingNoAnomalies() string {
	return fmt.Sprintf("*GoatOS GCP billing anomaly watcher test*\nProject: `billing account scoped: %s`\nService: `all monitored services`\nSpend/usage: `no daily anomaly currently above threshold`\nDelta: `below configured thresholds`\nTop SKU/metric: `BigQuery billing export daily net cost by SKU`\nConsole: %s\nFirst query: `%s`",
		strings.Join(s.projects, ", "),
		s.console,
		s.queryHint,
	)
}

func (s *server) formatBillingNotReady(reason string) string {
	return fmt.Sprintf("*GoatOS GCP billing anomaly watcher test*\nProject: `billing account 01FEDE-96BCB3-76D992`\nService: `BigQuery billing export`\nSpend/usage: `not evaluated yet`\nDelta: `%s`\nTop SKU/metric: `gcp_billing_export_v1_01FEDE_96BCB3_76D992`\nConsole: %s\nFirst query: `%s`",
		reason,
		s.console,
		s.queryHint,
	)
}

func (row anomalyRow) deltaText() string {
	switch row.AlertType {
	case "daily_spend_jump":
		return fmt.Sprintf("%.1f%% over previous 7-day daily average", row.Delta*100)
	case "service_day_over_day":
		return fmt.Sprintf("%.1f%% day-over-day increase", row.Delta*100)
	default:
		if row.Delta > 0 {
			return formatMoney(row.CurrencyCode, row.Delta)
		}
		return "threshold crossed"
	}
}

func billingFirstQuery(project, service string) string {
	parts := []string{"Billing export SQL: filter latest usage_date"}
	if project != "" {
		parts = append(parts, `project.id="`+project+`"`)
	}
	if service != "" && service != "all services" {
		parts = append(parts, `service.description="`+service+`"`)
	}
	parts = append(parts, "group by sku.description order by net_cost desc")
	return strings.Join(parts, "; ")
}

func (s *server) queryFor(policy, condition string) string {
	text := strings.ToLower(policy + " " + condition)
	switch {
	case strings.Contains(text, "readobject"), strings.Contains(text, "media bucket"):
		return `resource.type="cloud_run_revision" AND resource.labels.service_name="goatos-api-stg" AND jsonPayload.event="proof_download_redirect"; then group GCS metrics by method/response_code on goatos-stg-media`
	case strings.Contains(text, "cloud storage"):
		return `Billing Reports: project=goatos-stg, service="Cloud Storage", group by SKU, compare today vs previous 7 days`
	case strings.Contains(text, "cloud run"):
		return `Billing Reports: service="Cloud Run", group by project and SKU; then open Cloud Run metrics by revision`
	case strings.Contains(text, "cloud sql"):
		return `Billing Reports: service="Cloud SQL", group by SKU; then open goatos-stg-core-db Query Insights`
	default:
		return s.queryHint
	}
}

func (s *server) postSlack(ctx context.Context, text string) error {
	if s.slackBot != "" && s.channelID != "" {
		return s.postSlackAPI(ctx, text)
	}
	payload, err := json.Marshal(map[string]string{"text": text})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.webhook, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "goatos-cost-alert-bridge/1.0")
	resp, err := s.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("slack webhook returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return nil
}

func (s *server) postSlackAPI(ctx context.Context, text string) error {
	payload, err := json.Marshal(map[string]string{
		"channel": s.channelID,
		"text":    text,
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://slack.com/api/chat.postMessage", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+s.slackBot)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "goatos-cost-alert-bridge/1.0")
	resp, err := s.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("slack api returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var result struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("decode slack api response: %w", err)
	}
	if !result.OK {
		return fmt.Errorf("slack api rejected message: %s", result.Error)
	}
	return nil
}

func decodeJSON(body io.Reader, out any) error {
	decoder := json.NewDecoder(io.LimitReader(body, maxBodyBytes))
	return decoder.Decode(out)
}

func envDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func isBillingExportNotReady(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "not found") ||
		strings.Contains(msg, "was not found") ||
		strings.Contains(msg, "no such table") ||
		strings.Contains(msg, "notfound")
}

func formatMoney(currency string, amount float64) string {
	if amount == 0 {
		return ""
	}
	if currency == "" {
		currency = "INR"
	}
	return currency + " " + strconv.FormatFloat(amount, 'f', 2, 64)
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func billingAnomalySQL(table string) string {
	table = strings.Trim(table, "`")
	// projection-review: membership=Cloud Billing standard export rows for the latest
	// completed usage_date, filtered to the monitored project ids; group_key=(usage_date,
	// project_id, service, sku) for service alerts and usage_date alone for billing-account
	// daily spend; join_cardinality=no joins to dimension tables, only pre-aggregated CTEs
	// over the same base rows and scalar ARRAY_AGG top-SKU lookups, so SKU selection cannot
	// multiply spend; pagination=none and LIMIT 25 only caps Slack alert lines after all
	// threshold decisions are made; scope=billing account export table plus explicit project
	// id allow-list, with GoatOS represented by project_id goatos-stg.
	// scale-guard:ignore: daily ops alert query over Cloud Billing export, scheduled once/day,
	// bounded to latest 10 usage days and explicit monitored projects, not a request-path read.
	return fmt.Sprintf(
		`
WITH base AS (
  SELECT
    DATE(usage_start_time) AS usage_date,
    IFNULL(project.id, '(no project)') AS project_id,
    service.description AS service,
    sku.description AS sku,
    currency AS currency_code,
    cost + IFNULL((SELECT SUM(credit.amount) FROM UNNEST(credits) AS credit), 0) AS net_cost
  FROM `+"`%s`"+` -- scale-guard:ignore: daily ops alert query over a 10-day billing export window, not a request-path read
  WHERE DATE(usage_start_time) BETWEEN DATE_SUB(DATE(@as_of_date), INTERVAL 10 DAY) AND DATE(@as_of_date)
    AND (ARRAY_LENGTH(@projects) = 0 OR project.id IN UNNEST(@projects))
),
latest_day AS (
  SELECT MAX(usage_date) AS usage_date
  FROM base
  WHERE usage_date < DATE(@as_of_date)
),
daily AS (
  SELECT usage_date, ANY_VALUE(currency_code) AS currency_code, SUM(net_cost) AS spend
  FROM base
  GROUP BY usage_date
),
daily_current AS (
  SELECT daily.*
  FROM daily
  JOIN latest_day USING (usage_date)
),
daily_reference AS (
  SELECT AVG(spend) AS avg_spend
  FROM daily, latest_day
  WHERE daily.usage_date BETWEEN DATE_SUB(latest_day.usage_date, INTERVAL 7 DAY) AND DATE_SUB(latest_day.usage_date, INTERVAL 1 DAY)
),
daily_top_sku AS (
  SELECT ARRAY_AGG(sku ORDER BY spend DESC LIMIT 1)[OFFSET(0)] AS top_sku
  FROM (
    SELECT sku, SUM(net_cost) AS spend
    FROM base
    JOIN latest_day USING (usage_date)
    GROUP BY sku
  )
),
service_daily AS (
  SELECT usage_date, project_id, service, ANY_VALUE(currency_code) AS currency_code, SUM(net_cost) AS spend
  FROM base
  GROUP BY usage_date, project_id, service
),
service_current AS (
  SELECT service_daily.*
  FROM service_daily
  JOIN latest_day USING (usage_date)
),
service_previous AS (
  SELECT service_daily.project_id, service_daily.service, service_daily.spend
  FROM service_daily, latest_day
  WHERE service_daily.usage_date = DATE_SUB(latest_day.usage_date, INTERVAL 1 DAY)
),
service_top_sku AS (
  SELECT project_id, service, ARRAY_AGG(sku ORDER BY spend DESC LIMIT 1)[OFFSET(0)] AS top_sku
  FROM (
    SELECT project_id, service, sku, SUM(net_cost) AS spend
    FROM base
    JOIN latest_day USING (usage_date)
    GROUP BY project_id, service, sku
  )
  GROUP BY project_id, service
)
SELECT
  'daily_spend_jump' AS alert_type,
  'billing-account' AS project_id,
  'all services' AS service,
  daily_current.spend AS spend,
  SAFE_DIVIDE(daily_current.spend - daily_reference.avg_spend, daily_reference.avg_spend) AS delta,
  daily_reference.avg_spend AS reference_spend,
  daily_top_sku.top_sku AS top_sku,
  daily_current.usage_date AS usage_date,
  daily_current.currency_code AS currency_code
FROM daily_current, daily_reference, daily_top_sku
WHERE daily_reference.avg_spend > 0
  AND daily_current.spend > daily_reference.avg_spend * 1.3
UNION ALL
SELECT
  'service_day_over_day' AS alert_type,
  service_current.project_id,
  service_current.service,
  service_current.spend AS spend,
  SAFE_DIVIDE(service_current.spend - IFNULL(service_previous.spend, 0), NULLIF(IFNULL(service_previous.spend, 0), 0)) AS delta,
  service_previous.spend AS reference_spend,
  service_top_sku.top_sku,
  service_current.usage_date,
  service_current.currency_code
FROM service_current
LEFT JOIN service_previous USING (project_id, service)
LEFT JOIN service_top_sku USING (project_id, service)
WHERE service_current.spend > 1000
  AND (IFNULL(service_previous.spend, 0) = 0 OR service_current.spend > service_previous.spend * 2)
UNION ALL
SELECT
  CONCAT('service_threshold_', LOWER(REPLACE(service_current.service, ' ', '_'))) AS alert_type,
  service_current.project_id,
  service_current.service,
  service_current.spend AS spend,
  CASE
    WHEN service_current.service = 'Cloud Storage' THEN service_current.spend - 1000
    WHEN service_current.service = 'Cloud Run' THEN service_current.spend - 2000
    WHEN service_current.service = 'Cloud SQL' THEN service_current.spend - 1000
    ELSE 0
  END AS delta,
  NULL AS reference_spend,
  service_top_sku.top_sku,
  service_current.usage_date,
  service_current.currency_code
FROM service_current
LEFT JOIN service_top_sku USING (project_id, service)
WHERE (service_current.service = 'Cloud Storage' AND service_current.spend > 1000)
   OR (service_current.service = 'Cloud Run' AND service_current.spend > 2000)
   OR (service_current.service = 'Cloud SQL' AND service_current.spend > 1000)
ORDER BY alert_type, spend DESC
LIMIT 25
`, table)
}
