package books

import (
	"encoding/xml"
	"errors"
	"os"
	"path"
	"strings"
)

// ErrNoCover is returned when a file carries no cover image.
var ErrNoCover = errors.New("no cover in file")

// Read returns the metadata of a book or comic file (path validated by the caller).
func Read(p string) (Info, error) {
	format := FormatOf(p)
	var (
		in  Info
		err error
	)
	switch {
	case format == "epub":
		in, err = readEPUB(p)
	case IsComic(format):
		in, err = readComic(p)
	case format == "pdf":
		in, err = readPDF(p)
	case format == "mobi" || format == "azw3":
		in, _, err = readMOBI(p, false)
	default:
		return Info{}, ErrUnsupported
	}
	in.Format = format
	return in, err
}

// Cover returns the cover image embedded in a book file: the EPUB cover image, the
// first comic page or the MOBI cover record. PDFs are rendered by the caller.
func Cover(p string) ([]byte, error) {
	format := FormatOf(p)
	switch {
	case format == "epub":
		a, err := OpenArchive(p)
		if err != nil {
			return nil, err
		}
		defer func() { _ = a.Close() }()
		opfPath, err := rootFile(a)
		if err != nil {
			return nil, err
		}
		data, err := a.ReadFile(opfPath, MaxMetaBytes)
		if err != nil {
			return nil, err
		}
		o, err := ParseOPF(data)
		if err != nil {
			return nil, err
		}
		if o.CoverHref == "" {
			return nil, ErrNoCover
		}
		return a.ReadFile(ResolveHref(opfPath, o.CoverHref), MaxEntryBytes)
	case IsComic(format):
		a, err := OpenArchive(p)
		if err != nil {
			return nil, err
		}
		defer func() { _ = a.Close() }()
		data, _, err := ReadPage(a, 0)
		if errors.Is(err, ErrNoSuchPage) {
			return nil, ErrNoCover
		}
		return data, err
	case format == "mobi" || format == "azw3":
		_, cover, err := readMOBI(p, true)
		if err == nil && cover == nil {
			err = ErrNoCover
		}
		return cover, err
	}
	return nil, ErrNoCover
}

// ResolveHref resolves an href found in the OPF against the OPF's own folder.
func ResolveHref(opfPath, href string) string {
	href = strings.SplitN(href, "#", 2)[0]
	href = strings.ReplaceAll(href, "%20", " ")
	return path.Clean(path.Join(path.Dir(opfPath), href))
}

func rootFile(a Archive) (string, error) {
	data, err := a.ReadFile("META-INF/container.xml", MaxMetaBytes)
	if err != nil {
		return "", errors.New("epub: no META-INF/container.xml")
	}
	var c struct {
		Rootfiles []struct {
			FullPath  string `xml:"full-path,attr"`
			MediaType string `xml:"media-type,attr"`
		} `xml:"rootfiles>rootfile"`
	}
	if err := xml.Unmarshal(data, &c); err != nil {
		return "", err
	}
	for _, r := range c.Rootfiles {
		if r.MediaType == "" || r.MediaType == "application/oebps-package+xml" {
			if !SafeName(r.FullPath) {
				return "", ErrUnsafeName
			}
			return r.FullPath, nil
		}
	}
	return "", errors.New("epub: no package document")
}

func readEPUB(p string) (Info, error) {
	a, err := OpenArchive(p)
	if err != nil {
		return Info{}, err
	}
	defer func() { _ = a.Close() }()
	opfPath, err := rootFile(a)
	if err != nil {
		return Info{}, err
	}
	data, err := a.ReadFile(opfPath, MaxMetaBytes)
	if err != nil {
		return Info{}, err
	}
	o, err := ParseOPF(data)
	return o.Info, err
}

func readComic(p string) (Info, error) {
	a, err := OpenArchive(p)
	if err != nil {
		return Info{}, err
	}
	defer func() { _ = a.Close() }()
	var in Info
	for _, e := range a.Entries() {
		if strings.EqualFold(path.Base(e.Name), "ComicInfo.xml") {
			if data, err := a.ReadFile(e.Name, MaxMetaBytes); err == nil {
				if ci, err := ParseComicInfo(data); err == nil {
					in = ci
				}
			}
			break
		}
	}
	// The archive itself is the truth about how many pages there are.
	in.PageCount = len(Pages(a.Entries()))
	return in, nil
}

func readPDF(p string) (Info, error) {
	f, err := os.Open(p) //nolint:gosec // G304: validated by the caller
	if err != nil {
		return Info{}, err
	}
	defer func() { _ = f.Close() }()
	st, err := f.Stat()
	if err != nil {
		return Info{}, err
	}
	return ParsePDF(f, st.Size())
}
