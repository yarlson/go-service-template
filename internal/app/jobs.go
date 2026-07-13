package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivertype"
)

type JobListOptions struct {
	Queue string
	State string
	Limit int
}

type listedJob struct {
	ID          int64              `json:"id"`
	Kind        string             `json:"kind"`
	Queue       string             `json:"queue"`
	State       rivertype.JobState `json:"state"`
	Attempt     int                `json:"attempt"`
	MaxAttempts int                `json:"maxAttempts"`
	CreatedAt   time.Time          `json:"createdAt"`
	ScheduledAt time.Time          `json:"scheduledAt"`
	AttemptedAt *time.Time         `json:"attemptedAt,omitempty"`
	FinalizedAt *time.Time         `json:"finalizedAt,omitempty"`
}

func ListJobs(ctx context.Context, databaseURL string, output io.Writer, options JobListOptions) error {
	if options.Limit < 1 || options.Limit > 1000 {
		return fmt.Errorf("job list limit must be between 1 and 1000")
	}

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return fmt.Errorf("create PostgreSQL pool: %w", err)
	}
	defer pool.Close()

	client, err := river.NewClient(riverpgxv5.New(pool), &river.Config{})
	if err != nil {
		return fmt.Errorf("create River query client: %w", err)
	}
	params := river.NewJobListParams().First(options.Limit)
	if options.Queue != "" {
		params = params.Queues(options.Queue)
	}
	if options.State != "" {
		state, err := parseJobState(options.State)
		if err != nil {
			return err
		}
		params = params.States(state)
	}

	result, err := client.JobList(ctx, params)
	if err != nil {
		return fmt.Errorf("list River jobs: %w", err)
	}
	jobs := make([]listedJob, 0, len(result.Jobs))
	for _, job := range result.Jobs {
		jobs = append(jobs, listedJob{
			ID: job.ID, Kind: job.Kind, Queue: job.Queue, State: job.State,
			Attempt: job.Attempt, MaxAttempts: job.MaxAttempts,
			CreatedAt: job.CreatedAt, ScheduledAt: job.ScheduledAt,
			AttemptedAt: job.AttemptedAt, FinalizedAt: job.FinalizedAt,
		})
	}
	if err := json.NewEncoder(output).Encode(jobs); err != nil {
		return fmt.Errorf("encode jobs: %w", err)
	}
	return nil
}

func parseJobState(value string) (rivertype.JobState, error) {
	state := rivertype.JobState(value)
	for _, allowed := range rivertype.JobStates() {
		if state == allowed {
			return state, nil
		}
	}
	return "", fmt.Errorf("unknown job state %q", value)
}
