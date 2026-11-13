package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/streamero-app/streamero/server/internal/db"
)

// CreateOptions say what kind of backup to write.
type CreateOptions struct {
	Kind          string // KindScheduled, KindManual or KindPreRestore
	IncludeImages bool
}

// Create writes a new backup into Dir and returns its name. It fails with ErrBusy while
// another backup or a restore runs.
func (s *Service) Create(ctx context.Context, opts CreateOptions) (Info, error) {
	unlock, err := s.lock(ctx, false)
	if err != nil {
		return Info{}, err
	}
	defer unlock()
	return s.create(ctx, opts)
}

// create writes a backup; the caller holds the backup lock.
func (s *Service) create(ctx context.Context, opts CreateOptions) (Info, error) {
	if opts.Kind == "" {
		opts.Kind = KindManual
	}
	if err := os.MkdirAll(s.Dir, 0o750); err != nil {
		return Info{}, fmt.Errorf("backup folder: %w", err)
	}
	// Leftovers of an interrupted backup (the lock guarantees none is in progress).
	for _, pattern := range []string{".tmp-*", ".streamero*.partial"} {
		old, _ := filepath.Glob(filepath.Join(s.Dir, pattern))
		for _, o := range old {
			_ = os.RemoveAll(o)
		}
	}
	tmp, err := os.MkdirTemp(s.Dir, ".tmp-")
	if err != nil {
		return Info{}, fmt.Errorf("backup folder: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmp) }()

	m, err := s.dump(ctx, tmp)
	if err != nil {
		return Info{}, err
	}
	m.Kind = opts.Kind
	m.IncludesImages = opts.IncludeImages

	files, err := s.collectFiles(opts.IncludeImages)
	if err != nil {
		return Info{}, err
	}
	for _, f := range files {
		switch f.prefix {
		case configPrefix:
			m.Files.Config++
		case avatarPrefix:
			m.Files.Avatars++
		case imagePrefix:
			m.Files.Images++
		}
	}

	prefix := "streamero"
	if opts.Kind == KindPreRestore {
		prefix = "streamero-pre-restore"
	}
	name := s.fileName(prefix, m.CreatedAt, s.Version)
	final := filepath.Join(s.Dir, name)
	if err := s.writeArchive(final, tmp, m, files); err != nil {
		return Info{}, err
	}
	fi, err := os.Stat(final)
	if err != nil {
		return Info{}, err
	}
	if m.ScanRunning {
		s.log().Warn("backup taken while a library scan was running; it is consistent but may miss items being added", "backup", name)
	}
	if len(m.Skipped) > 0 {
		s.log().Warn("backup skipped unreadable tables", "backup", name, "tables", m.SkippedNames())
	}
	s.log().Info("backup written", "backup", name, "bytes", fi.Size(), "tables", len(m.Tables))
	return Info{Name: name, Size: fi.Size(), Kind: kindOf(name, m), Manifest: m, ModTime: fi.ModTime().UTC()}, nil
}

// dump copies every application table into dir inside one read-only repeatable-read
// transaction, so all tables come from the same snapshot.
func (s *Service) dump(ctx context.Context, dir string) (*Manifest, error) {
	tx, err := s.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	m := &Manifest{Format: FormatVersion, App: appName, ServerVersion: s.Version, Commit: s.Commit,
		CreatedAt: s.now(), Tables: []Table{}, Sequences: []Sequence{}}
	if m.SchemaVersion, err = db.SchemaVersion(ctx, tx); err != nil {
		return nil, err
	}
	if m.SchemaVersion == 0 {
		return nil, errors.New("the database has no Streamero schema")
	}
	m.ScanRunning = ScanRunning(ctx, tx)
	tables, err := dataTables(ctx, tx)
	if err != nil {
		return nil, err
	}
	for _, t := range tables {
		cols, err := tableColumns(ctx, tx, t)
		cols = withoutSecrets(t, cols)
		if err != nil {
			return nil, err
		}
		entry, err := s.dumpTable(ctx, tx, dir, t, cols)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			// A damaged table must not cost the whole backup: record it and go on.
			m.Skipped = append(m.Skipped, Skipped{Name: t, Error: err.Error()})
			continue
		}
		m.Tables = append(m.Tables, entry)
	}
	rows, err := tx.Query(ctx, `SELECT sequencename, last_value FROM pg_sequences
		WHERE schemaname = 'public' AND sequencename NOT LIKE 'river\_%' ORDER BY sequencename`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		var last *int64
		if err := rows.Scan(&name, &last); err != nil {
			return nil, err
		}
		if last != nil {
			m.Sequences = append(m.Sequences, Sequence{Name: name, Value: *last, IsCalled: true})
		}
	}
	return m, rows.Err()
}

func (s *Service) dumpTable(ctx context.Context, tx pgx.Tx, dir, table string, cols []string) (Table, error) {
	if _, err := tx.Exec(ctx, `SAVEPOINT dump_table`); err != nil {
		return Table{}, err
	}
	t, err := copyOut(ctx, tx, dir, table, cols)
	if err != nil {
		if _, rerr := tx.Exec(ctx, `ROLLBACK TO SAVEPOINT dump_table`); rerr != nil {
			return Table{}, errors.Join(err, rerr)
		}
		return Table{}, err
	}
	_, err = tx.Exec(ctx, `RELEASE SAVEPOINT dump_table`)
	return t, err
}

func copyOut(ctx context.Context, tx pgx.Tx, dir, table string, cols []string) (Table, error) {
	p := filepath.Join(dir, table+".tsv")
	f, err := os.Create(p) //nolint:gosec // G304: temp folder + a table name from pg_catalog
	if err != nil {
		return Table{}, err
	}
	defer func() { _ = f.Close() }()
	sum := sha256.New()
	cw := &countingWriter{w: io.MultiWriter(f, sum)}
	src := copyTarget(table, cols)
	if where, ok := secretRows[table]; ok {
		quoted := make([]string, len(cols))
		for i, c := range cols {
			quoted[i] = pgx.Identifier{c}.Sanitize()
		}
		src = "(SELECT " + strings.Join(quoted, ", ") + " FROM " + pgx.Identifier{table}.Sanitize() + " WHERE " + where + ")"
	}
	tag, err := tx.Conn().PgConn().CopyTo(ctx, cw, "COPY "+src+" TO STDOUT")
	if err != nil {
		_ = os.Remove(p)
		return Table{}, err
	}
	if err := f.Close(); err != nil {
		return Table{}, err
	}
	return Table{Name: table, Columns: cols, Rows: tag.RowsAffected(), Bytes: cw.n, SHA256: hex.EncodeToString(sum.Sum(nil))}, nil
}

type countingWriter struct {
	w io.Writer
	n int64
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}

// copyTarget renders `"table" ("a", "b")` with quoted identifiers.
func copyTarget(table string, cols []string) string {
	quoted := make([]string, len(cols))
	for i, c := range cols {
		quoted[i] = pgx.Identifier{c}.Sanitize()
	}
	return pgx.Identifier{table}.Sanitize() + " (" + strings.Join(quoted, ", ") + ")"
}

type querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// dataTables lists the application tables: everything in public except the migration
// bookkeeping and River's job queue (jobs are transient and rebuilt by the worker).
func dataTables(ctx context.Context, q querier) ([]string, error) {
	rows, err := q.Query(ctx, `SELECT c.relname FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'public' AND c.relkind IN ('r', 'p') AND NOT c.relispartition
			AND c.relname <> 'goose_db_version' AND c.relname NOT LIKE 'river\_%'
		ORDER BY c.relname`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

// secretColumns are never written into a backup. A restored row gets the column's
// default (NULL), which is what those features expect after the secret is gone.
var secretColumns = map[string][]string{
	// The other server's API key, kept only while an import runs.
	"import_runs": {"secret"},
}

// StreamKeySetting is the settings row holding the stream URL signing key.
const StreamKeySetting = "stream.key"

// secretRows are rows never written into a backup (SQL conditions for the rows
// that are kept). The stream signing key stays with the server: whoever reads a
// backup could otherwise sign media links, and a restore keeps the running
// server's key (see Restore).
var secretRows = map[string]string{
	"settings": "key <> '" + StreamKeySetting + "'",
}

// withoutSecrets drops the secretColumns of a table from its column list.
func withoutSecrets(table string, cols []string) []string {
	skip := secretColumns[table]
	if len(skip) == 0 {
		return cols
	}
	out := cols[:0:0]
	for _, c := range cols {
		if !slices.Contains(skip, c) {
			out = append(out, c)
		}
	}
	return out
}

// tableColumns lists the stored (not generated, not dropped) columns in order.
func tableColumns(ctx context.Context, q querier, table string) ([]string, error) {
	rows, err := q.Query(ctx, `SELECT a.attname FROM pg_attribute a
		WHERE a.attrelid = ('public.' || quote_ident($1))::regclass AND a.attnum > 0 AND NOT a.attisdropped AND a.attgenerated = ''
		ORDER BY a.attnum`, table)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

type archiveFile struct {
	prefix string // configPrefix, avatarPrefix or imagePrefix
	rel    string // slash-separated path below the prefix
	path   string // absolute path on disk
}

// collectFiles lists config files (without the backup folder), avatars and, on request,
// cached artwork. Only regular files are archived; symlinks are skipped.
func (s *Service) collectFiles(images bool) ([]archiveFile, error) {
	var out []archiveFile
	backupDir, _ := filepath.Abs(s.Dir)
	walk := func(root, prefix string) error {
		if root == "" {
			return nil
		}
		root, err := filepath.Abs(root)
		if err != nil {
			return err
		}
		err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				if p == root && errors.Is(err, fs.ErrNotExist) {
					return fs.SkipAll
				}
				// Unreadable entries are skipped rather than failing the backup.
				s.log().Warn("backup: skipping unreadable path", "path", p, "err", err)
				if d != nil && d.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
			if d.IsDir() {
				if p == backupDir {
					return fs.SkipDir
				}
				return nil
			}
			if !d.Type().IsRegular() || d.Name() == "CACHEDIR.TAG" {
				return nil
			}
			rel, err := filepath.Rel(root, p)
			if err != nil {
				return err
			}
			out = append(out, archiveFile{prefix: prefix, rel: filepath.ToSlash(rel), path: p})
			return nil
		})
		if errors.Is(err, fs.SkipAll) {
			return nil
		}
		return err
	}
	if err := walk(s.ConfigDir, configPrefix); err != nil {
		return nil, err
	}
	if s.CacheDir != "" {
		if err := walk(filepath.Join(s.CacheDir, "avatars"), avatarPrefix); err != nil {
			return nil, err
		}
		if images {
			if err := walk(filepath.Join(s.CacheDir, "images"), imagePrefix); err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}

// writeArchive writes final atomically: a hidden partial file renamed when complete.
func (s *Service) writeArchive(final, tmp string, m *Manifest, files []archiveFile) (err error) {
	partial := filepath.Join(filepath.Dir(final), "."+filepath.Base(final)+".partial")
	f, err := os.OpenFile(partial, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) //nolint:gosec // G304: inside the backup folder
	if err != nil {
		return err
	}
	defer func() {
		_ = f.Close()
		if err != nil {
			_ = os.Remove(partial)
		}
	}()
	gz, err := gzip.NewWriterLevel(f, gzip.BestSpeed)
	if err != nil {
		return err
	}
	tw := tar.NewWriter(gz)
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err := tw.WriteHeader(&tar.Header{Name: manifestEntry, Mode: 0o600, Size: int64(len(raw)), ModTime: m.CreatedAt, Typeflag: tar.TypeReg}); err != nil {
		return err
	}
	if _, err := tw.Write(raw); err != nil {
		return err
	}
	for _, t := range m.Tables {
		if err := addFile(tw, t.Entry(), filepath.Join(tmp, t.Name+".tsv"), true); err != nil {
			return err
		}
	}
	for _, af := range files {
		if err := addFile(tw, af.prefix+af.rel, af.path, false); err != nil {
			return err
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	if err := gz.Close(); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(partial, final); err != nil {
		return err
	}
	if d, derr := os.Open(filepath.Dir(final)); derr == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}

// addFile appends one file. A file that vanished meanwhile is skipped unless required.
func addFile(tw *tar.Writer, name, p string, required bool) error {
	f, err := os.Open(p) //nolint:gosec // G304: paths come from walking the config/cache folders or the temp dump
	if err != nil {
		if !required && errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	defer func() { _ = f.Close() }()
	fi, err := f.Stat()
	if err != nil {
		return err
	}
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: fi.Size(), ModTime: fi.ModTime(), Typeflag: tar.TypeReg}); err != nil {
		return err
	}
	if _, err := io.CopyN(tw, f, fi.Size()); err != nil {
		return fmt.Errorf("archive %s: %w", name, err)
	}
	return nil
}
