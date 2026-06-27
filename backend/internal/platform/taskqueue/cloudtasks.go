package taskqueue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	cloudtasks "cloud.google.com/go/cloudtasks/apiv2"
	taskspb "cloud.google.com/go/cloudtasks/apiv2/cloudtaskspb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type Config struct {
	ProjectID           string
	Location            string
	QueueID             string
	TargetURL           string
	OAuthServiceAccount string
}

type Enqueuer struct {
	client *cloudtasks.Client
	config Config
}

func ConfigFromEnv() (Config, bool, error) {
	cfg := Config{
		ProjectID:           firstNonEmptyEnv("GOATOS_CLOUD_TASKS_PROJECT_ID", "GOOGLE_CLOUD_PROJECT"),
		Location:            firstNonEmptyEnv("GOATOS_CLOUD_TASKS_LOCATION", "GOATOS_REGION"),
		QueueID:             firstNonEmptyEnv("GOATOS_CLOUD_TASKS_QUEUE_ID", "GOATOS_NEAR_TERM_TASK_QUEUE_ID"),
		TargetURL:           strings.TrimSpace(os.Getenv("GOATOS_NOTIFICATION_DISPATCHER_RUN_URL")),
		OAuthServiceAccount: strings.TrimSpace(os.Getenv("GOATOS_CLOUD_TASKS_OAUTH_SERVICE_ACCOUNT")),
	}
	anyConfigured := strings.TrimSpace(os.Getenv("GOATOS_CLOUD_TASKS_PROJECT_ID")) != "" ||
		cfg.Location != "" || cfg.QueueID != "" || cfg.TargetURL != "" || cfg.OAuthServiceAccount != ""
	if !anyConfigured {
		return Config{}, false, nil
	}
	if cfg.ProjectID == "" || cfg.Location == "" || cfg.QueueID == "" || cfg.TargetURL == "" || cfg.OAuthServiceAccount == "" {
		return Config{}, false, errors.New("cloud tasks config requires project, location, queue, target URL, and OAuth service account")
	}
	return cfg, true, nil
}

func NewEnqueuer(ctx context.Context, cfg Config) (*Enqueuer, error) {
	client, err := cloudtasks.NewClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("cloud tasks client: %w", err)
	}
	return &Enqueuer{client: client, config: cfg}, nil
}

func (e *Enqueuer) Close() error {
	if e == nil || e.client == nil {
		return nil
	}
	return e.client.Close()
}

func (e *Enqueuer) EnqueueJSONPost(ctx context.Context, taskID string, payload any, scheduleAt time.Time) error {
	if e == nil || e.client == nil {
		return errors.New("cloud tasks enqueuer is not configured")
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode cloud task payload: %w", err)
	}
	parent := fmt.Sprintf("projects/%s/locations/%s/queues/%s", e.config.ProjectID, e.config.Location, e.config.QueueID)
	task := &taskspb.Task{
		Name: fmt.Sprintf("%s/tasks/%s", parent, SafeTaskID(taskID)),
		MessageType: &taskspb.Task_HttpRequest{
			HttpRequest: &taskspb.HttpRequest{
				HttpMethod: taskspb.HttpMethod_POST,
				Url:        e.config.TargetURL,
				Headers: map[string]string{
					"Content-Type": "application/json",
				},
				Body: body,
				AuthorizationHeader: &taskspb.HttpRequest_OauthToken{
					OauthToken: &taskspb.OAuthToken{
						ServiceAccountEmail: e.config.OAuthServiceAccount,
						Scope:               "https://www.googleapis.com/auth/cloud-platform",
					},
				},
			},
		},
	}
	if !scheduleAt.IsZero() {
		task.ScheduleTime = timestamppb.New(scheduleAt.UTC())
	}
	_, err = e.client.CreateTask(ctx, &taskspb.CreateTaskRequest{Parent: parent, Task: task})
	if status.Code(err) == codes.AlreadyExists {
		return nil
	}
	if err != nil {
		return fmt.Errorf("create cloud task %s: %w", task.Name, err)
	}
	return nil
}

var unsafeTaskID = regexp.MustCompile(`[^A-Za-z0-9_-]+`)

func SafeTaskID(raw string) string {
	id := unsafeTaskID.ReplaceAllString(strings.TrimSpace(raw), "-")
	id = strings.Trim(id, "-_")
	if id == "" {
		id = "task"
	}
	if len(id) > 500 {
		id = id[:500]
	}
	return id
}

func firstNonEmptyEnv(keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return ""
}
