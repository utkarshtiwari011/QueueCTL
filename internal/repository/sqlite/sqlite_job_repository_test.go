package sqlite_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"queuectl/internal/database"
	"queuectl/internal/domain"
	"queuectl/internal/repository"
	"queuectl/internal/repository/sqlite"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func setupTestDB(t *testing.T) *sql.DB {
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)

	err = database.RunMigrations(context.Background(), db)
	require.NoError(t, err)

	return db
}

func TestInsertAndGetByID(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	repo := sqlite.NewSQLiteJobRepository(db)
	ctx := context.Background()

	job := &domain.Job{
		ID:           "test-id-1",
		Type:         "email",
		Payload:      `{"to":"test@example.com"}`,
		Queue:        "default",
		Status:       domain.StatusPending,
		MaxRetries:   3,
		RetriesCount: 0,
		RunAt:        time.Now().Truncate(time.Second),
		CreatedAt:    time.Now().Truncate(time.Second),
		UpdatedAt:    time.Now().Truncate(time.Second),
	}

	err := repo.Insert(ctx, job)
	require.NoError(t, err)

	fetched, err := repo.GetByID(ctx, job.ID)
	require.NoError(t, err)
	assert.Equal(t, job.ID, fetched.ID)
	assert.Equal(t, job.Type, fetched.Type)
	assert.Equal(t, job.Payload, fetched.Payload)
	assert.Equal(t, job.Status, fetched.Status)
	assert.Equal(t, job.MaxRetries, fetched.MaxRetries)
	assert.WithinDuration(t, job.RunAt, fetched.RunAt, 2*time.Second)
}

func TestUpdateJob(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	repo := sqlite.NewSQLiteJobRepository(db)
	ctx := context.Background()

	job := &domain.Job{
		ID:           "test-id-2",
		Type:         "image_resize",
		Payload:      "{}",
		Queue:        "default",
		Status:       domain.StatusPending,
		MaxRetries:   3,
		RetriesCount: 0,
		RunAt:        time.Now().UTC(),
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}

	err := repo.Insert(ctx, job)
	require.NoError(t, err)

	job.Status = domain.StatusRunning
	job.RetriesCount = 1
	job.ErrorMessage = "temporary network failure"

	err = repo.Update(ctx, job)
	require.NoError(t, err)

	fetched, err := repo.GetByID(ctx, job.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.StatusRunning, fetched.Status)
	assert.Equal(t, 1, fetched.RetriesCount)
	assert.Equal(t, "temporary network failure", fetched.ErrorMessage)
}

