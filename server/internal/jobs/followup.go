package jobs

import (
	"context"
	"math/rand/v2"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
)

// Inserter is the part of a River client that queues jobs.
type Inserter interface {
	Insert(ctx context.Context, args river.JobArgs, opts *river.InsertOpts) (*rivertype.JobInsertResult, error)
}

// followUpper is a per-library job that can queue one more run behind a running one.
type followUpper interface {
	river.JobArgs
	followUp() river.JobArgs
}

// Enqueue queues a per-library job. River's uniqueness has to cover running jobs
// (a requirement of River itself), so a request made while the same job runs would
// be dropped and the change that caused it (new files, new items) would wait for the
// next periodic run. Such a request queues a follow-up instead: the same arguments
// marked FollowUp, unique on their own, so at most one waits behind the running job.
func Enqueue(ctx context.Context, c Inserter, args river.JobArgs) error {
	res, err := c.Insert(ctx, args, nil)
	if err != nil {
		return err
	}
	f, ok := args.(followUpper)
	if !ok || !res.UniqueSkippedAsDuplicate || res.Job == nil || res.Job.State != rivertype.JobStateRunning {
		return nil
	}
	_, err = c.Insert(ctx, f.followUp(), nil)
	return err
}

// waitForSibling snoozes a job while another run of the same kind for the same
// library is still working (a follow-up must not run beside the job it follows). The
// pause is jittered so two jobs that started together do not keep meeting again.
func waitForSibling(ctx context.Context, pool *pgxpool.Pool, kind string, jobID int64, libraryID uuid.UUID) error {
	if pool == nil {
		return nil
	}
	var busy bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM river_job WHERE state = 'running' AND kind = $1
		AND id <> $2 AND args->>'library_id' = $3)`, kind, jobID, libraryID.String()).Scan(&busy); err != nil {
		return err
	}
	if busy {
		return river.JobSnooze(20*time.Second + time.Duration(rand.Int64N(int64(20*time.Second)))) //nolint:gosec // jitter, not a secret
	}
	return nil
}
