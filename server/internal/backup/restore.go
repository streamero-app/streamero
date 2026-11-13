package backup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/streamero-app/streamero/server/internal/db"
	"github.com/streamero-app/streamero/server/internal/library"
)

// RestoreOptions control a restore.
type RestoreOptions struct {
	// SafetyBackup writes a pre-restore backup of the current state first.
	SafetyBackup bool
	// SkipFiles restores only the database (no config files, avatars or artwork).
	SkipFiles bool
}

// RestoreResult describes a finished (or failed) restore.
type RestoreResult struct {
	Manifest     *Manifest
	SafetyBackup string
	Warnings     []string
	// Changed is true once the database was modified; a failure after that point leaves
	// it in an unknown state (the safety backup brings it back).
	Changed bool
}

// Prepare verifies an archive and checks that this server can restore it.
func Prepare(p string) (*Manifest, error) {
	m, err := Verify(p)
	if err != nil {
		return nil, err
	}
	latest, err := db.LatestVersion()
	if err != nil {
		return nil, err
	}
	if m.SchemaVersion > latest {
		return nil, fmt.Errorf("%w (backup schema %d, this server %d)", ErrNewerSchema, m.SchemaVersion, latest)
	}
	return m, nil
}

// Restore replaces the database (and, unless SkipFiles, config files, avatars and any
// archived artwork) with the backup at p, then migrates the schema forward.
//
// When the backup's schema version equals the database's, the data swap happens in one
// transaction: on failure nothing changed. Otherwise the schema is recreated at the
// backup's version first, which cannot be rolled back; the safety backup covers that.
func (s *Service) Restore(ctx context.Context, p string, opts RestoreOptions) (*RestoreResult, error) {
	res := &RestoreResult{}
	m, err := Prepare(p)
	if err != nil {
		return res, err
	}
	res.Manifest = m
	unlock, err := s.lock(ctx, true)
	if err != nil {
		return res, err
	}
	defer unlock()

	current, err := db.SchemaVersion(ctx, s.Pool)
	if err != nil {
		return res, err
	}
	if opts.SafetyBackup && current > 0 {
		info, err := s.create(ctx, CreateOptions{Kind: KindPreRestore})
		if err != nil {
			return res, fmt.Errorf("safety backup: %w", err)
		}
		res.SafetyBackup = info.Name
	}
	if len(m.Skipped) > 0 {
		res.Warnings = append(res.Warnings, "tables missing from the backup (left empty): "+strings.Join(m.SkippedNames(), ", "))
	}

	if current != m.SchemaVersion {
		s.log().Info("restore: recreating the database schema", "from", current, "to", m.SchemaVersion)
		res.Changed = true
		if err := resetSchema(ctx, s); err != nil {
			return res, fmt.Errorf("reset schema: %w", err)
		}
		if err := db.MigrateTo(ctx, s.Pool, m.SchemaVersion); err != nil {
			return res, err
		}
	}
	warnings, err := s.load(ctx, p, m, opts, res)
	res.Warnings = append(res.Warnings, warnings...)
	if err != nil {
		return res, err
	}
	// Forward to this binary's schema (no-op when the backup is current).
	if err := db.Migrate(ctx, s.Pool); err != nil {
		return res, fmt.Errorf("migrate after restore: %w", err)
	}
	// Scans that were running when the backup was taken are not running now.
	if _, err := s.Pool.Exec(ctx, `UPDATE libraries SET scan_state = 'idle' WHERE scan_state IN ('queued', 'scanning')`); err != nil {
		res.Warnings = append(res.Warnings, "could not reset library scan states: "+err.Error())
	}
	s.log().Info("restore finished", "backup", filepath.Base(p), "schema", m.SchemaVersion, "warnings", len(res.Warnings))
	return res, nil
}

// resetSchema drops every object in the public schema (tables, River's queue,
// extensions) so migrations can rebuild it at the backup's version.
func resetSchema(ctx context.Context, s *Service) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	if _, err := tx.Exec(ctx, `DROP SCHEMA public CASCADE`); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `CREATE SCHEMA public`); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

type foreignKey struct{ table, name, def string }

