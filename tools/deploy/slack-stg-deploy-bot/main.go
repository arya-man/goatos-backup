package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const maxSlackSkew = 5 * time.Minute

type config struct {
	ProjectID     string
	Location      string
	TriggerID     string
	SigningSecret string
	AllowedUsers  map[string]bool
}

type slackActionPayload struct {
	Type        string `json:"type"`
	ResponseURL string `json:"response_url"`
	User        struct {
		ID       string `json:"id"`
		Username string `json:"username"`
		Name     string `json:"name"`
	} `json:"user"`
	Actions []struct {
		ActionID string `json:"action_id"`
		Value    string `json:"value"`
	} `json:"actions"`
}

type cloudBuildRunResponse struct {
	Metadata struct {
		Build struct {
			ID string `json:"id"`
		} `json:"build"`
	} `json:"metadata"`
}

func main() {
	cfg := config{
		ProjectID:     env("PROJECT_ID", "goatos-stg"),
		Location:      env("TRIGGER_LOCATION", "global"),
		TriggerID:     mustEnv("TRIGGER_ID"),
		SigningSecret: mustEnv("SLACK_SIGNING_SECRET"),
		AllowedUsers:  parseAllowedUsers(os.Getenv("SLACK_ALLOWED_USER_IDS")),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("/slack/actions", cfg.handleSlackAction)

	port := env("PORT", "8080")
	log.Printf("goatos stg deploy bot listening on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, mux))
}

func (cfg config) handleSlackAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, "read body", http.StatusBadRequest)
		return
	}
	if err := verifySlackSignature(cfg.SigningSecret, r.Header, body); err != nil {
		http.Error(w, "bad signature", http.StatusUnauthorized)
		return
	}

	values, err := url.ParseQuery(string(body))
	if err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	var payload slackActionPayload
	if err := json.Unmarshal([]byte(values.Get("payload")), &payload); err != nil {
		http.Error(w, "bad payload", http.StatusBadRequest)
		return
	}
	if len(cfg.AllowedUsers) > 0 && !cfg.AllowedUsers[payload.User.ID] {
		writeSlackJSON(w, map[string]any{
			"response_type": "ephemeral",
			"text":          "You are not allowed to deploy Goat OS staging.",
		})
		return
	}
	if len(payload.Actions) == 0 || payload.Actions[0].ActionID != "deploy_goatos_stg_main" {
		writeSlackJSON(w, map[string]any{
			"response_type": "ephemeral",
			"text":          "Unknown deploy action.",
		})
		return
	}

	buildID, err := cfg.runTrigger(r.Context())
	if err != nil {
		log.Printf("run trigger failed: %v", err)
		writeSlackJSON(w, map[string]any{
			"response_type": "ephemeral",
			"text":          fmt.Sprintf("Failed to start STG deploy: `%s`", err.Error()),
		})
		return
	}

	buildURL := fmt.Sprintf("https://console.cloud.google.com/cloud-build/builds/%s?project=%s", buildID, cfg.ProjectID)
	deployURL := fmt.Sprintf("https://console.cloud.google.com/deploy/delivery-pipelines/%s/goatos-stg?project=%s", cfg.Location, cfg.ProjectID)
	writeSlackJSON(w, map[string]any{
		"response_type":    "in_channel",
		"replace_original": false,
		"text":             fmt.Sprintf("STG deploy from `main` started by <@%s>.\nCloud Build: %s\nCloud Deploy: %s", payload.User.ID, buildURL, deployURL),
	})
}

func (cfg config) runTrigger(ctx context.Context) (string, error) {
	token, err := metadataToken(ctx)
	if err != nil {
		return "", err
	}
	endpoint := fmt.Sprintf("https://cloudbuild.googleapis.com/v1/projects/%s/locations/%s/triggers/%s:run", cfg.ProjectID, cfg.Location, cfg.TriggerID)
	body := strings.NewReader(`{"source":{"branchName":"main"}}`)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("cloud build trigger returned %s: %s", resp.Status, strings.TrimSpace(string(respBody)))
	}

	var parsed cloudBuildRunResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", err
	}
	if parsed.Metadata.Build.ID == "" {
		return "pending", nil
	}
	return parsed.Metadata.Build.ID, nil
}

func metadataToken(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://metadata.google.internal/computeMetadata/v1/instance/service-accounts/default/token", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Metadata-Flavor", "Google")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("metadata token returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var payload struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", err
	}
	if payload.AccessToken == "" {
		return "", errors.New("metadata token response missing access_token")
	}
	return payload.AccessToken, nil
}

func verifySlackSignature(secret string, h http.Header, body []byte) error {
	ts := h.Get("X-Slack-Request-Timestamp")
	sig := h.Get("X-Slack-Signature")
	if ts == "" || sig == "" {
		return errors.New("missing slack signature headers")
	}
	unix, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return err
	}
	if time.Since(time.Unix(unix, 0)) > maxSlackSkew || time.Until(time.Unix(unix, 0)) > maxSlackSkew {
		return errors.New("stale slack request")
	}
	base := []byte("v0:" + ts + ":")
	base = append(base, body...)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(base)
	expected := "v0=" + hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(sig)) {
		return errors.New("signature mismatch")
	}
	return nil
}

func writeSlackJSON(w http.ResponseWriter, payload map[string]any) {
	var buf bytes.Buffer
	_ = json.NewEncoder(&buf).Encode(payload)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(buf.Bytes())
}

func parseAllowedUsers(raw string) map[string]bool {
	out := map[string]bool{}
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			out[part] = true
		}
	}
	return out
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func mustEnv(key string) string {
	value := os.Getenv(key)
	if value == "" {
		log.Fatalf("%s is required", key)
	}
	return value
}
