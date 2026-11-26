package adult

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	_ "image/gif" // decoder for gallery pictures
	"image/jpeg"
	_ "image/png" // decoder for gallery pictures
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/streamero-app/streamero/server/internal/books"
	"github.com/streamero-app/streamero/server/internal/library"
)

// ThumbSize is the longest side of a gallery thumbnail.
const ThumbSize = 480

// thumbSlots limits concurrent thumbnail work (a grid asks for many at once).
var thumbSlots = make(chan struct{}, 4)

var thumbLocks sync.Map // image id → *sync.Mutex

// thumbPath and fullPath are cached files under ImageDir (never under the item
// image folder or any public cache), built from a UUID only.
func (s *Service) thumbPath(id uuid.UUID) string {
	if s.ImageDir == "" {
		return ""
	}
	return filepath.Join(s.ImageDir, "gallery", id.String()[:2], id.String()+".thumb.jpg")
}

func (s *Service) fullPath(id uuid.UUID) string {
	if s.ImageDir == "" {
		return ""
	}
	return filepath.Join(s.ImageDir, "gallery", id.String()[:2], id.String()+".full.jpg")
}

type imageRow struct {
	path, entry, format string
	orientation         int
}

func (s *Service) imageRow(ctx context.Context, id uuid.UUID) (imageRow, error) {
	var r imageRow
	err := s.Pool.QueryRow(ctx, `SELECT i.path, i.entry, i.format, i.orientation FROM adult_images i
		JOIN libraries l ON l.id = i.library_id AND l.kind = 'adult' WHERE i.id = $1`, id).Scan(&r.path, &r.entry, &r.format, &r.orientation)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, ErrNotFound
	}
	return r, err
}

// readSource returns the bytes of a picture (a file under the media folders or
// an archive entry read in memory).
func (s *Service) readSource(r imageRow) ([]byte, error) {
	p, err := library.ResolveSafePath(s.Roots, r.path)
	if err != nil {
		return nil, ErrNotFound
	}
	if r.entry == "" {
		f, err := os.Open(p) // #nosec G304 -- validated by ResolveSafePath
		if err != nil {
			return nil, ErrNotFound
		}
		defer func() { _ = f.Close() }()
		data, err := io.ReadAll(io.LimitReader(f, books.MaxEntryBytes+1))
		if err != nil || len(data) > books.MaxEntryBytes {
			return nil, ErrNotFound
		}
		return data, nil
	}
	a, err := books.OpenArchive(p)
	if err != nil {
		return nil, ErrNotFound
	}
	defer func() { _ = a.Close() }()
	data, err := a.ReadFile(r.entry, books.MaxEntryBytes)
	if err != nil {
		return nil, ErrNotFound
	}
	return data, nil
}

// Picture is what the image endpoint sends.
type Picture struct {
	Data        io.ReadCloser
	ContentType string
	Name        string
}

var mimeOf = map[string]string{"jpeg": "image/jpeg", "png": "image/png", "webp": "image/webp", "gif": "image/gif"}

// Image returns a gallery picture: "thumb" (a cached JPEG thumbnail), "full"
// (the original; HEIC/AVIF converted to a cached JPEG) or "download" (the
// original file as it is; the caller checks the download permission).
func (s *Service) Image(ctx context.Context, id uuid.UUID, variant string) (Picture, error) {
	r, err := s.imageRow(ctx, id)
	if err != nil {
		return Picture{}, err
	}
	name := filepath.Base(r.path)
	if r.entry != "" {
		name = filepath.Base(strings.ReplaceAll(r.entry, "\\", "/"))
	}
	switch variant {
	case "download":
		data, err := s.readSource(r)
		if err != nil {
			return Picture{}, err
		}
		ct := mimeOf[r.format]
		if ct == "" {
			ct = "application/octet-stream"
		}
		return Picture{Data: io.NopCloser(bytes.NewReader(data)), ContentType: ct, Name: name}, nil
	case "full":
		if ct := mimeOf[r.format]; ct != "" {
			data, err := s.readSource(r)
			if err != nil {
				return Picture{}, err
			}
			return Picture{Data: io.NopCloser(bytes.NewReader(data)), ContentType: ct, Name: name}, nil
		}
		f, err := s.cached(ctx, id, r, s.fullPath(id), 0)
		if err != nil {
			return Picture{}, err
		}
		return Picture{Data: f, ContentType: "image/jpeg", Name: strings.TrimSuffix(name, filepath.Ext(name)) + ".jpg"}, nil
	case "thumb":
		f, err := s.cached(ctx, id, r, s.thumbPath(id), ThumbSize)
		if err != nil {
			return Picture{}, err
		}
		return Picture{Data: f, ContentType: "image/jpeg", Name: "thumb.jpg"}, nil
	}
	return Picture{}, ErrNotFound
}

