package jobs

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"

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
