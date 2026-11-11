package livetv

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// --- .nfo -----------------------------------------------------------------------

type nfoEpisode struct {
	XMLName   xml.Name `xml:"episodedetails"`
	Title     string   `xml:"title"`
	ShowTitle string   `xml:"showtitle"`
	Season    *int     `xml:"season,omitempty"`
	Episode   *int     `xml:"episode,omitempty"`
	Plot      string   `xml:"plot,omitempty"`
	Aired     string   `xml:"aired,omitempty"`
	Genres    []string `xml:"genre"`
	Studio    string   `xml:"studio,omitempty"`
}

type nfoMovie struct {
	XMLName xml.Name `xml:"movie"`
	Title   string   `xml:"title"`
	Year    *int     `xml:"year,omitempty"`
	Plot    string   `xml:"plot,omitempty"`
	Genres  []string `xml:"genre"`
	Studio  string   `xml:"studio,omitempty"`
}

type nfoShow struct {
	XMLName xml.Name `xml:"tvshow"`
	Title   string   `xml:"title"`
	Genres  []string `xml:"genre"`
	Studio  string   `xml:"studio,omitempty"`
}

// writeNFO puts the guide's metadata beside the recording (Kodi .nfo, which
// the library reads as local metadata), and a tvshow.nfo in a new series folder.
func (s *Service) writeNFO(r Recording) error {
	stem := strings.TrimSuffix(r.Path, filepath.Ext(r.Path))
	var doc any
	isMovie := strings.HasPrefix(filepath.ToSlash(r.Path), filepath.ToSlash(filepath.Join(s.RecordingsDir, "Movies"))+"/")
	if isMovie {
		doc = nfoMovie{Title: r.Title, Year: r.Year, Plot: r.Description, Genres: r.Categories, Studio: r.ChannelName}
	} else {
		title := r.SubTitle
		if title == "" {
			title = r.Title + " " + r.Start.Local().Format("2006-01-02")
		}
		doc = nfoEpisode{Title: title, ShowTitle: r.Title, Season: r.Season, Episode: r.Episode, Plot: r.Description,
			Aired: r.Start.Local().Format("2006-01-02"), Genres: r.Categories, Studio: r.ChannelName}
		showDir := filepath.Join(s.RecordingsDir, safeName(r.Title))
		show := filepath.Join(showDir, "tvshow.nfo")
		if _, err := os.Stat(show); errors.Is(err, os.ErrNotExist) {
			_ = writeXML(show, nfoShow{Title: r.Title, Genres: r.Categories, Studio: r.ChannelName})
		}
	}
	return writeXML(stem+".nfo", doc)
}

func writeXML(path string, v any) error {
	b, err := xml.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append([]byte(xml.Header), b...), 0o640) // #nosec G306 -- read by the library scanner only
}

// --- post-processing ----------------------------------------------------------------

// PostProcessPlaceholders are replaced in every argument (documented in the API).
var PostProcessPlaceholders = []string{"{path}", "{dir}", "{filename}", "{title}", "{subtitle}", "{channel}", "{channelNumber}",
	"{start}", "{end}", "{season}", "{episode}", "{recordingId}"}

func validatePostProcess(p PostProcess) error {
	cmd := strings.TrimSpace(p.Command)
	if cmd == "" {
		return nil
	}
	if !filepath.IsAbs(cmd) {
		return invalid("the post-processing command must be an absolute path")
	}
	st, err := os.Stat(cmd)
	if err != nil || !st.Mode().IsRegular() || st.Mode().Perm()&0o111 == 0 {
		return invalid("the post-processing command is not an executable file on the server")
	}
	if p.TimeoutMinutes < 1 || p.TimeoutMinutes > 1440 {
		return invalid("the post-processing timeout must be 1 to 1440 minutes")
	}
	return nil
}

// PostProcessArgs expands the placeholders of each argument. Each argument
// stays one argument (no shell, no word splitting), whatever the title contains.
func PostProcessArgs(args []string, r Recording) []string {
	num := func(v *int) string {
		if v == nil {
			return ""
		}
		return strconv.Itoa(*v)
	}
	rep := strings.NewReplacer(
		"{path}", r.Path, "{dir}", filepath.Dir(r.Path), "{filename}", filepath.Base(r.Path),
		"{title}", r.Title, "{subtitle}", r.SubTitle, "{channel}", r.ChannelName, "{channelNumber}", r.ChannelNumber,
		"{start}", r.Start.UTC().Format(time.RFC3339), "{end}", r.End.UTC().Format(time.RFC3339),
		"{season}", num(r.Season), "{episode}", num(r.Episode), "{recordingId}", r.ID.String())
	out := make([]string, len(args))
	for i, a := range args {
		out[i] = rep.Replace(a)
	}
	return out
}

// postProcess runs the admin's command for a finished recording.
func (s *Service) postProcess(ctx context.Context, r Recording) error {
	pp := s.LoadSettings(ctx).PostProcess
	if strings.TrimSpace(pp.Command) == "" {
		return nil
	}
	if err := validatePostProcess(pp); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(pp.TimeoutMinutes)*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, pp.Command, PostProcessArgs(pp.Args, r)...) // #nosec G204 -- admin-configured program, run without a shell
	cmd.Dir = filepath.Dir(r.Path)
	cmd.Env = []string{"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C.UTF-8", "HOME=" + os.TempDir(),
		"STREAMERO_RECORDING_PATH=" + r.Path, "STREAMERO_RECORDING_ID=" + r.ID.String()}
	var out strings.Builder
	cmd.Stdout = &limited{b: &out, n: 4096}
	cmd.Stderr = cmd.Stdout
	err := cmd.Run()
	if err != nil {
		msg := strings.TrimSpace(out.String())
		if len(msg) > 500 {
			msg = msg[len(msg)-500:]
		}
		s.log().Warn("post-processing failed", "path", r.Path, "err", err, "output", msg)
		return fmt.Errorf("%w: %s", err, msg)
	}
	s.log().Info("post-processing done", "path", r.Path)
	return nil
}