func TestAcquireNextPendingJob(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	repo := sqlite.NewSQLiteJobRepository(db)
	ctx := context.Background()

	// 1. Job scheduled in the future (should not be acquired)
	futureJob := &domain.Job{
		ID:           "future-job",
		Type:         "email",
		Payload:      "{}",
		Queue:        "default",
		Status:       domain.StatusPending,
		MaxRetries:   3,
		RetriesCount: 0,
		RunAt:        time.Now().Add(1 * time.Hour).UTC(),
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
	err := repo.Insert(ctx, futureJob)
	require.NoError(t, err)

	// 2. Job scheduled now (should be acquired)
	nowJob := &domain.Job{
		ID:           "now-job",
		Type:         "email",
		Payload:      "{}",
		Queue:        "default",
		Status:       domain.StatusPending,
		MaxRetries:   3,
		RetriesCount: 0,
		RunAt:        time.Now().Add(-10 * time.Second).UTC(),
		CreatedAt:    time.Now().Add(-10 * time.Second).UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
	err = repo.Insert(ctx, nowJob)
	require.NoError(t, err)

	acquired, err := repo.AcquireNextPendingJob(ctx, "default")
	require.NoError(t, err)
	require.NotNil(t, acquired)
	assert.Equal(t, "now-job", acquired.ID)
	assert.Equal(t, domain.StatusRunning, acquired.Status)

	// Attempting to acquire again should return nil since no other eligible pending jobs exist
	secondAcquire, err := repo.AcquireNextPendingJob(ctx, "default")
	require.NoError(t, err)
	assert.Nil(t, secondAcquire)
}

func TestTransactions(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	repo := sqlite.NewSQLiteJobRepository(db)
	ctx := context.Background()

	// Transaction that inserts a job and rolls back
	err := repo.WithTx(ctx, func(txCtx context.Context) error {
		job := &domain.Job{
			ID:           "tx-job",
			Type:         "email",
			Payload:      "{}",
			Queue:        "default",
			Status:       domain.StatusPending,
			MaxRetries:   3,
			RetriesCount: 0,
			RunAt:        time.Now().UTC(),
			CreatedAt:    time.Now().UTC(),
			UpdatedAt:    time.Now().UTC(),
		}
		err := repo.Insert(txCtx, job)
		require.NoError(t, err)

		// Return an error to force rollback
		return assert.AnError
	})

	assert.ErrorIs(t, err, assert.AnError)

	// Job should not exist in DB
	_, err = repo.GetByID(ctx, "tx-job")
	assert.Error(t, err)
}

func TestOptimisticConcurrencyControl(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	repo := sqlite.NewSQLiteJobRepository(db)
	ctx := context.Background()

	job := &domain.Job{
		ID:         "occ-job",
		Type:       "email",
		Payload:    "{}",
		Queue:      "default",
		Status:     domain.StatusPending,
		MaxRetries: 3,
		RunAt:      time.Now().Add(-10 * time.Second).UTC(),
		CreatedAt:  time.Now().Add(-10 * time.Second).UTC(),
		UpdatedAt:  time.Now().Add(-10 * time.Second).UTC(),
	}

	err := repo.Insert(ctx, job)
	require.NoError(t, err)

	// Fetch two instances of the same record
	inst1, err := repo.GetByID(ctx, "occ-job")
	require.NoError(t, err)
	inst2, err := repo.GetByID(ctx, "occ-job")
	require.NoError(t, err)

	// Modify inst1 and save (should succeed and update UpdatedAt timestamp)
	inst1.Status = domain.StatusRunning
	err = repo.Update(ctx, inst1)
	require.NoError(t, err)

	// Attempting to update inst2 should fail with ErrConcurrencyConflict because UpdatedAt timestamp is outdated
	inst2.Status = domain.StatusCompleted
	err = repo.Update(ctx, inst2)
	assert.ErrorIs(t, err, repository.ErrConcurrencyConflict)
}

func TestSQLiteJobRepository_ListJobs(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	repo := sqlite.NewSQLiteJobRepository(db)
	ctx := context.Background()

	// Enqueue jobs on default and other queue
	job1 := &domain.Job{ID: "job-l1", Type: "task", Queue: "default", Status: domain.StatusPending, RunAt: time.Now().UTC(), CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	job2 := &domain.Job{ID: "job-l2", Type: "task", Queue: "default", Status: domain.StatusCompleted, RunAt: time.Now().UTC(), CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	job3 := &domain.Job{ID: "job-l3", Type: "task", Queue: "other", Status: domain.StatusPending, RunAt: time.Now().UTC(), CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}

	require.NoError(t, repo.Insert(ctx, job1))
	require.NoError(t, repo.Insert(ctx, job2))
	require.NoError(t, repo.Insert(ctx, job3))

	// List default queue pending
	jobs, err := repo.ListJobs(ctx, "default", domain.StatusPending, "", 10)
	require.NoError(t, err)
	assert.Len(t, jobs, 1)
	assert.Equal(t, "job-l1", jobs[0].ID)

	// List default queue completed
	jobs, err = repo.ListJobs(ctx, "default", domain.StatusCompleted, "", 10)
	require.NoError(t, err)
	assert.Len(t, jobs, 1)
	assert.Equal(t, "job-l2", jobs[0].ID)

	// List all queue with search type filter
	jobs, err = repo.ListJobs(ctx, "", "", "task", 10)
	require.NoError(t, err)
	assert.Len(t, jobs, 3)
}

func TestSQLiteJobRepository_GetQueueStats(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	repo := sqlite.NewSQLiteJobRepository(db)
	ctx := context.Background()

	job1 := &domain.Job{ID: "job-s1", Type: "task", Queue: "q1", Status: domain.StatusPending, RunAt: time.Now().UTC(), CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	job2 := &domain.Job{ID: "job-s2", Type: "task", Queue: "q1", Status: domain.StatusRunning, RunAt: time.Now().UTC(), CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}

	require.NoError(t, repo.Insert(ctx, job1))
	require.NoError(t, repo.Insert(ctx, job2))

	stats, err := repo.GetQueueStats(ctx)
	require.NoError(t, err)
	assert.Len(t, stats, 1)
	assert.Contains(t, stats, "q1")
	assert.Equal(t, 1, stats["q1"][domain.StatusPending])
	assert.Equal(t, 1, stats["q1"][domain.StatusRunning])
}

func TestSQLiteJobRepository_DeleteAndPurge(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	repo := sqlite.NewSQLiteJobRepository(db)
	ctx := context.Background()

	job1 := &domain.Job{ID: "job-d1", Type: "task", Queue: "default", Status: domain.StatusCompleted, RunAt: time.Now().UTC(), CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	job2 := &domain.Job{ID: "job-d2", Type: "task", Queue: "default", Status: domain.StatusPending, RunAt: time.Now().UTC(), CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}

	require.NoError(t, repo.Insert(ctx, job1))
	require.NoError(t, repo.Insert(ctx, job2))

	// Delete job2
	err := repo.Delete(ctx, "job-d2")
	require.NoError(t, err)

	_, err = repo.GetByID(ctx, "job-d2")
	assert.Error(t, err)

	// Purge completed jobs older than -10s (i.e. completed anytime before 10s in the future)
	purged, err := repo.DeleteCompletedJobs(ctx, -10*time.Second)
	require.NoError(t, err)
	assert.Equal(t, int64(1), purged)

	_, err = repo.GetByID(ctx, "job-d1")
	assert.Error(t, err)
}

func TestSQLiteJobRepository_ContextCancellation(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	repo := sqlite.NewSQLiteJobRepository(db)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Pre-cancel context

	job := &domain.Job{ID: "job-cancel", Type: "task", Queue: "default", Status: domain.StatusPending, RunAt: time.Now().UTC(), CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	err := repo.Insert(ctx, job)
	assert.Error(t, err)
}
