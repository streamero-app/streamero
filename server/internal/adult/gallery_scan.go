package adult

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/streamero-app/streamero/server/internal/books"
	"github.com/streamero-app/streamero/server/internal/exif"
	"github.com/streamero-app/streamero/server/internal/library"
)

// Picture formats of galleries (D-058). HEIC/HEIF and AVIF are shown converted
// to JPEG (with ffmpeg), like in photo libraries.
var galleryImageExts = map[string]string{".jpg": "jpeg", ".jpeg": "jpeg", ".png": "png", ".webp": "webp", ".gif": "gif",
	".avif": "avif", ".heic": "heic", ".heif": "heic"}

// IsGalleryImage reports a picture file a gallery shows.
func IsGalleryImage(name string) bool { return galleryImageExts[strings.ToLower(filepath.Ext(name))] != "" }

// IsGalleryArchive reports a zip or cbz archive of pictures. Archives are read
// in memory through the size and name guards of the comic reader (D-039) and
// never extracted to disk.
func IsGalleryArchive(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".zip", ".cbz":
		return true
	}
	return false
}

type foundImage struct {
	path, entry string
	size        int64
	mtime       time.Time
}

// foundGallery is a folder of pictures, or an archive (images then holds the
// archive file itself until its entries are listed).
type foundGallery struct {
	path, parent string
	archive      bool
	images       []foundImage
}

type knownImage struct {
	id, gallery uuid.UUID
	size        int64
	mtime       time.Time
}

type imageKey struct{ path, entry string }