// load streams the archive a second time: tables go into one transaction (foreign keys
// dropped during the load and re-created after it), then files are written.
func (s *Service) load(ctx context.Context, p string, m *Manifest, opts RestoreOptions, res *RestoreResult) ([]string, error) {
	var warnings []string
	r, m2, err := open(p)
	if err != nil {
		return nil, err
	}
	defer r.close()
	if m2.SchemaVersion != m.SchemaVersion || len(m2.Tables) != len(m.Tables) {
		return nil, fmt.Errorf("%w: the file changed during the restore", ErrInvalid)
	}

	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	current, err := dataTables(ctx, tx)
	if err != nil {
		return nil, err
	}
	exists := map[string]bool{}
	for _, t := range current {
		exists[t] = true
	}
	byEntry := map[string]Table{}
	for _, t := range m.Tables {
		if !exists[t.Name] {
			return nil, fmt.Errorf("%w: table %s is not part of schema %d", ErrInvalid, t.Name, m.SchemaVersion)
		}
		byEntry[t.Entry()] = t
	}
	// The running server keeps its stream signing key (older backups may carry
	// another server's key; newer ones carry none).
	var streamKey []byte
	if err := tx.QueryRow(ctx, `SELECT value FROM settings WHERE key = $1`, StreamKeySetting).Scan(&streamKey); err != nil &&
		!errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	fks, err := dropForeignKeys(ctx, tx)
	if err != nil {
		return nil, err
	}
	quoted := make([]string, len(current))
	for i, t := range current {
		quoted[i] = pgx.Identifier{t}.Sanitize()
		if _, err := tx.Exec(ctx, `ALTER TABLE `+quoted[i]+` DISABLE TRIGGER USER`); err != nil {
			return nil, err
		}
	}
	if len(quoted) > 0 {
		if _, err := tx.Exec(ctx, `TRUNCATE `+strings.Join(quoted, ", ")); err != nil {
			return nil, err
		}
	}

	committed := false
	finishDB := func() error {
		var err error
		if streamKey != nil {
			_, err = tx.Exec(ctx, `INSERT INTO settings (key, value) VALUES ($1, $2)
				ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()`, StreamKeySetting, streamKey)
		} else {
			// No key here yet: the server creates a fresh one, never the backup's.
			_, err = tx.Exec(ctx, `DELETE FROM settings WHERE key = $1`, StreamKeySetting)
		}
		if err != nil {
			return err
		}
		for _, q := range quoted {
			if _, err := tx.Exec(ctx, `ALTER TABLE `+q+` ENABLE TRIGGER USER`); err != nil {
				return err
			}
		}
		w, err := addForeignKeys(ctx, tx, fks)
		warnings = append(warnings, w...)
		if err != nil {
			return err
		}
		for _, sq := range m.Sequences {
			if _, err := tx.Exec(ctx, `SELECT setval(('public.' || quote_ident($1))::regclass, $2, $3)`, sq.Name, sq.Value, sq.IsCalled); err != nil {
				warnings = append(warnings, "sequence "+sq.Name+": "+err.Error())
				return err
			}
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		committed = true
		res.Changed = true
		return nil
	}

	for {
		h, err := r.tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return warnings, fmt.Errorf("%w: %w", ErrInvalid, err)
		}
		prefix, rel, ok := entryKind(h.Name)
		if !ok {
			return warnings, fmt.Errorf("%w: unexpected entry %q", ErrInvalid, h.Name)
		}
		if prefix == dbPrefix {
			if committed {
				return warnings, fmt.Errorf("%w: table data after files", ErrInvalid)
			}
			t, ok := byEntry[h.Name]
			if !ok {
				return warnings, fmt.Errorf("%w: unexpected entry %q", ErrInvalid, h.Name)
			}
			if err := copyIn(ctx, tx, t, r.tr); err != nil {
				return warnings, fmt.Errorf("restore table %s: %w", t.Name, err)
			}
			continue
		}
		if !committed {
			if err := finishDB(); err != nil {
				return warnings, err
			}
		}
		if opts.SkipFiles {
			continue
		}
		if err := s.extract(prefix, rel, r.tr); err != nil {
			warnings = append(warnings, fmt.Sprintf("file %s: %v", h.Name, err))
		}
	}
	if !committed {
		if err := finishDB(); err != nil {
			return warnings, err
		}
	}
	return warnings, r.finish()
}

