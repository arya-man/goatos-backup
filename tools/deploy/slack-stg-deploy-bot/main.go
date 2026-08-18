package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const maxSlackSkew = 5 * time.Minute

type config struct {
	ProjectID       string
	ProjectNumber   string
	Location        string
	TriggerID       string
	SigningSecret   string
	AllowedUsers    map[string]bool
	ConsoleAuthUser string
	GitHubPATSecret string
	GitHubOwner     string
	GitHubRepo      string
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
	State struct {
		Values map[string]map[string]struct {
			Type            string `json:"type"`
			SelectedOptions []struct {
				Value string `json:"value"`
			} `json:"selected_options"`
		} `json:"values"`
	} `json:"state"`
}

type cloudBuildRunResponse struct {
	Metadata struct {
		Build struct {
			ID string `json:"id"`
		} `json:"build"`
	} `json:"metadata"`
}

type cloudBuildListResponse struct {
	Builds []cloudBuildListBuild `json:"builds"`
}

type cloudBuildListBuild struct {
	ID             string            `json:"id"`
	Status         string            `json:"status"`
	BuildTriggerID string            `json:"buildTriggerId"`
	Substitutions  map[string]string `json:"substitutions"`
}

type cloudBuildGetBuild struct {
	ID            string            `json:"id"`
	Status        string            `json:"status"`
	CreateTime    string            `json:"createTime"`
	FinishTime    string            `json:"finishTime"`
	Substitutions map[string]string `json:"substitutions"`
	Steps         []struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	} `json:"steps"`
}

type androidReleaseVersion struct {
	Name      string
	Code      int
	CommitSHA string
}

type githubContentResponse struct {
	SHA      string `json:"sha"`
	Content  string `json:"content"`
	Encoding string `json:"encoding"`
}

type githubUpdateRequest struct {
	Message   string `json:"message"`
	Content   string `json:"content"`
	SHA       string `json:"sha"`
	Branch    string `json:"branch"`
	Committer struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	} `json:"committer"`
}

type githubUpdateResponse struct {
	Commit struct {
		SHA string `json:"sha"`
	} `json:"commit"`
}

