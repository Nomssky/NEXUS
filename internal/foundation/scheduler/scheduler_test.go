package scheduler

import (
	"testing"
	"time"
)

// TEST-M5-014: Submit job to queue
func TestSchedulerSubmitJob(t *testing.T) {
	s := NewScheduler()
	job, err := s.SubmitJob("task-1", "wf-1", "biz-1", PriorityNormal, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if job.Status != JobStatusQueued {
		t.Errorf("expected queued, got %v", job.Status)
	}
	if job.Priority != PriorityNormal {
		t.Errorf("expected normal priority, got %v", job.Priority)
	}
}

// TEST-M5-015: Lease job to agent
func TestSchedulerLeaseJob(t *testing.T) {
	s := NewScheduler()
	job, _ := s.SubmitJob("task-1", "wf-1", "biz-1", PriorityNormal, nil)

	leased := s.LeaseJob("agent-1", "biz-1")
	if leased == nil {
		t.Fatal("expected to lease a job")
	}
	if leased.ID != job.ID {
		t.Errorf("expected to lease job %s, got %s", job.ID, leased.ID)
	}
	if leased.AgentID != "agent-1" {
		t.Errorf("expected agent-1, got %v", leased.AgentID)
	}
	if job.Status != JobStatusLeased {
		t.Errorf("expected leased status, got %v", job.Status)
	}
}

// TEST-M5-016: Priority ordering
func TestSchedulerPriorityOrdering(t *testing.T) {
	s := NewScheduler()
	s.SubmitJob("task-low", "wf-1", "biz-1", PriorityLow, nil)
	s.SubmitJob("task-high", "wf-1", "biz-1", PriorityHigh, nil)
	s.SubmitJob("task-normal", "wf-1", "biz-1", PriorityNormal, nil)

	leased := s.LeaseJob("agent-1", "biz-1")
	if leased.Priority != PriorityHigh {
		t.Errorf("expected high priority first, got %v", leased.Priority)
	}
}

// TEST-M5-017: Business isolation in scheduling
func TestSchedulerBusinessIsolation(t *testing.T) {
	s := NewScheduler()
	s.SubmitJob("task-1", "wf-1", "biz-1", PriorityNormal, nil)
	s.SubmitJob("task-2", "wf-2", "biz-2", PriorityHigh, nil)

	// Lease only biz-2 jobs
	leased := s.LeaseJob("agent-1", "biz-2")
	if leased == nil {
		t.Fatal("expected to lease a job")
	}
	if leased.BusinessID != "biz-2" {
		t.Errorf("expected biz-2 job, got %v", leased.BusinessID)
	}
}

// TEST-M5-018: No jobs available
func TestSchedulerNoJobsAvailable(t *testing.T) {
	s := NewScheduler()
	leased := s.LeaseJob("agent-1", "biz-1")
	if leased != nil {
		t.Error("expected nil when no jobs available")
	}
}

// TEST-M5-019: Complete job
func TestSchedulerCompleteJob(t *testing.T) {
	s := NewScheduler()
	job, _ := s.SubmitJob("task-1", "wf-1", "biz-1", PriorityNormal, nil)
	s.LeaseJob("agent-1", "biz-1")

	err := s.CompleteJob(job.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if job.Status != JobStatusCompleted {
		t.Errorf("expected completed, got %v", job.Status)
	}
}

// TEST-M5-020: Fail job with retry
func TestSchedulerFailJobRetry(t *testing.T) {
	s := NewScheduler()
	job, _ := s.SubmitJob("task-1", "wf-1", "biz-1", PriorityNormal, nil)
	s.LeaseJob("agent-1", "biz-1")

	s.FailJob(job.ID, true)
	if job.Status != JobStatusQueued {
		t.Errorf("expected requeued, got %v", job.Status)
	}
	if job.RetryCount != 1 {
		t.Errorf("expected retry count 1, got %d", job.RetryCount)
	}
}

// TEST-M5-021: Fail job without retry
func TestSchedulerFailJobNoRetry(t *testing.T) {
	s := NewScheduler()
	job, _ := s.SubmitJob("task-1", "wf-1", "biz-1", PriorityNormal, nil)
	s.LeaseJob("agent-1", "biz-1")

	s.FailJob(job.ID, false)
	if job.Status != JobStatusFailed {
		t.Errorf("expected failed, got %v", job.Status)
	}
}

// TEST-M5-022: Deadline expiry
func TestSchedulerDeadlineExpiry(t *testing.T) {
	now := time.Now()
	past := now.Add(-1 * time.Hour)

	s := NewSchedulerWithClock(func() time.Time { return now })
	s.SubmitJob("task-1", "wf-1", "biz-1", PriorityNormal, &past)

	leased := s.LeaseJob("agent-1", "biz-1")
	if leased != nil {
		t.Error("expected nil for expired job")
	}
}

// TEST-M5-023: Queue length
func TestSchedulerQueueLength(t *testing.T) {
	s := NewScheduler()
	s.SubmitJob("task-1", "wf-1", "biz-1", PriorityNormal, nil)
	s.SubmitJob("task-2", "wf-1", "biz-1", PriorityNormal, nil)
	s.SubmitJob("task-3", "wf-1", "biz-1", PriorityNormal, nil)

	if s.QueueLength() != 3 {
		t.Errorf("expected 3, got %d", s.QueueLength())
	}

	s.LeaseJob("agent-1", "biz-1")
	if s.QueueLength() != 2 {
		t.Errorf("expected 2 after lease, got %d", s.QueueLength())
	}
}
