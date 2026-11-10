package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/streamero-app/streamero/server/internal/api/gen"
	"github.com/streamero-app/streamero/server/internal/importer"
	"github.com/streamero-app/streamero/server/internal/library"
)

var errImportRunning = &StatusError{Status: http.StatusConflict, Code: "import_running", Message: "another import is still running"}

func importSourceError(err error) error {
	var up *importer.ErrUpstream
	if errors.As(err, &up) && (up.Status == http.StatusUnauthorized || up.Status == http.StatusForbidden) {
		return &StatusError{Status: http.StatusBadGateway, Code: "import_key_rejected", Message: err.Error()}
	}
	if errors.As(err, &up) || errors.Is(err, importer.ErrNotFound) {
		return &StatusError{Status: http.StatusBadGateway, Code: "import_source_failed", Message: err.Error()}
	}
	return err
}

func importClient(rawURL, key string) (*importer.Client, error) {
	u, err := importer.CleanURL(rawURL)
	if err != nil {
		return nil, &StatusError{Status: http.StatusBadRequest, Code: "invalid_url", Message: err.Error()}
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, &StatusError{Status: http.StatusBadRequest, Code: "invalid_key", Message: "an API key is required"}
	}
	return &importer.Client{BaseURL: u, Key: key}, nil
}