func main() {
	cfg := config{
		ProjectID:       env("PROJECT_ID", "goatos-stg"),
		ProjectNumber:   env("PROJECT_NUMBER", "514832198871"),
		Location:        env("TRIGGER_LOCATION", "global"),
		TriggerID:       mustEnv("TRIGGER_ID"),
		SigningSecret:   mustEnv("SLACK_SIGNING_SECRET"),
		AllowedUsers:    parseAllowedUsers(os.Getenv("SLACK_ALLOWED_USER_IDS")),
		ConsoleAuthUser: env("CONSOLE_AUTHUSER", "ravi@mesha.sg"),
		GitHubPATSecret: env("GITHUB_PAT_SECRET", "goatos-github-pat"),
		GitHubOwner:     env("GITHUB_OWNER", "vgoats"),
		GitHubRepo:      env("GITHUB_REPO", "goatos"),
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

func (cfg config) bumpAndroidReleaseVersion(ctx context.Context, triggeredBy string) (androidReleaseVersion, error) {
	pat, err := cfg.secret(ctx, cfg.GitHubPATSecret)
	if err != nil {
		return androidReleaseVersion{}, err
	}
	if strings.TrimSpace(pat) == "" {
		return androidReleaseVersion{}, fmt.Errorf("secret %s is empty", cfg.GitHubPATSecret)
	}

	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		next, err := cfg.bumpAndroidReleaseVersionOnce(ctx, strings.TrimSpace(pat), triggeredBy)
		if err == nil {
			return next, nil
		}
		lastErr = err
		if !strings.Contains(err.Error(), "409") {
			break
		}
		time.Sleep(time.Duration(attempt) * 500 * time.Millisecond)
	}
	return androidReleaseVersion{}, lastErr
}

func (cfg config) bumpAndroidReleaseVersionOnce(ctx context.Context, pat, triggeredBy string) (androidReleaseVersion, error) {
	const path = "apps/goatos-android/app/build.gradle.kts"
	endpoint := fmt.Sprintf("https://api.github.com/repos/%s/%s/contents/%s?ref=main", cfg.GitHubOwner, cfg.GitHubRepo, path)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return androidReleaseVersion{}, err
	}
	req.Header.Set("Authorization", "Bearer "+pat)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return androidReleaseVersion{}, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<22))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return androidReleaseVersion{}, fmt.Errorf("github contents get returned %s: %s", resp.Status, strings.TrimSpace(string(respBody)))
	}

	var current githubContentResponse
	if err := json.Unmarshal(respBody, &current); err != nil {
		return androidReleaseVersion{}, err
	}
	if current.Encoding != "base64" {
		return androidReleaseVersion{}, fmt.Errorf("github contents encoding %q is not base64", current.Encoding)
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(current.Content, "\n", ""))
	if err != nil {
		return androidReleaseVersion{}, err
	}

	updated, next, err := nextAndroidVersionFile(string(decoded))
	if err != nil {
		return androidReleaseVersion{}, err
	}

	var update githubUpdateRequest
	update.Message = fmt.Sprintf("chore(android): bump STG release to %s (%d)", next.Name, next.Code)
	update.Content = base64.StdEncoding.EncodeToString([]byte(updated))
	update.SHA = current.SHA
	update.Branch = "main"
	update.Committer.Name = "Goat OS Deploy Bot"
	update.Committer.Email = "deploy@vgoats.com"

	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(update); err != nil {
		return androidReleaseVersion{}, err
	}
	putReq, err := http.NewRequestWithContext(ctx, http.MethodPut, strings.Split(endpoint, "?")[0], bytes.NewReader(buf.Bytes()))
	if err != nil {
		return androidReleaseVersion{}, err
	}
	putReq.Header.Set("Authorization", "Bearer "+pat)
	putReq.Header.Set("Accept", "application/vnd.github+json")
	putReq.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	putReq.Header.Set("Content-Type", "application/json")

	putResp, err := http.DefaultClient.Do(putReq)
	if err != nil {
		return androidReleaseVersion{}, err
	}
	defer putResp.Body.Close()
	putBody, _ := io.ReadAll(io.LimitReader(putResp.Body, 1<<20))
	if putResp.StatusCode < 200 || putResp.StatusCode >= 300 {
		return androidReleaseVersion{}, fmt.Errorf("github contents update returned %s: %s", putResp.Status, strings.TrimSpace(string(putBody)))
	}
	var updatedContent githubUpdateResponse
	if err := json.Unmarshal(putBody, &updatedContent); err != nil {
		return androidReleaseVersion{}, err
	}
	if updatedContent.Commit.SHA == "" {
		return androidReleaseVersion{}, errors.New("github contents update did not return a commit SHA")
	}

	log.Printf("Android release version bumped by %s to %s (%d)", triggeredBy, next.Name, next.Code)
	next.CommitSHA = updatedContent.Commit.SHA
	return next, nil
}

func nextAndroidVersionFile(content string) (string, androidReleaseVersion, error) {
	codeRe := regexp.MustCompile(`(?s)(val releaseVersionCode = \([\s\S]*?\)\s*\?\.\s*takeIf \{ it\.isNotBlank\(\) \}\s*\?\.\s*toInt\(\)\s*\?: )(\d+)`)
	nameRe := regexp.MustCompile(`(?s)(val releaseVersionName = \([\s\S]*?\)\s*\?\.\s*takeIf \{ it\.isNotBlank\(\) \}\s*\?: ")(0\.1\.)(\d+)(")`)

	codeMatch := codeRe.FindStringSubmatch(content)
	nameMatch := nameRe.FindStringSubmatch(content)
	if len(codeMatch) != 3 || len(nameMatch) != 5 {
		return "", androidReleaseVersion{}, errors.New("could not find checked-in Android release version fields")
	}

	code, err := strconv.Atoi(codeMatch[2])
	if err != nil {
		return "", androidReleaseVersion{}, err
	}
	patch, err := strconv.Atoi(nameMatch[3])
	if err != nil {
		return "", androidReleaseVersion{}, err
	}

	next := androidReleaseVersion{
		Name: fmt.Sprintf("%s%d", nameMatch[2], patch+1),
		Code: code + 1,
	}
	updated := codeRe.ReplaceAllString(content, fmt.Sprintf("${1}%d", next.Code))
	updated = nameRe.ReplaceAllString(updated, fmt.Sprintf("${1}%s${4}", next.Name))
	return updated, next, nil
}