func copyIn(ctx context.Context, tx pgx.Tx, t Table, src io.Reader) error {
	sum := sha256.New()
	cr := &countingReader{r: io.TeeReader(src, sum)}
	tag, err := tx.Conn().PgConn().CopyFrom(ctx, cr, "COPY "+copyTarget(t.Name, t.Columns)+" FROM STDIN")
	if err != nil {
		return err
	}
	if cr.n != t.Bytes || hex.EncodeToString(sum.Sum(nil)) != t.SHA256 {
		return fmt.Errorf("%w: table %s is damaged", ErrInvalid, t.Name)
	}
	if tag.RowsAffected() != t.Rows {
		return fmt.Errorf("table %s: %d rows loaded, %d expected", t.Name, tag.RowsAffected(), t.Rows)
	}
	return nil
}

type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

func dropForeignKeys(ctx context.Context, tx pgx.Tx) ([]foreignKey, error) {
	rows, err := tx.Query(ctx, `SELECT r.relname, c.conname, pg_get_constraintdef(c.oid)
		FROM pg_constraint c JOIN pg_class r ON r.oid = c.conrelid JOIN pg_namespace n ON n.oid = r.relnamespace
		WHERE c.contype = 'f' AND n.nspname = 'public' AND r.relname NOT LIKE 'river\_%'
		ORDER BY r.relname, c.conname`)
	if err != nil {
		return nil, err
	}
	fks, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (foreignKey, error) {
		var fk foreignKey
		err := row.Scan(&fk.table, &fk.name, &fk.def)
		return fk, err
	})
	if err != nil {
		return nil, err
	}
	for _, fk := range fks {
		if _, err := tx.Exec(ctx, `ALTER TABLE `+pgx.Identifier{fk.table}.Sanitize()+` DROP CONSTRAINT `+pgx.Identifier{fk.name}.Sanitize()); err != nil {
			return nil, err
		}
	}
	return fks, nil
}

// addForeignKeys re-creates the constraints. Rows that no longer match (a damaged table
// was skipped at backup time) keep the constraint NOT VALID instead of failing the restore.
func addForeignKeys(ctx context.Context, tx pgx.Tx, fks []foreignKey) ([]string, error) {
	var warnings []string
	for _, fk := range fks {
		stmt := `ALTER TABLE ` + pgx.Identifier{fk.table}.Sanitize() + ` ADD CONSTRAINT ` + pgx.Identifier{fk.name}.Sanitize() + ` ` + fk.def
		if _, err := tx.Exec(ctx, `SAVEPOINT add_fk`); err != nil {
			return warnings, err
		}
		if _, err := tx.Exec(ctx, stmt); err != nil {
			if _, rerr := tx.Exec(ctx, `ROLLBACK TO SAVEPOINT add_fk`); rerr != nil {
				return warnings, rerr
			}
			if _, err2 := tx.Exec(ctx, stmt+` NOT VALID`); err2 != nil {
				return warnings, errors.Join(err, err2)
			}
			warnings = append(warnings, fmt.Sprintf("%s.%s: rows without a match were kept (%v)", fk.table, fk.name, err))
		}
		if _, err := tx.Exec(ctx, `RELEASE SAVEPOINT add_fk`); err != nil {
			return warnings, err
		}
	}
	return warnings, nil
}

// extract writes one archived file below its target folder. Entry names were checked by
// entryKind; the target folder is checked again with library.ResolveSafePath.
func (s *Service) extract(prefix, rel string, src io.Reader) error {
	var root string
	switch prefix {
	case configPrefix:
		root = s.ConfigDir
	case avatarPrefix:
		root = filepath.Join(s.CacheDir, "avatars")
	case imagePrefix:
		root = filepath.Join(s.CacheDir, "images")
	}
	if root == "" || (prefix != configPrefix && s.CacheDir == "") {
		return nil
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	if !filepath.IsLocal(filepath.FromSlash(rel)) {
		return errors.New("unsafe path")
	}
	target := filepath.Join(root, filepath.FromSlash(rel))
	if backupDir, _ := filepath.Abs(s.Dir); target == backupDir || strings.HasPrefix(target, backupDir+string(filepath.Separator)) {
		return nil // never overwrite backups with older backups
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
		return err
	}
	parent, err := library.ResolveSafePath(library.Roots{root}, filepath.Dir(target))
	if err != nil {
		return err
	}
	final := filepath.Join(parent, filepath.Base(target))
	if fi, err := os.Lstat(final); err == nil && !fi.Mode().IsRegular() {
		return errors.New("not a regular file")
	}
	tmp, err := os.CreateTemp(parent, ".restore-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	// The archive was written by a backup; its size is bounded by the verified file.
	if _, err := io.Copy(tmp, src); err != nil { //nolint:gosec // G110: entries come from a verified backup archive
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), final)
}
