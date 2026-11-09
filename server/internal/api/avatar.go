package api

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	_ "image/gif" // avatar uploads
	"image/jpeg"
	_ "image/png" // avatar uploads
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/google/uuid"

	"github.com/streamero-app/streamero/server/internal/api/gen"
	"github.com/streamero-app/streamero/server/internal/auth"
	"github.com/streamero-app/streamero/server/internal/library"
)

// avatarSize is the edge of the stored square picture.
const avatarSize = 256

// maxAvatarBytes bounds an upload.
const maxAvatarBytes = 8 << 20

func (h *Handler) avatarPath(id uuid.UUID) string {
	// Built from a UUID, never from client text.
	return filepath.Join(h.Paths.Cache, "avatars", id.String()+".jpg")
}

var errBadImage = &StatusError{Status: http.StatusBadRequest, Code: "invalid_image", Message: "the file is not a JPEG, PNG or GIF picture"}

// squareAvatar decodes an image, crops the centre square and scales it down
// with an area average so faces stay smooth.
func squareAvatar(r io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxAvatarBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxAvatarBytes {
		return nil, &StatusError{Status: http.StatusRequestEntityTooLarge, Code: "image_too_large", Message: "the picture is larger than 8 MB"}
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width*cfg.Height > 50_000_000 {
		return nil, errBadImage
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, errBadImage
	}
	b := src.Bounds()
	side := min(b.Dx(), b.Dy())
	x0, y0 := b.Min.X+(b.Dx()-side)/2, b.Min.Y+(b.Dy()-side)/2
	out := min(avatarSize, side)
	dst := image.NewRGBA(image.Rect(0, 0, out, out))
	for y := 0; y < out; y++ {
		sy0, sy1 := y0+y*side/out, y0+(y+1)*side/out
		for x := 0; x < out; x++ {
			sx0, sx1 := x0+x*side/out, x0+(x+1)*side/out
			var r, g, bl, a, n uint64
			for sy := sy0; sy < max(sy1, sy0+1); sy++ {
				for sx := sx0; sx < max(sx1, sx0+1); sx++ {
					cr, cg, cb, ca := src.At(sx, sy).RGBA()
					r, g, bl, a, n = r+uint64(cr), g+uint64(cg), bl+uint64(cb), a+uint64(ca), n+1
				}
			}
			// Transparent parts become the dark page colour, not black.
			alpha := float64(a) / float64(n) / 0xffff
			mix := func(v uint64, bg float64) uint8 {
				return uint8(float64(v)/float64(n)/0xffff*255 + (1-alpha)*bg)
			}
			dst.Set(x, y, color.RGBA{mix(r, 20), mix(g, 17), mix(bl, 15), 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 88}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (h *Handler) saveAvatar(id uuid.UUID, r io.Reader) error {
	data, err := squareAvatar(r)
	if err != nil {
		return err
	}
	dst := h.avatarPath(id)
	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		return err
	}
	if err := os.WriteFile(dst+".tmp", data, 0o600); err != nil {
		return err
	}
	return os.Rename(dst+".tmp", dst)
}

// UpdateMe implements PUT /auth/me.
func (h *Handler) UpdateMe(ctx context.Context, req *gen.UpdateMeRequest) (*gen.User, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	in := auth.AccountInput{}
	if v, ok := req.DisplayName.Get(); ok {
		in.DisplayName = &v
	}
	if v, ok := req.Email.Get(); ok {
		in.Email = &v
	}
	if v, ok := req.Language.Get(); ok {
		l := string(v)
		in.Language = &l
	}
	if _, err := h.Auth().UpdateAccount(ctx, p.User.ID, in); err != nil {
		if errors.Is(err, auth.ErrInvalidEmail) {
			return nil, &StatusError{Status: http.StatusBadRequest, Code: "invalid_email", Message: err.Error()}
		}
		return nil, err
	}
	return h.GetMe(ctx)
}

// SetMyAvatar implements PUT /auth/me/avatar.
func (h *Handler) SetMyAvatar(ctx context.Context, req gen.SetMyAvatarReq) error {
	p, err := principal(ctx)
	if err != nil {
		return err
	}
	if err := h.saveAvatar(p.User.ID, req.Data); err != nil {
		return err
	}
	return h.Auth().SetUserAvatar(ctx, p.User.ID, true)
}

// DeleteMyAvatar implements DELETE /auth/me/avatar.
func (h *Handler) DeleteMyAvatar(ctx context.Context) error {
	p, err := principal(ctx)
	if err != nil {
		return err
	}
	_ = os.Remove(h.avatarPath(p.User.ID))
	return h.Auth().SetUserAvatar(ctx, p.User.ID, false)
}

func (h *Handler) ownProfile(ctx context.Context, id uuid.UUID) error {
	p, err := adult(ctx)
	if err != nil {
		return err
	}
	ok, err := h.Auth().OwnsProfile(ctx, p.User.ID, id)
	if err != nil {
		return err
	}
	if !ok {
		return library.ErrNotFound
	}
	return nil
}

// SetProfileAvatar implements PUT /profiles/{profileId}/avatar.
func (h *Handler) SetProfileAvatar(ctx context.Context, req gen.SetProfileAvatarReq, params gen.SetProfileAvatarParams) error {
	if err := h.ownProfile(ctx, params.ProfileId); err != nil {
		return err
	}
	if err := h.saveAvatar(params.ProfileId, req.Data); err != nil {
		return err
	}
	return h.Auth().SetProfileAvatar(ctx, params.ProfileId, true)
}

// DeleteProfileAvatar implements DELETE /profiles/{profileId}/avatar.
func (h *Handler) DeleteProfileAvatar(ctx context.Context, params gen.DeleteProfileAvatarParams) error {
	if err := h.ownProfile(ctx, params.ProfileId); err != nil {
		return err
	}
	_ = os.Remove(h.avatarPath(params.ProfileId))
	return h.Auth().SetProfileAvatar(ctx, params.ProfileId, false)
}

// GetAvatar implements GET /avatars/{ownerId}.
func (h *Handler) GetAvatar(ctx context.Context, params gen.GetAvatarParams) (*gen.GetAvatarOKHeaders, error) {
	if _, err := principal(ctx); err != nil {
		return nil, err
	}
	f, err := os.Open(h.avatarPath(params.OwnerId))
	if err != nil {
		return nil, library.ErrNotFound
	}
	return &gen.GetAvatarOKHeaders{CacheControl: gen.NewOptString("private, max-age=300"), Response: gen.GetAvatarOK{Data: f}}, nil
}