// CheckImportSource implements POST /import/source.
func (h *Handler) CheckImportSource(ctx context.Context, req *gen.ImportSource) (*gen.ImportSourceInfo, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	if h.lib() == nil {
		return nil, errStarting
	}
	c, err := importClient(req.URL, req.ApiKey)
	if err != nil {
		return nil, err
	}
	info, err := c.Info(ctx)
	if err != nil {
		return nil, importSourceError(err)
	}
	users, err := c.Users(ctx)
	if err != nil {
		return nil, importSourceError(err)
	}
	var local []importer.LocalUser
	rows, err := h.lib().Pool.Query(ctx, `SELECT id, username FROM users`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var u importer.LocalUser
		if err := rows.Scan(&u.ID, &u.Username); err != nil {
			rows.Close()
			return nil, err
		}
		local = append(local, u)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	suggested := importer.SuggestUsers(users, local)
	out := &gen.ImportSourceInfo{ServerName: info.ServerName, Version: info.Version, Users: []gen.ImportSourceUser{}}
	for _, u := range users {
		su := gen.ImportSourceUser{ID: u.ID, Name: u.Name, IsAdmin: u.Policy.IsAdministrator}
		if id, ok := suggested[u.ID]; ok {
			su.SuggestedUserId = gen.NewOptUUID(id)
		}
		out.Users = append(out.Users, su)
	}
	return out, nil
}

// StartImport implements POST /import/runs.
func (h *Handler) StartImport(ctx context.Context, req *gen.ImportRequest) (*gen.ImportRun, error) {
	p, err := admin(ctx)
	if err != nil {
		return nil, err
	}
	if h.lib() == nil || h.EnqueueImport == nil {
		return nil, errStarting
	}
	c, err := importClient(req.URL, req.ApiKey)
	if err != nil {
		return nil, err
	}
	opts := importer.Options{Playlists: req.Playlists.Or(true)}
	for _, u := range req.Users {
		m := importer.UserMapping{SourceUserID: u.SourceUserId, SourceUserName: u.SourceUserName}
		if id, ok := u.UserId.Get(); ok {
			m.UserID = &id
		}
		opts.Users = append(opts.Users, m)
	}
	for _, m := range req.PathMappings {
		from := strings.TrimSpace(m.From)
		// The Streamero side names a folder on this server, so it must be inside the media folders.
		to, err := library.ResolveSafePath(h.lib().Roots, strings.TrimSpace(m.To))
		if from == "" || err != nil {
			return nil, &StatusError{Status: http.StatusBadRequest, Code: "invalid_path",
				Message: "path mapping " + m.To + " is not a folder inside the media folders"}
		}
		opts.PathMappings = append(opts.PathMappings, importer.PathMapping{From: from, To: to})
	}
	rawOpts, err := json.Marshal(opts)
	if err != nil {
		return nil, err
	}
	id, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}
	pool := h.lib().Pool
	if err := importer.SweepStale(ctx, pool); err != nil {
		return nil, err
	}
	var busy bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM import_runs WHERE status IN ('queued', 'running'))`).Scan(&busy); err != nil {
		return nil, err
	}
	if busy {
		return nil, errImportRunning
	}
	if _, err := pool.Exec(ctx, `INSERT INTO import_runs (id, created_by, dry_run, source_url, secret, options)
		VALUES ($1, $2, $3, $4, $5, $6)`, id, p.User.ID, req.DryRun, c.BaseURL, c.Key, rawOpts); err != nil {
		if pgErr := (*pgconn.PgError)(nil); errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return nil, errImportRunning
		}
		return nil, err
	}
	if err := h.EnqueueImport(ctx, id); err != nil {
		_, _ = pool.Exec(context.WithoutCancel(ctx), `UPDATE import_runs SET status = 'failed', error = $2, secret = NULL,
			finished_at = now() WHERE id = $1`, id, err.Error())
		return nil, err
	}
	if h.Auth() != nil {
		_ = h.Auth().Audit(ctx, &p.User.ID, "import.started", map[string]any{"source": c.BaseURL, "dryRun": req.DryRun})
	}
	return h.importRun(ctx, id)
}

const importRunCols = `id, status, dry_run, source_url, created_at, started_at, finished_at, COALESCE(error, ''), progress, report`

func scanImportRun(row pgx.Row) (*gen.ImportRun, error) {
	var r gen.ImportRun
	var status, errMsg string
	var started, finished *time.Time
	var rawProgress, rawReport []byte
	if err := row.Scan(&r.ID, &status, &r.DryRun, &r.SourceUrl, &r.CreatedAt, &started, &finished, &errMsg, &rawProgress, &rawReport); err != nil {
		return nil, err
	}
	r.Status = gen.ImportRunStatus(status)
	if started != nil {
		r.StartedAt = gen.NewOptDateTime(*started)
	}
	if finished != nil {
		r.FinishedAt = gen.NewOptDateTime(*finished)
	}
	r.Error = optString(errMsg)
	var pr importer.Progress
	_ = json.Unmarshal(rawProgress, &pr)
	r.Progress = gen.ImportProgress{UsersDone: pr.UsersDone, UsersTotal: pr.UsersTotal, CurrentUser: optString(pr.CurrentUser)}
	if rawReport != nil {
		var rep importer.Report
		if err := json.Unmarshal(rawReport, &rep); err == nil {
			r.Report = gen.NewOptImportReport(toImportReport(rep))
		}
	}
	return &r, nil
}

func toImportReport(rep importer.Report) gen.ImportReport {
	out := gen.ImportReport{Users: []gen.ImportUserReport{}, SkippedUsers: append([]string{}, rep.SkippedUsers...),
		MatchedByProvider: rep.MatchedByProvider, MatchedByPath: rep.MatchedByPath, MatchedByTitle: rep.MatchedByTitle,
		Unmatched: rep.Unmatched, UnmatchedSample: []gen.ImportUnmatched{}, PlaylistsCreated: rep.PlaylistsCreated,
		PlaylistsUpdated: rep.PlaylistsUpdated, PlaylistItemsMissing: rep.PlaylistItemsMissing, KeptNewer: rep.KeptNewer}
	for _, u := range rep.Users {
		out.Users = append(out.Users, gen.ImportUserReport{SourceUserName: u.SourceUserName, UserId: u.UserID, Username: u.Username,
			Items: u.Items, Matched: u.Matched, Unmatched: u.Unmatched, Played: u.Played, Resume: u.Resume, Favorites: u.Favorites,
			Playlists: u.Playlists})
	}
	for _, u := range rep.UnmatchedSample {
		out.UnmatchedSample = append(out.UnmatchedSample, gen.ImportUnmatched{User: u.User, Name: u.Name, Kind: u.Kind, Path: optString(u.Path)})
	}
	return out
}

func (h *Handler) importRun(ctx context.Context, id uuid.UUID) (*gen.ImportRun, error) {
	r, err := scanImportRun(h.lib().Pool.QueryRow(ctx, `SELECT `+importRunCols+` FROM import_runs WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, &StatusError{Status: http.StatusNotFound, Code: "not_found", Message: "import not found"}
	}
	return r, err
}

// ListImportRuns implements GET /import/runs.
func (h *Handler) ListImportRuns(ctx context.Context) ([]gen.ImportRun, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	if h.lib() == nil {
		return nil, errStarting
	}
	if err := importer.SweepStale(ctx, h.lib().Pool); err != nil {
		return nil, err
	}
	rows, err := h.lib().Pool.Query(ctx, `SELECT `+importRunCols+` FROM import_runs ORDER BY created_at DESC LIMIT 10`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []gen.ImportRun{}
	for rows.Next() {
		r, err := scanImportRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}

// GetImportRun implements GET /import/runs/{runId}.
func (h *Handler) GetImportRun(ctx context.Context, params gen.GetImportRunParams) (*gen.ImportRun, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	if h.lib() == nil {
		return nil, errStarting
	}
	return h.importRun(ctx, params.RunId)
}
