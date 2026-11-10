package compat

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

// Identifiers in this API are 32 lowercase hex digits (a GUID without dashes).
//
// Streamero's own objects (libraries, items, media files, users, sessions) are
// UUIDs, so their compat ID is the UUID without dashes and parses back to it.
// Objects that have no UUID in Streamero (people, genres) get a synthetic ID:
//
//	tttttttt 00000 ppppppppppppppppppp
//	  tag    zero        payload (19 hex digits)
//
// Character 12 is the UUID version nibble. Streamero only creates version 4 and
// 7 UUIDs, so a zero there can never collide with a real item ID.
const (
	tagPerson     = "70657273" // "pers": payload = TMDb person id
	tagPersonName = "706e616d" // "pnam": payload = hash of the name (people without a TMDb id)
	tagGenre      = "67656e72" // "genr": payload = hash of the lower-cased name
)

// ID formats a UUID the way clients of this API expect (32 hex digits).
func ID(u uuid.UUID) string { return strings.ReplaceAll(u.String(), "-", "") }

// ParseID accepts dashed and undashed forms.
func ParseID(s string) (uuid.UUID, error) { return uuid.Parse(strings.TrimSpace(s)) }

func synthetic(tag, payload string) string { return tag + "00000" + payload }

// PersonID is the ID of a person known to TMDb.
func PersonID(tmdbID int64) string { return synthetic(tagPerson, fmt.Sprintf("%019x", tmdbID)) }

// nameHash is the 19-digit payload for names; SQL computes the same value with nameHashSQL.
func nameHash(name string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(name)))
	return hex.EncodeToString(sum[:])[:19]
}

// nameHashSQL is the SQL expression of nameHash for a column.
func nameHashSQL(col string) string {
	return `substr(encode(sha256(convert_to(lower(` + col + `), 'UTF8')), 'hex'), 1, 19)`
}

// PersonNameID is the ID of a person without a TMDb id.
func PersonNameID(name string) string { return synthetic(tagPersonName, nameHash(name)) }

// GenreID is the ID of a genre.
func GenreID(name string) string { return synthetic(tagGenre, nameHash(name)) }

// syntheticKind returns the tag and payload of a synthetic ID ("" when s is not one).
func syntheticKind(s string) (tag, payload string) {
	s = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(s), "-", ""))
	if len(s) != 32 || s[8:13] != "00000" {
		return "", ""
	}
	if _, err := hex.DecodeString(s); err != nil {
		return "", ""
	}
	switch s[:8] {
	case tagPerson, tagPersonName, tagGenre:
		return s[:8], s[13:]
	}
	return "", ""
}

// personTag is the image tag of a person photo (photos do not change).
func personTag(personID string) string { return "p" + strings.ToLower(personID)[13:] }

// ParsePersonID returns the TMDb id of a person ID (ok = false for other IDs).
func ParsePersonID(s string) (int64, bool) {
	tag, payload := syntheticKind(s)
	if tag != tagPerson {
		return 0, false
	}
	n, err := strconv.ParseInt(payload, 16, 64)
	return n, err == nil && n > 0
}
