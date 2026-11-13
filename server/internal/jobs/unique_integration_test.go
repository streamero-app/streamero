package jobs

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivertype"

	"github.com/streamero-app/streamero/server/internal/db"
)

// A finished scan must not block the next one (regression: rescans were silently dropped).
func TestScanJobsAreUniqueOnlyWhileQueuedIntegration(t *testing.T) {
	url := os.Getenv("STREAMERO_TEST_DB_URL")
	if url == "" {
		t.Skip("STREAMERO_TEST_DB_URL not set")
	}
	ctx := context.Background()
	pool, err := db.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	client, err := river.NewClient(riverpgxv5.New(pool), &river.Config{})
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range []river.JobArgs{LibraryScanArgs{LibraryID: uuid.New()}, MetadataRefreshArgs{LibraryID: uuid.New()}} {
		first, err := client.Insert(ctx, args, nil)
		if err != nil || first.UniqueSkippedAsDuplicate {
			t.Fatalf("%s first insert: %+v %v", args.Kind(), first, err)
		}
		t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM river_job WHERE id = $1`, first.Job.ID) })
		// While the first is still waiting, a second is a duplicate.
		dup, err := client.Insert(ctx, args, nil)
		if err != nil || !dup.UniqueSkippedAsDuplicate {
			t.Fatalf("%s duplicate while queued: %+v %v", args.Kind(), dup, err)
		}
		if _, err := pool.Exec(ctx, `UPDATE river_job SET state = 'completed', finalized_at = now() WHERE id = $1`, first.Job.ID); err != nil {
			t.Fatal(err)
		}
		again, err := client.Insert(ctx, args, nil)
		if err != nil || again.UniqueSkippedAsDuplicate {
			t.Fatalf("%s insert after completion was dropped: %+v %v", args.Kind(), again, err)
		}
		t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM river_job WHERE id = $1`, again.Job.ID) })
	}
}

// A request made while the same job runs queues one follow-up (not silently dropped,
// not one per request), and the follow-up waits for the running job.
func TestRequestDuringRunQueuesOneFollowUpIntegration(t *testing.T) {
	url := os.Getenv("STREAMERO_TEST_DB_URL")
	if url == "" {
		t.Skip("STREAMERO_TEST_DB_URL not set")
	}
	ctx := context.Background()
	pool, err := db.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	client, err := river.NewClient(riverpgxv5.New(pool), &river.Config{})
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range []river.JobArgs{LibraryScanArgs{LibraryID: uuid.New()}, MetadataRefreshArgs{LibraryID: uuid.New()},
		MediaAnalyzeArgs{LibraryID: uuid.New()}, SegmentDetectArgs{LibraryID: uuid.New()}} {
		var lib uuid.UUID
		switch a := args.(type) {
		case LibraryScanArgs:
			lib = a.LibraryID
		case MetadataRefreshArgs:
			lib = a.LibraryID
		case MediaAnalyzeArgs:
			lib = a.LibraryID
		case SegmentDetectArgs:
			lib = a.LibraryID
		}
		t.Cleanup(func() {
			_, _ = pool.Exec(context.Background(), `DELETE FROM river_job WHERE args->>'library_id' = $1`, lib.String())
		})
		count := func() int {
			var n int
			_ = pool.QueryRow(ctx, `SELECT count(*) FROM river_job WHERE args->>'library_id' = $1`, lib.String()).Scan(&n)
			return n
		}
		if err := Enqueue(ctx, client, args); err != nil {
			t.Fatal(err)
		}
		// Waiting: a second request is the same job.
		if err := Enqueue(ctx, client, args); err != nil || count() != 1 {
			t.Fatalf("%s while waiting: %d jobs %v", args.Kind(), count(), err)
		}
		var first int64
		if err := pool.QueryRow(ctx, `UPDATE river_job SET state = 'running', attempted_at = now(), attempt = 1
			WHERE args->>'library_id' = $1 RETURNING id`, lib.String()).Scan(&first); err != nil {
			t.Fatal(err)
		}
		// Running: one follow-up is queued, however many requests come.
		for range 3 {
			if err := Enqueue(ctx, client, args); err != nil {
				t.Fatal(err)
			}
		}
		if n := count(); n != 2 {
			t.Fatalf("%s while running: %d jobs, want the running one and one follow-up", args.Kind(), n)
		}
		var follow int64
		if err := pool.QueryRow(ctx, `SELECT id FROM river_job WHERE args->>'library_id' = $1 AND (args->>'follow_up')::bool`,
			lib.String()).Scan(&follow); err != nil {
			t.Fatalf("%s follow-up: %v", args.Kind(), err)
		}
		// The follow-up does not run beside the running job.
		var snooze *rivertype.JobSnoozeError
		if err := waitForSibling(ctx, pool, args.Kind(), follow, lib); !errors.As(err, &snooze) {
			t.Fatalf("%s follow-up while the first runs: %v", args.Kind(), err)
		}
		if _, err := pool.Exec(ctx, `UPDATE river_job SET state = 'completed', finalized_at = now() WHERE id = $1`, first); err != nil {
			t.Fatal(err)
		}
		if err := waitForSibling(ctx, pool, args.Kind(), follow, lib); err != nil {
			t.Fatalf("%s follow-up after the first finished: %v", args.Kind(), err)
		}
	}
}