// cached returns the cached JPEG at dst, making it first (scaled to size, 0 =
// full size) when it does not exist yet.
func (s *Service) cached(ctx context.Context, id uuid.UUID, r imageRow, dst string, size int) (*os.File, error) {
	if dst == "" {
		return nil, ErrNotFound
	}
	if f, err := os.Open(dst); err == nil { // #nosec G304 -- built from a UUID under ImageDir
		return f, nil
	}
	mu, _ := thumbLocks.LoadOrStore(id.String()+dst, &sync.Mutex{})
	mu.(*sync.Mutex).Lock()
	defer mu.(*sync.Mutex).Unlock()
	if f, err := os.Open(dst); err == nil { // #nosec G304 -- see above
		return f, nil
	}
	select {
	case thumbSlots <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-thumbSlots }()
	data, err := s.readSource(r)
	if err != nil {
		return nil, err
	}
	out, err := s.render(ctx, data, r.orientation, size)
	if err != nil {
		return nil, ErrNotFound
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		return nil, err
	}
	tmp := fmt.Sprintf("%s.%d.tmp", dst, time.Now().UnixNano())
	if err := os.WriteFile(tmp, out, 0o600); err != nil {
		return nil, err
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return nil, err
	}
	return os.Open(dst) // #nosec G304 -- see above
}

// render turns picture bytes into an upright JPEG no larger than size (0 = as
// large as it is). ffmpeg reads every format (WebP, HEIC, AVIF); without it the
// standard library handles JPEG, PNG and GIF.
func (s *Service) render(ctx context.Context, data []byte, orientation, size int) ([]byte, error) {
	if s.FFmpeg != "" {
		vf := orientationFilter(orientation)
		if size > 0 {
			vf += fmt.Sprintf("scale='if(gt(iw,ih),min(%[1]d,iw),-2)':'if(gt(iw,ih),-2,min(%[1]d,ih))'", size)
		} else {
			vf = strings.TrimSuffix(vf, ",")
		}
		args := []string{"-v", "error", "-noautorotate", "-i", "pipe:0"}
		if vf != "" {
			args = append(args, "-vf", vf)
		}
		args = append(args, "-frames:v", "1", "-f", "image2", "-c:v", "mjpeg", "-q:v", "4", "pipe:1")
		cctx, cancel := context.WithTimeout(ctx, 60*time.Second)
		defer cancel()
		// #nosec G204 -- fixed binary and arguments; the picture comes in on stdin.
		cmd := exec.CommandContext(cctx, s.FFmpeg, args...)
		cmd.Stdin = bytes.NewReader(data)
		var out bytes.Buffer
		cmd.Stdout = &out
		if err := cmd.Run(); err == nil && out.Len() > 100 {
			return out.Bytes(), nil
		}
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	img = orient(img, orientation)
	if size > 0 {
		img = downscale(img, size)
	}
	var out bytes.Buffer
	if err := jpeg.Encode(&out, img, &jpeg.Options{Quality: 82}); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// orientationFilter undoes EXIF orientation 2–8 for ffmpeg (its own rotation is off).
func orientationFilter(o int) string {
	switch o {
	case 2:
		return "hflip,"
	case 3:
		return "hflip,vflip,"
	case 4:
		return "vflip,"
	case 5:
		return "transpose=0,"
	case 6:
		return "transpose=1,"
	case 7:
		return "transpose=3,"
	case 8:
		return "transpose=2,"
	}
	return ""
}

// orient applies EXIF orientation in Go (the fallback without ffmpeg).
func orient(src image.Image, o int) image.Image {
	if o < 2 || o > 8 {
		return src
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	swap := o >= 5
	dw, dh := w, h
	if swap {
		dw, dh = h, w
	}
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for y := range h {
		for x := range w {
			var dx, dy int
			switch o {
			case 2:
				dx, dy = w-1-x, y
			case 3:
				dx, dy = w-1-x, h-1-y
			case 4:
				dx, dy = x, h-1-y
			case 5:
				dx, dy = y, x
			case 6:
				dx, dy = h-1-y, x
			case 7:
				dx, dy = h-1-y, w-1-x
			case 8:
				dx, dy = y, w-1-x
			}
			dst.Set(dx, dy, src.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return dst
}

// downscale shrinks an image so its longest side is at most size (box filter).
func downscale(src image.Image, size int) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= size && h <= size {
		return src
	}
	dw, dh := size, h*size/w
	if h > w {
		dw, dh = w*size/h, size
	}
	dw, dh = max(dw, 1), max(dh, 1)
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for y := range dh {
		y0, y1 := y*h/dh, max((y+1)*h/dh, y*h/dh+1)
		for x := range dw {
			x0, x1 := x*w/dw, max((x+1)*w/dw, x*w/dw+1)
			var r, g, bl, a, n uint64
			for sy := y0; sy < y1; sy++ {
				for sx := x0; sx < x1; sx++ {
					cr, cg, cb, ca := src.At(b.Min.X+sx, b.Min.Y+sy).RGBA()
					r, g, bl, a, n = r+uint64(cr), g+uint64(cg), bl+uint64(cb), a+uint64(ca), n+1
				}
			}
			dst.Set(x, y, color.RGBA64{uint16(r / n), uint16(g / n), uint16(bl / n), uint16(a / n)}) // #nosec G115 -- averages of 16-bit values
		}
	}
	return dst
}
