package stream

import (
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"

	"github.com/streamero-app/streamero/server/internal/library"
)

func TestPickFileSkipsVersionsGoneFromDisk(t *testing.T) {
	old := library.MediaFile{ID: uuid.New(), Path: "/media/downloads/ep.mkv"}
	moved := library.MediaFile{ID: uuid.New(), Path: "/media/tv/Show/ep.mkv"}
	onDisk := func(p string) bool { return p == moved.Path }

	got, err := pickFile([]library.MediaFile{old, moved}, nil, onDisk)
	if err != nil || got.ID != moved.ID {
		t.Fatalf("best version: %v %v", got.Path, err)
	}
	got, err = pickFile([]library.MediaFile{old, moved}, &old.ID, onDisk)
	if err != nil || got.ID != moved.ID {
		t.Fatalf("requested version gone, another must play: %v %v", got.Path, err)
	}
	if _, err := pickFile([]library.MediaFile{old}, nil, onDisk); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("nothing on disk: %v", err)
	}
}