// scanGalleries stores the galleries found by walk: new galleries are parsed
// from their names, new and changed pictures are read (size and orientation
// only), pictures and galleries that are gone are removed with the same
// offline-folder safety as video files.
func (s *Service) scanGalleries(ctx context.Context, libID uuid.UUID, found []foundGallery, opts library.ScanOptions, res *library.ScanResult) error {
	knownGal := map[string]uuid.UUID{}
	rows, err := s.Pool.Query(ctx, `SELECT id, path FROM adult_galleries WHERE library_id = $1`, libID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id uuid.UUID
		var p string
		if err := rows.Scan(&id, &p); err != nil {
			rows.Close()
			return err
		}
		knownGal[p] = id
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	knownImg := map[imageKey]knownImage{}
	rows, err = s.Pool.Query(ctx, `SELECT id, gallery_id, path, entry, size, mtime FROM adult_images WHERE library_id = $1`, libID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var k knownImage
		var key imageKey
		if err := rows.Scan(&k.id, &k.gallery, &key.path, &key.entry, &k.size, &k.mtime); err != nil {
			rows.Close()
			return err
		}
		k.mtime = k.mtime.UTC()
		knownImg[key] = k
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	// Archive files by path, to tell whether an archive changed without opening it.
	archiveSeen := map[string]knownImage{}
	for key, k := range knownImg {
		if key.entry != "" {
			archiveSeen[key.path] = k
		}
	}

	seenGal := map[string]bool{}
	seenImg := map[imageKey]bool{}
	for _, g := range found {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if g.archive {
			arch := g.images[0]
			if k, ok := archiveSeen[arch.path]; ok && k.size == arch.size && k.mtime.Equal(arch.mtime) {
				// Unchanged archive: keep its entries as they are.
				seenGal[g.path] = true
				for key := range knownImg {
					if key.path == arch.path {
						seenImg[key] = true
					}
				}
				continue
			}
			entries, err := archiveImages(arch)
			if err != nil {
				res.Unreadable = append(res.Unreadable, arch.path)
				// An unreadable archive keeps what it had.
				seenGal[g.path] = true
				for key := range knownImg {
					if key.path == arch.path {
						seenImg[key] = true
					}
				}
				continue
			}
			g.images = entries
		}
		if len(g.images) == 0 {
			continue
		}
		sort.SliceStable(g.images, func(i, j int) bool {
			return books.NaturalLess(filepath.Base(g.images[i].path)+g.images[i].entry, filepath.Base(g.images[j].path)+g.images[j].entry)
		})
		seenGal[g.path] = true
		res.Files += len(g.images)
		galID, err := s.upsertGallery(ctx, libID, g, knownGal)
		if err != nil {
			return err
		}
		var arch *books.Archive
		for i, im := range g.images {
			key := imageKey{im.path, im.entry}
			seenImg[key] = true
			k, ok := knownImg[key]
			if ok && k.gallery == galID && k.size == im.size && k.mtime.Equal(im.mtime) {
				if _, err := s.Pool.Exec(ctx, `UPDATE adult_images SET position = $2 WHERE id = $1 AND position <> $2`, k.id, i); err != nil {
					return err
				}
				continue
			}
			res.Probed++
			info := s.readImageInfo(im, g.archive, &arch)
			id := uuid.Must(uuid.NewV7())
			if ok {
				id = k.id
			}
			format := galleryImageExts[strings.ToLower(filepath.Ext(im.path))]
			if im.entry != "" {
				format = galleryImageExts[strings.ToLower(filepath.Ext(im.entry))]
			}
			if _, err := s.Pool.Exec(ctx, `INSERT INTO adult_images (id, gallery_id, library_id, path, entry, position, size, mtime,
					width, height, orientation, format)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
				ON CONFLICT (library_id, path, entry) DO UPDATE SET gallery_id = EXCLUDED.gallery_id, position = EXCLUDED.position,
					size = EXCLUDED.size, mtime = EXCLUDED.mtime, width = EXCLUDED.width, height = EXCLUDED.height,
					orientation = EXCLUDED.orientation, format = EXCLUDED.format`,
				id, galID, libID, im.path, im.entry, i, im.size, im.mtime, nilZero(info.Width), nilZero(info.Height), info.Orientation, format); err != nil {
				return err
			}
			// A changed picture gets a new thumbnail.
			if ok {
				s.removeImageCache(id)
			}
		}
		if arch != nil {
			_ = (*arch).Close()
		}
	}

	// Pictures that are gone.
	var gone []knownImage
	for key, k := range knownImg {
		if seenImg[key] || underAny(key.path, res.Unreadable) {
			continue
		}
		gone = append(gone, k)
	}
	foundAny := len(seenImg) > 0
	if len(gone) > 0 && !opts.ConfirmRemoval && (!foundAny || (len(gone) >= minHeldRemoval && len(gone)*2 > len(knownImg))) {
		res.Held += len(gone)
		gone = nil
		for p := range knownGal {
			seenGal[p] = true
		}
	}
	for _, k := range gone {
		if _, err := s.Pool.Exec(ctx, `DELETE FROM adult_images WHERE id = $1`, k.id); err != nil {
			return err
		}
		s.removeImageCache(k.id)
		res.Removed++
	}
	for p, id := range knownGal {
		if !seenGal[p] && !underAny(p, res.Unreadable) {
			if _, err := s.Pool.Exec(ctx, `DELETE FROM adult_galleries WHERE id = $1`, id); err != nil {
				return err
			}
		}
	}
	// Counts and covers; galleries left without pictures go.
	if _, err := s.Pool.Exec(ctx, `UPDATE adult_galleries g SET image_count = c.n, cover_image_id = c.cover
		FROM (SELECT g2.id, count(i.id)::int AS n,
			(SELECT i2.id FROM adult_images i2 WHERE i2.gallery_id = g2.id ORDER BY i2.position, i2.id LIMIT 1) AS cover
			FROM adult_galleries g2 LEFT JOIN adult_images i ON i.gallery_id = g2.id WHERE g2.library_id = $1 GROUP BY g2.id) c
		WHERE g.id = c.id AND (g.image_count <> c.n OR g.cover_image_id IS DISTINCT FROM c.cover)`, libID); err != nil {
		return err
	}
	_, err = s.Pool.Exec(ctx, `DELETE FROM adult_galleries WHERE library_id = $1 AND image_count = 0`, libID)
	return err
}

// archiveImages lists the picture entries of an archive.
func archiveImages(arch foundImage) ([]foundImage, error) {
	a, err := books.OpenArchive(arch.path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = a.Close() }()
	var out []foundImage
	for _, name := range books.Pages(a.Entries()) {
		if !IsGalleryImage(name) {
			continue
		}
		out = append(out, foundImage{path: arch.path, entry: name, size: arch.size, mtime: arch.mtime})
	}
	return out, nil
}

// readImageInfo reads the size and orientation of a picture (headers only for
// files; an archive entry is read through the archive's guards).
func (s *Service) readImageInfo(im foundImage, archive bool, arch **books.Archive) exif.Info {
	if !archive {
		info, _ := exif.ReadFile(im.path)
		return info
	}
	if *arch == nil {
		a, err := books.OpenArchive(im.path)
		if err != nil {
			return exif.Info{}
		}
		*arch = &a
	}
	data, err := (**arch).ReadFile(im.entry, books.MaxEntryBytes)
	if err != nil {
		return exif.Info{}
	}
	info, _ := exif.Read(bytes.NewReader(data), int64(len(data)))
	return info
}

// upsertGallery stores a gallery; its name is parsed once (a rescan keeps what
// it has, so matched data is not overwritten).
func (s *Service) upsertGallery(ctx context.Context, libID uuid.UUID, g foundGallery, known map[string]uuid.UUID) (uuid.UUID, error) {
	if id, ok := known[g.path]; ok {
		return id, nil
	}
	var parsed Parsed
	if g.archive {
		parsed = ParseFile(g.parent, filepath.Base(g.path))
	} else {
		parsed = ParseFolder(g.parent, filepath.Base(g.path))
	}
	raw, _ := json.Marshal(parsed)
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	studio, err := upsertStudio(ctx, tx, parsed.Studio)
	if err != nil {
		return uuid.Nil, err
	}
	var date *string
	if parsed.Date != "" {
		date = &parsed.Date
	}
	var year *int
	if parsed.Year > 0 {
		year = &parsed.Year
	}
	id := uuid.Must(uuid.NewV7())
	if err := tx.QueryRow(ctx, `INSERT INTO adult_galleries (id, library_id, path, archive, title, sort_title, release_date, year, studio_id, parsed)
		VALUES ($1, $2, $3, $4, $5, $6, $7::date, $8, $9, $10)
		ON CONFLICT (library_id, path) DO UPDATE SET updated_at = now() RETURNING id`,
		id, libID, g.path, g.archive, parsed.Title, sortTitle(parsed.Title), date, year, studio, raw).Scan(&id); err != nil {
		return uuid.Nil, err
	}
	for i, n := range parsed.Performers {
		pid, ok, err := upsertPerformer(ctx, tx, n)
		if err != nil {
			return uuid.Nil, err
		}
		if !ok {
			continue
		}
		if _, err := tx.Exec(ctx, `INSERT INTO adult_gallery_performers (gallery_id, performer_id, position) VALUES ($1, $2, $3)
			ON CONFLICT DO NOTHING`, id, pid, i); err != nil {
			return uuid.Nil, err
		}
	}
	known[g.path] = id
	return id, tx.Commit(ctx)
}

// LinkGalleries joins galleries to the scene they belong to: the same studio and
// release date (and, when that day has several scenes, the same title), or the
// same studio and title without a date. A linked gallery takes the scene's tags,
// and its performers when its name named none.
func (s *Service) LinkGalleries(ctx context.Context, libID uuid.UUID) error {
	rows, err := s.Pool.Query(ctx, `SELECT g.id, g.title, g.studio_id, g.release_date FROM adult_galleries g
		WHERE g.library_id = $1 AND g.studio_id IS NOT NULL`, libID)
	if err != nil {
		return err
	}
	type gal struct {
		id, studio uuid.UUID
		title      string
		date       *time.Time
	}
	var list []gal
	for rows.Next() {
		var g gal
		if err := rows.Scan(&g.id, &g.title, &g.studio, &g.date); err != nil {
			rows.Close()
			return err
		}
		list = append(list, g)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, g := range list {
		scene, err := s.matchScene(ctx, libID, g.studio, g.title, g.date)
		if err != nil {
			return err
		}
		if scene == uuid.Nil {
			if _, err := s.Pool.Exec(ctx, `UPDATE adult_galleries SET scene_id = NULL WHERE id = $1 AND scene_id IS NOT NULL`, g.id); err != nil {
				return err
			}
			continue
		}
		if _, err := s.Pool.Exec(ctx, `UPDATE adult_galleries g SET scene_id = $2, tags = a.tags, updated_at = now()
			FROM adult_scenes a WHERE g.id = $1 AND a.id = $2 AND (g.scene_id IS DISTINCT FROM $2 OR g.tags <> a.tags)`, g.id, scene); err != nil {
			return err
		}
		if _, err := s.Pool.Exec(ctx, `INSERT INTO adult_gallery_performers (gallery_id, performer_id, position)
			SELECT $1, sp.performer_id, sp.position FROM adult_scene_performers sp WHERE sp.scene_id = $2
			AND NOT EXISTS (SELECT 1 FROM adult_gallery_performers gp WHERE gp.gallery_id = $1)
			ON CONFLICT DO NOTHING`, g.id, scene); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) matchScene(ctx context.Context, libID, studio uuid.UUID, title string, date *time.Time) (uuid.UUID, error) {
	type cand struct {
		id    uuid.UUID
		title string
	}
	var q string
	args := []any{libID, studio}
	if date != nil {
		q = `SELECT id, title FROM adult_scenes WHERE library_id = $1 AND studio_id = $2 AND release_date = $3::date`
		args = append(args, date.Format("2006-01-02"))
	} else {
		q = `SELECT id, title FROM adult_scenes WHERE library_id = $1 AND studio_id = $2`
	}
	rows, err := s.Pool.Query(ctx, q+` ORDER BY id LIMIT 200`, args...)
	if err != nil {
		return uuid.Nil, err
	}
	var cands []cand
	for rows.Next() {
		var c cand
		if err := rows.Scan(&c.id, &c.title); err != nil {
			rows.Close()
			return uuid.Nil, err
		}
		cands = append(cands, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return uuid.Nil, err
	}
	key := foldKey(title)
	var same []uuid.UUID
	for _, c := range cands {
		if key != "" && foldKey(c.title) == key {
			same = append(same, c.id)
		}
	}
	switch {
	case len(same) == 1:
		return same[0], nil
	case date != nil && len(cands) == 1:
		return cands[0].id, nil
	}
	return uuid.Nil, nil
}

// removeImageCache deletes the cached thumbnail and converted copy of a picture.
func (s *Service) removeImageCache(id uuid.UUID) {
	if s.ImageDir == "" {
		return
	}
	for _, p := range []string{s.thumbPath(id), s.fullPath(id)} {
		if p != "" {
			_ = os.Remove(p)
		}
	}
}


