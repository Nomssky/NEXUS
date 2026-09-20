// Package scheduler implements the NEXUS Scheduling & Resource Runtime (C11).
//
// The scheduler manages job queues, task leasing, priority ordering, and
// basic resource allocation. It ensures tasks are executed in order of
// priority while respecting business isolation and resource budgets.
//
// The scheduler does NOT execute tasks itself — it leases them to agents.
// It does NOT own agent identity or model/provider selection.
package scheduler

import (
	"fmt"
	"sort"
	"time"
)

// JobStatus tracks the lifecycle of a scheduled job.
type JobStatus string

const (
	JobStatusPending   JobStatus = "pending"
	JobStatusQueued    JobStatus = "queued"
	JobStatusLeased    JobStatus = "leased"
	JobStatusRunning   JobStatus = "running"
	JobStatusCompleted JobStatus = "completed"
	JobStatusFailed    JobStatus = "failed"
	JobStatusExpired   JobStatus = "expired"
)

// Priority defines job execution order (higher = more important).
type Priority int

const (
	PriorityLow      Priority = 1
	PriorityNormal   Priority = 5
	PriorityHigh     Priority = 8
	PriorityCritical Priority = 10
)

// Job represents a unit of work in the scheduler queue.
type Job struct {
	ID         string     `json:"id"`
	TaskID     string     `json:"task_id"`
	WorkflowID string     `json:"workflow_id"`
	BusinessID string     `json:"business_id"`
	Priority   Priority   `json:"priority"`
	Status     JobStatus  `json:"status"`
	AgentID    string     `json:"agent_id,omitempty"`
	LeasedAt   *time.Time `json:"leased_at,omitempty"`
	Deadline   *time.Time `json:"deadline,omitempty"`
	MaxRetries int        `json:"max_retries"`
	RetryCount int        `json:"retry_count"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

// Lease represents a lease on a job to an agent.
type Lease struct {
	ID        string    `json:"id"`
	JobID     string    `json:"job_id"`
	AgentID   string    `json:"agent_id"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}

// Scheduler manages job queues and task leasing.
// It ensures fair scheduling across businesses and priorities.
type Scheduler struct {
	jobs    map[string]*Job
	leases  map[string]*Lease
	queue   []*Job // sorted by priority (descending), then creation time
	now     func() time.Time
	counter int64
}

// NewScheduler creates a new scheduler.
func NewScheduler() *Scheduler {
	return &Scheduler{
		jobs:   make(map[string]*Job),
		leases: make(map[string]*Lease),
		now:    time.Now,
	}
}

// NewSchedulerWithClock creates a new scheduler with an injectable clock.
func NewSchedulerWithClock(now func() time.Time) *Scheduler {
	return &Scheduler{
		jobs:   make(map[string]*Job),
		leases: make(map[string]*Lease),
		now:    now,
	}
}

// SubmitJob adds a job to the scheduler queue.
func (s *Scheduler) SubmitJob(
	taskID, workflowID, businessID string,
	priority Priority,
	deadline *time.Time,
) (*Job, error) {
	if taskID == "" {
		return nil, fmt.Errorf("task ID is required")
	}

	now := s.now()
	job := &Job{
		ID:         fmt.Sprintf("job-%d", now.UnixNano()),
		TaskID:     taskID,
		WorkflowID: workflowID,
		BusinessID: businessID,
		Priority:   priority,
		Status:     JobStatusQueued,
		MaxRetries: 3,
		Deadline:   deadline,
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	s.jobs[job.ID] = job
	s.queue = append(s.queue, job)
	s.sortQueue()
	return job, nil
}

// LeaseJob leases the next available job to an agent.
// Returns nil if no jobs are available.
func (s *Scheduler) LeaseJob(agentID string, businessID string) *Job {
	for _, job := range s.queue {
		if job.Status != JobStatusQueued {
			continue
		}
		// Business isolation: only lease jobs from the same business
		if businessID != "" && job.BusinessID != businessID {
			continue
		}
		// Check deadline
		if job.Deadline != nil && s.now().After(*job.Deadline) {
			job.Status = JobStatusExpired
			job.UpdatedAt = s.now()
			continue
		}

		now := s.now()
		job.Status = JobStatusLeased
		job.AgentID = agentID
		job.LeasedAt = &now
		job.UpdatedAt = now

		lease := &Lease{
			ID:        fmt.Sprintf("lease-%d", now.UnixNano()),
			JobID:     job.ID,
			AgentID:   agentID,
			ExpiresAt: now.Add(30 * time.Minute),
			CreatedAt: now,
		}
		s.leases[lease.ID] = lease

		return job
	}
	return nil
}

// CompleteJob marks a job as completed.
func (s *Scheduler) CompleteJob(jobID string) error {
	job, ok := s.jobs[jobID]
	if !ok {
		return fmt.Errorf("job %s not found", jobID)
	}

	job.Status = JobStatusCompleted
	job.UpdatedAt = s.now()
	return nil
}

// FailJob marks a job as failed and optionally requeues it.
func (s *Scheduler) FailJob(jobID string, requeue bool) error {
	job, ok := s.jobs[jobID]
	if !ok {
		return fmt.Errorf("job %s not found", jobID)
	}

	if requeue && job.RetryCount < job.MaxRetries {
		job.RetryCount++
		job.Status = JobStatusQueued
		job.AgentID = ""
		job.LeasedAt = nil
		job.UpdatedAt = s.now()
		s.sortQueue()
	} else {
		job.Status = JobStatusFailed
		job.UpdatedAt = s.now()
	}

	return nil
}

// GetJob returns a job by ID.
func (s *Scheduler) GetJob(jobID string) (*Job, bool) {
	job, ok := s.jobs[jobID]
	return job, ok
}

// QueueLength returns the number of queued jobs.
func (s *Scheduler) QueueLength() int {
	count := 0
	for _, job := range s.jobs {
		if job.Status == JobStatusQueued {
			count++
		}
	}
	return count
}

// sortQueue sorts the queue by priority (descending) then creation time (ascending).
func (s *Scheduler) sortQueue() {
	sort.Slice(s.queue, func(i, j int) bool {
		if s.queue[i].Priority != s.queue[j].Priority {
			return s.queue[i].Priority > s.queue[j].Priority
		}
		return s.queue[i].CreatedAt.Before(s.queue[j].CreatedAt)
	})
}

// JobCount returns the total number of jobs.
func (s *Scheduler) JobCount() int {
	return len(s.jobs)
}