func (cfg config) secret(ctx context.Context, secretName string) (string, error) {
	token, err := metadataToken(ctx)
	if err != nil {
		return "", err
	}
	endpoint := fmt.Sprintf("https://secretmanager.googleapis.com/v1/projects/%s/secrets/%s/versions/latest:access", cfg.ProjectID, secretName)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("secret access returned %s: %s", resp.Status, strings.TrimSpace(string(respBody)))
	}
	var parsed struct {
		Payload struct {
			Data string `json:"data"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", err
	}
	decoded, err := base64.StdEncoding.DecodeString(parsed.Payload.Data)
	if err != nil {
		return "", err
	}
	return string(decoded), nil
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
	if len(payload.Actions) == 0 {
		writeSlackJSON(w, map[string]any{
			"response_type": "ephemeral",
			"text":          "Unknown deploy action.",
		})
		return
	}

	deploySTG, mobileDistribution, actionLabel, err := payload.deployMode()
	if err != nil {
		writeSlackJSON(w, map[string]any{
			"response_type": "ephemeral",
			"text":          "Unknown deploy action.",
		})
		return
	}

	triggeredBy := slackUserLabel(payload.User.ID, payload.User.Username, payload.User.Name)
	responseURL := payload.ResponseURL
	writeSlackJSON(w, map[string]any{
		"response_type":    "in_channel",
		"replace_original": true,
		"text":             fmt.Sprintf("%s request accepted from `main` by <@%s>. Checking active deploys now.", actionLabel, payload.User.ID),
		"blocks":           deployAcceptedBlocks(triggeredBy, actionLabel, deploySTG, mobileDistribution),
	})

	go cfg.startDeployAsync(responseURL, deploySTG, mobileDistribution, payload.User.ID, triggeredBy, actionLabel)
}

func (cfg config) startDeployAsync(responseURL string, deploySTG, mobileDistribution bool, slackUserID, triggeredBy, actionLabel string) {
	cfg.postSlackResponse(responseURL, map[string]any{
		"response_type":    "in_channel",
		"replace_original": false,
		"text":             fmt.Sprintf("%s request received from `main` by %s. Starting deploy checks now.", actionLabel, triggeredBy),
		"blocks":           deployQueuedBlocks(triggeredBy, actionLabel, deploySTG, mobileDistribution),
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	active, err := cfg.activeDeployBuild(ctx)
	if err != nil {
		log.Printf("active build check failed: %v", err)
		cfg.postSlackResponse(responseURL, map[string]any{
			"response_type": "ephemeral",
			"text":          fmt.Sprintf("Could not check active deployments: `%s`", err.Error()),
		})
		return
	}
	if active.ID != "" {
		buildURL := cfg.cloudBuildURL(active.ID)
		cfg.postSlackResponse(responseURL, map[string]any{
			"response_type":    "in_channel",
			"replace_original": true,
			"text":             fmt.Sprintf("Goat OS deployment already running: `%s`", active.Status),
			"blocks":           deployAlreadyRunningBlocks(active, buildURL, cfg.cloudDeployURL()),
		})
		return
	}

	sourceCommitSHA := ""
	if mobileDistribution {
		next, err := cfg.bumpAndroidReleaseVersion(ctx, triggeredBy)
		if err != nil {
			log.Printf("android version bump failed: %v", err)
			cfg.postSlackResponse(responseURL, map[string]any{
				"response_type": "ephemeral",
				"text":          fmt.Sprintf("Failed to bump Android release version before deploy: `%s`", err.Error()),
			})
			return
		}
		log.Printf("bumped Android STG release to %s (%d)", next.Name, next.Code)
		sourceCommitSHA = next.CommitSHA
	}

	buildID, err := cfg.runTrigger(ctx, deploySTG, mobileDistribution, slackUserID, triggeredBy, sourceCommitSHA)
	if err != nil {
		log.Printf("run trigger failed: %v", err)
		cfg.postSlackResponse(responseURL, map[string]any{
			"response_type": "ephemeral",
			"text":          fmt.Sprintf("Failed to start %s: `%s`", actionLabel, err.Error()),
		})
		return
	}

	buildURL := cfg.cloudBuildURL(buildID)
	deployURL := cfg.cloudDeployURL()
	cfg.postSlackResponse(responseURL, map[string]any{
		"response_type":    "in_channel",
		"replace_original": true,
		"text":             fmt.Sprintf("%s from `main` started by %s.\nCloud Build: %s", actionLabel, triggeredBy, buildURL),
		"blocks":           deployStartedBlocks(triggeredBy, actionLabel, deploySTG, mobileDistribution, buildURL, deployURL),
	})
	go cfg.monitorBuild(responseURL, buildID, triggeredBy, actionLabel, deploySTG, mobileDistribution)
}

func deployQueuedBlocks(triggeredBy, actionLabel string, deploySTG, mobileDistribution bool) []map[string]any {
	return []map[string]any{
		{
			"type": "section",
			"text": map[string]string{
				"type": "mrkdwn",
				"text": fmt.Sprintf("*%s request received* by %s\nSTG deploy: `%t`\nMobile distribution: `%t`\n\nStarting active-deploy checks now.", actionLabel, triggeredBy, deploySTG, mobileDistribution),
			},
		},
	}
}

func deployAcceptedBlocks(triggeredBy, actionLabel string, deploySTG, mobileDistribution bool) []map[string]any {
	return []map[string]any{
		{
			"type": "section",
			"text": map[string]string{
				"type": "mrkdwn",
				"text": fmt.Sprintf("*%s request received* by %s\nSTG deploy: `%t`\nMobile distribution: `%t`\n\nChecking whether another deploy is already active.", actionLabel, triggeredBy, deploySTG, mobileDistribution),
			},
		},
	}
}

func deployAlreadyRunningBlocks(build cloudBuildListBuild, buildURL, deployURL string) []map[string]any {
	mode := buildMode(build)
	return []map[string]any{
		{
			"type": "section",
			"text": map[string]string{
				"type": "mrkdwn",
				"text": fmt.Sprintf("*Goat OS deploy already running*\nMode: `%s`\nStatus: `%s`\nBuild: `%s`", mode, build.Status, build.ID),
			},
		},
		{
			"type": "actions",
			"elements": []map[string]any{
				{"type": "button", "text": map[string]string{"type": "plain_text", "text": "Cloud Build logs"}, "url": buildURL},
				{"type": "button", "text": map[string]string{"type": "plain_text", "text": "Cloud Deploy"}, "url": deployURL},
			},
		},
	}
}

func (cfg config) postSlackResponse(responseURL string, payload map[string]any) {
	if responseURL == "" {
		log.Printf("slack response_url is empty; cannot post async deploy response")
		return
	}
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(payload); err != nil {
		log.Printf("encode slack async response failed: %v", err)
		return
	}
	req, err := http.NewRequest(http.MethodPost, responseURL, bytes.NewReader(buf.Bytes()))
	if err != nil {
		log.Printf("create slack async response failed: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("post slack async response failed: %v", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		log.Printf("post slack async response returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
}

func deployStartedBlocks(triggeredBy, actionLabel string, deploySTG, mobileDistribution bool, buildURL, deployURL string) []map[string]any {
	links := fmt.Sprintf("<%s|Cloud Build logs>", buildURL)
	if deploySTG {
		links += fmt.Sprintf(" | <%s|Cloud Deploy rollout>", deployURL)
	}
	return []map[string]any{
		{
			"type": "section",
			"text": map[string]string{
				"type": "mrkdwn",
				"text": fmt.Sprintf("*%s in progress* by %s\nSTG deploy: `%t`\nMobile distribution: `%t`\n\nThe deploy buttons will return only after success or failure.\n%s", actionLabel, triggeredBy, deploySTG, mobileDistribution, links),
			},
		},
	}
}

func (cfg config) monitorBuild(responseURL, buildID, triggeredBy, actionLabel string, deploySTG, mobileDistribution bool) {
	if buildID == "" || buildID == "pending" {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 130*time.Minute)
	defer cancel()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			cfg.postSlackResponse(responseURL, map[string]any{
				"response_type":    "in_channel",
				"replace_original": false,
				"text":             fmt.Sprintf("%s monitor timed out for build `%s`; check Cloud Build logs.", actionLabel, buildID),
				"attachments":      deployTerminalAttachments("TIMED_OUT", triggeredBy, actionLabel, buildID, deploySTG, mobileDistribution, cfg.cloudBuildURL(buildID), cfg.cloudDeployURL(), nil),
			})
			cfg.postDeployPanel(responseURL)
			return
		case <-ticker.C:
			build, err := cfg.getBuild(ctx, buildID)
			if err != nil {
				log.Printf("build monitor get failed for %s: %v", buildID, err)
				continue
			}
			if !isTerminalBuildStatus(build.Status) {
				continue
			}
			if build.Status == "SUCCESS" {
				return
			}
			cfg.postSlackResponse(responseURL, map[string]any{
				"response_type":    "in_channel",
				"replace_original": false,
				"text":             fmt.Sprintf("%s finished with Cloud Build status `%s` for `%s`.", actionLabel, build.Status, buildID),
				"attachments":      deployTerminalAttachments(build.Status, triggeredBy, actionLabel, buildID, deploySTG, mobileDistribution, cfg.cloudBuildURL(buildID), cfg.cloudDeployURL(), build.Steps),
			})
			cfg.postDeployPanel(responseURL)
			return
		}
	}
}

func (cfg config) getBuild(ctx context.Context, buildID string) (cloudBuildGetBuild, error) {
	token, err := metadataToken(ctx)
	if err != nil {
		return cloudBuildGetBuild{}, err
	}
	endpoint := fmt.Sprintf("https://cloudbuild.googleapis.com/v1/projects/%s/locations/%s/builds/%s", cfg.ProjectID, cfg.Location, buildID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return cloudBuildGetBuild{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return cloudBuildGetBuild{}, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return cloudBuildGetBuild{}, fmt.Errorf("cloud build get returned %s: %s", resp.Status, strings.TrimSpace(string(respBody)))
	}

	var parsed cloudBuildGetBuild
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return cloudBuildGetBuild{}, err
	}
	return parsed, nil
}

func isTerminalBuildStatus(status string) bool {
	switch status {
	case "SUCCESS", "FAILURE", "INTERNAL_ERROR", "TIMEOUT", "CANCELLED", "EXPIRED":
		return true
	default:
		return false
	}
}

func deployTerminalAttachments(status, triggeredBy, actionLabel, buildID string, deploySTG, mobileDistribution bool, buildURL, deployURL string, steps []struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}) []map[string]any {
	title := "Goat OS deploy finished"
	color := "#AAAAAA"
	switch status {
	case "SUCCESS":
		title = "Goat OS deploy succeeded"
		color = "#2EB67D"
	case "FAILURE", "INTERNAL_ERROR", "TIMEOUT", "CANCELLED", "EXPIRED", "TIMED_OUT":
		title = "Goat OS deploy needs attention"
		color = "#E01E5A"
	}
	stepText := "Cloud Build reached a terminal state."
	if len(steps) > 0 {
		var parts []string
		for _, step := range steps {
			if step.Status != "SUCCESS" && step.Status != "" {
				parts = append(parts, fmt.Sprintf("`%s`: `%s`", step.ID, step.Status))
			}
		}
		if len(parts) > 0 {
			stepText = "Non-green steps: " + strings.Join(parts, ", ")
		}
	}

	actions := []map[string]any{
		{"type": "button", "text": "Cloud Build logs", "url": buildURL},
	}
	if deploySTG {
		actions = append(actions, map[string]any{"type": "button", "text": "Cloud Deploy", "url": deployURL})
	}

	return []map[string]any{
		{
			"color": color,
			"title": title,
			"text":  stepText,
			"fields": []map[string]any{
				{"title": "Build", "value": buildID, "short": true},
				{"title": "Status", "value": status, "short": true},
				{"title": "Triggered by", "value": triggeredBy, "short": false},
				{"title": "Mode", "value": deployModeLabel(deploySTG, mobileDistribution), "short": false},
			},
			"actions": actions,
		},
	}
}

func deployModeLabel(deploySTG, mobileDistribution bool) string {
	switch {
	case deploySTG && mobileDistribution:
		return "STG + Android mobile"
	case deploySTG:
		return "STG"
	case mobileDistribution:
		return "Android mobile"
	default:
		return "unknown"
	}
}

func (cfg config) postDeployPanel(responseURL string) {
	cfg.postSlackResponse(responseURL, map[string]any{
		"response_type":    "in_channel",
		"replace_original": false,
		"text":             "Goat OS STG deploy",
		"blocks": []map[string]any{
			{
				"type": "section",
				"text": map[string]string{
					"type": "mrkdwn",
					"text": "*Goat OS STG deploy*\nDeploy the current `main` branch to Google staging, or distribute only the Android STG build.",
				},
			},
			{
				"type":     "actions",
				"block_id": "deploy_options",
				"elements": []map[string]any{
					{
						"type":      "checkboxes",
						"action_id": "deploy_options",
						"options": []map[string]any{
							{
								"text": map[string]string{
									"type": "plain_text",
									"text": "Also distribute Android mobile",
								},
								"description": map[string]string{
									"type": "plain_text",
									"text": "Firebase App Distribution, Play Internal Testing, and mesha.sg/app.apk",
								},
								"value": "mobile_distribution",
							},
						},
					},
				},
			},
			{
				"type": "actions",
				"elements": []map[string]any{
					{
						"type":      "button",
						"text":      map[string]string{"type": "plain_text", "text": "Deploy main to STG"},
						"style":     "primary",
						"action_id": "deploy_goatos_stg_main",
						"value":     "main",
					},
					{
						"type":      "button",
						"text":      map[string]string{"type": "plain_text", "text": "Distribute Android only"},
						"action_id": "deploy_goatos_mobile_only",
						"value":     "mobile",
					},
				},
			},
		},
	})
}

func (cfg config) runTrigger(ctx context.Context, deploySTG, mobileDistribution bool, slackUserID, triggeredBy, sourceCommitSHA string) (string, error) {
	token, err := metadataToken(ctx)
	if err != nil {
		return "", err
	}
	endpoint := fmt.Sprintf("https://cloudbuild.googleapis.com/v1/projects/%s/locations/%s/triggers/%s:run", cfg.ProjectID, cfg.Location, cfg.TriggerID)
	source := map[string]any{
		"branchName": "main",
		"substitutions": map[string]string{
			"_DEPLOY_STG":    strconv.FormatBool(deploySTG),
			"_DEPLOY_MOBILE": strconv.FormatBool(mobileDistribution),
			"_SLACK_USER_ID": slackUserID,
			"_TRIGGERED_BY":  triggeredBy,
		},
	}
	if sourceCommitSHA != "" {
		delete(source, "branchName")
		source["commitSha"] = sourceCommitSHA
	}
	requestBody := map[string]any{
		"source": source,
	}
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(requestBody); err != nil {
		return "", err
	}
	body := bytes.NewReader(buf.Bytes())
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

func (cfg config) activeDeployBuild(ctx context.Context) (cloudBuildListBuild, error) {
	token, err := metadataToken(ctx)
	if err != nil {
		return cloudBuildListBuild{}, err
	}
	endpoint := fmt.Sprintf("https://cloudbuild.googleapis.com/v1/projects/%s/builds?filter=%s", cfg.ProjectID, url.QueryEscape(`(status="QUEUED" OR status="WORKING")`))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return cloudBuildListBuild{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return cloudBuildListBuild{}, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return cloudBuildListBuild{}, fmt.Errorf("cloud build list returned %s: %s", resp.Status, strings.TrimSpace(string(respBody)))
	}

	var parsed cloudBuildListResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return cloudBuildListBuild{}, err
	}
	for _, build := range parsed.Builds {
		if build.BuildTriggerID == cfg.TriggerID && isDeployBuild(build) {
			return build, nil
		}
	}
	return cloudBuildListBuild{}, nil
}

func isDeployBuild(build cloudBuildListBuild) bool {
	return build.Substitutions["_DEPLOY_STG"] == "true" || build.Substitutions["_DEPLOY_MOBILE"] == "true"
}

func buildMode(build cloudBuildListBuild) string {
	deploySTG := build.Substitutions["_DEPLOY_STG"] == "true"
	deployMobile := build.Substitutions["_DEPLOY_MOBILE"] == "true"
	switch {
	case deploySTG && deployMobile:
		return "STG + Android mobile"
	case deploySTG:
		return "STG"
	case deployMobile:
		return "Android mobile"
	default:
		return "unknown"
	}
}

func slackUserLabel(userID, username, name string) string {
	if userID != "" {
		return "<@" + userID + ">"
	}
	if username != "" {
		return "@" + username
	}
	if name != "" {
		return name
	}
	return "unknown Slack user"
}

func (cfg config) cloudBuildURL(buildID string) string {
	values := url.Values{}
	values.Set("project", cfg.consoleProject())
	if cfg.ConsoleAuthUser != "" {
		values.Set("authuser", cfg.ConsoleAuthUser)
	}
	return fmt.Sprintf("https://console.cloud.google.com/cloud-build/builds;region=%s/%s?%s", cfg.Location, buildID, values.Encode())
}

func (cfg config) cloudDeployURL() string {
	values := url.Values{}
	values.Set("project", cfg.consoleProject())
	if cfg.ConsoleAuthUser != "" {
		values.Set("authuser", cfg.ConsoleAuthUser)
	}
	return fmt.Sprintf("https://console.cloud.google.com/deploy/delivery-pipelines/%s/goatos-stg?%s", cfg.Location, values.Encode())
}

func (cfg config) consoleProject() string {
	if cfg.ProjectNumber != "" {
		return cfg.ProjectNumber
	}
	return cfg.ProjectID
}

func (payload slackActionPayload) deployMode() (deploySTG, mobileDistribution bool, actionLabel string, err error) {
	switch payload.Actions[0].ActionID {
	case "deploy_goatos_stg_main":
		return true, payload.hasSelectedOption("mobile_distribution"), "STG deploy", nil
	case "deploy_goatos_mobile_only":
		return false, true, "Android mobile distribution", nil
	default:
		return false, false, "", fmt.Errorf("unknown action %q", payload.Actions[0].ActionID)
	}
}

func (payload slackActionPayload) hasSelectedOption(want string) bool {
	for _, block := range payload.State.Values {
		for _, action := range block {
			for _, option := range action.SelectedOptions {
				if option.Value == want {
					return true
				}
			}
		}
	}
	return false
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
