package auth

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/go-ldap/ldap/v3"
	"github.com/jackc/pgx/v5"

	"github.com/streamero-app/streamero/server/internal/settings"
)

// LDAPSettings configure directory sign-in (OpenLDAP, Active Directory, …).
type LDAPSettings struct {
	Enabled      bool   `json:"enabled"`
	URL          string `json:"url"`
	StartTLS     bool   `json:"startTls"`
	SkipVerify   bool   `json:"skipVerify"`
	CACert       string `json:"caCert"`
	BindDN       string `json:"bindDn"`
	BindPassword string `json:"bindPassword"` // never returned by the API
	BaseDN       string `json:"baseDn"`
	UserFilter   string `json:"userFilter"`
	UsernameAttr string `json:"usernameAttribute"`
	EmailAttr    string `json:"emailAttribute"`
	NameAttr     string `json:"nameAttribute"`
	GroupAttr    string `json:"groupAttribute"`
	GroupFilter  string `json:"groupFilter"`
	AdminGroup   string `json:"adminGroup"`
	AllowedGroup string `json:"allowedGroup"`
}

// ldapTimeout bounds every directory connection and request.
const ldapTimeout = 10 * time.Second

func (c *LDAPSettings) applyDefaults() {
	if c.UserFilter == "" {
		c.UserFilter = "(&(objectClass=inetOrgPerson)(uid={username}))"
	}
	if c.UsernameAttr == "" {
		c.UsernameAttr = "uid"
	}
	if c.EmailAttr == "" {
		c.EmailAttr = "mail"
	}
	if c.NameAttr == "" {
		c.NameAttr = "cn"
	}
	if c.GroupAttr == "" {
		c.GroupAttr = "memberOf"
	}
}

// LDAPSettings returns the directory settings (with defaults filled in).
func (s *Service) LDAPSettings(ctx context.Context) (LDAPSettings, error) {
	c := LDAPSettings{StartTLS: true}
	if _, err := settings.New(s.pool).Get(ctx, keyAuthLDAP, &c); err != nil {
		return c, err
	}
	c.applyDefaults()
	return c, nil
}

// LDAPInput is a change of directory settings; an empty password keeps the stored one.
type LDAPInput struct {
	LDAPSettings
	RemoveBindPassword bool
}

// ValidateLDAP checks settings and fills in the stored bind password when none is given.
func (s *Service) ValidateLDAP(ctx context.Context, in LDAPInput) (LDAPSettings, error) {
	c := in.LDAPSettings
	trim := func(p *string) { *p = strings.TrimSpace(*p) }
	for _, p := range []*string{&c.URL, &c.BindDN, &c.BaseDN, &c.UserFilter, &c.UsernameAttr, &c.EmailAttr, &c.NameAttr,
		&c.GroupAttr, &c.GroupFilter, &c.AdminGroup, &c.AllowedGroup} {
		trim(p)
	}
	c.applyDefaults()
	if c.BindPassword == "" && !in.RemoveBindPassword {
		old, err := s.LDAPSettings(ctx)
		if err != nil {
			return c, err
		}
		c.BindPassword = old.BindPassword
	}
	if c.URL == "" && !c.Enabled {
		return c, nil
	}
	u, err := url.Parse(c.URL)
	if err != nil || (u.Scheme != "ldap" && u.Scheme != "ldaps") || u.Hostname() == "" {
		return c, invalid("the server address must look like ldaps://host:636 or ldap://host:389")
	}
	if u.Scheme == "ldaps" {
		c.StartTLS = false
	}
	if c.BaseDN == "" {
		return c, invalid("a base DN is required")
	}
	if !strings.Contains(c.UserFilter, "{username}") {
		return c, invalid("the user filter must contain {username}")
	}
	if _, err := ldap.CompileFilter(strings.ReplaceAll(c.UserFilter, "{username}", "x")); err != nil {
		return c, invalid("the user filter is not a valid LDAP filter")
	}
	if c.GroupFilter != "" {
		f := strings.NewReplacer("{dn}", "x", "{username}", "x").Replace(c.GroupFilter)
		if _, err := ldap.CompileFilter(f); err != nil {
			return c, invalid("the group filter is not a valid LDAP filter")
		}
	}
	if c.CACert != "" && !x509.NewCertPool().AppendCertsFromPEM([]byte(c.CACert)) {
		return c, invalid("the CA certificate is not valid PEM")
	}
	return c, nil
}

// SaveLDAP stores directory settings.
func (s *Service) SaveLDAP(ctx context.Context, actor Principal, in LDAPInput) (LDAPSettings, error) {
	c, err := s.ValidateLDAP(ctx, in)
	if err != nil {
		return c, err
	}
	if !c.Enabled {
		// Turning LDAP off must not leave password sign-in off with no admin able to get in.
		off := true
		if err := s.guardExternalAdmin(ctx, "", &off); err != nil {
			return c, err
		}
	}
	if err := settings.New(s.pool).Set(ctx, keyAuthLDAP, c); err != nil {
		return c, err
	}
	_ = s.Audit(ctx, &actor.User.ID, "auth.ldap_changed", map[string]any{"enabled": c.Enabled, "url": c.URL})
	return c, nil
}

// LDAPEntry is a directory user found by a search.
type LDAPEntry struct {
	DN, Subject, Username, Email, DisplayName string
	Groups                                    []string
	IsAdmin, Allowed                          bool
}

func (c LDAPSettings) tlsConfig(host string) (*tls.Config, error) {
	tc := &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12, InsecureSkipVerify: c.SkipVerify} // #nosec G402 -- opt-in, off by default
	if c.CACert != "" {
		pool, err := x509.SystemCertPool()
		if err != nil || pool == nil {
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM([]byte(c.CACert)) {
			return nil, invalid("the CA certificate is not valid PEM")
		}
		tc.RootCAs = pool
	}
	return tc, nil
}

// ldapConnect dials, secures (LDAPS or StartTLS) and binds with the service account.
func ldapConnect(c LDAPSettings) (*ldap.Conn, error) {
	u, err := url.Parse(c.URL)
	if err != nil {
		return nil, err
	}
	tc, err := c.tlsConfig(u.Hostname())
	if err != nil {
		return nil, err
	}
	conn, err := ldap.DialURL(c.URL, ldap.DialWithDialer(&net.Dialer{Timeout: ldapTimeout}), ldap.DialWithTLSConfig(tc))
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	conn.SetTimeout(ldapTimeout)
	if u.Scheme == "ldap" && c.StartTLS {
		if err := conn.StartTLS(tc); err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("StartTLS: %w", err)
		}
	}
	if c.BindDN != "" {
		if err := conn.Bind(c.BindDN, c.BindPassword); err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("service account bind: %w", err)
		}
	}
	return conn, nil
}

// ldapFind looks a user up with the service connection (exactly one match).
func ldapFind(conn *ldap.Conn, c LDAPSettings, username string) (*LDAPEntry, error) {
	filter := strings.ReplaceAll(c.UserFilter, "{username}", ldap.EscapeFilter(username))
	attrs := []string{c.UsernameAttr, c.EmailAttr, c.NameAttr, c.GroupAttr, "entryUUID", "objectGUID"}
	res, err := conn.Search(ldap.NewSearchRequest(c.BaseDN, ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 2,
		int(ldapTimeout.Seconds()), false, filter, attrs, nil))
	if err != nil && !ldap.IsErrorWithCode(err, ldap.LDAPResultSizeLimitExceeded) {
		return nil, fmt.Errorf("user search: %w", err)
	}
	if res == nil || len(res.Entries) != 1 {
		return nil, ErrInvalidCredentials // none, or ambiguous
	}
	e := res.Entries[0]
	out := &LDAPEntry{DN: e.DN, Username: e.GetAttributeValue(c.UsernameAttr), Email: e.GetAttributeValue(c.EmailAttr),
		DisplayName: e.GetAttributeValue(c.NameAttr)}
	switch {
	case e.GetAttributeValue("entryUUID") != "":
		out.Subject = "entryUUID:" + e.GetAttributeValue("entryUUID")
	case len(e.GetRawAttributeValue("objectGUID")) > 0:
		out.Subject = "objectGUID:" + hex.EncodeToString(e.GetRawAttributeValue("objectGUID"))
	default:
		out.Subject = "dn:" + strings.ToLower(e.DN)
	}
	if c.GroupFilter != "" {
		gf := strings.NewReplacer("{dn}", ldap.EscapeFilter(e.DN), "{username}", ldap.EscapeFilter(username)).Replace(c.GroupFilter)
		gr, err := conn.Search(ldap.NewSearchRequest(c.BaseDN, ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 500,
			int(ldapTimeout.Seconds()), false, gf, []string{"dn"}, nil))
		if err != nil && !ldap.IsErrorWithCode(err, ldap.LDAPResultSizeLimitExceeded) {
			return nil, fmt.Errorf("group search: %w", err)
		}
		if gr != nil {
			for _, g := range gr.Entries {
				out.Groups = append(out.Groups, g.DN)
			}
		}
	} else {
		out.Groups = e.GetAttributeValues(c.GroupAttr)
	}
	out.IsAdmin = c.AdminGroup != "" && hasDN(out.Groups, c.AdminGroup)
	out.Allowed = c.AllowedGroup == "" || hasDN(out.Groups, c.AllowedGroup)
	return out, nil
}

// hasDN compares DNs the way directories do for our purposes: case- and
// space-around-separators-insensitive.
func hasDN(list []string, want string) bool {
	norm := func(s string) string {
		if dn, err := ldap.ParseDN(s); err == nil {
			parts := make([]string, 0, len(dn.RDNs))
			for _, r := range dn.RDNs {
				for _, a := range r.Attributes {
					parts = append(parts, strings.ToLower(a.Type)+"="+strings.ToLower(a.Value))
				}
			}
			return strings.Join(parts, ",")
		}
		return strings.ToLower(strings.TrimSpace(s))
	}
	w := norm(want)
	for _, g := range list {
		if norm(g) == w {
			return true
		}
	}
	return false
}

// ldapAuthenticate checks a username and password against the directory. It never
// binds without a password (an "unauthenticated bind" would succeed on many servers).
func (s *Service) ldapAuthenticate(_ context.Context, c LDAPSettings, username, password string) (*LDAPEntry, error) {
	username = strings.TrimSpace(username)
	if username == "" || password == "" {
		_, _ = VerifyPassword(password, dummyHash)
		return nil, ErrInvalidCredentials
	}
	conn, err := ldapConnect(c)
	if err != nil {
		return nil, err
	}
	defer func() { _ = conn.Close() }()
	entry, err := ldapFind(conn, c, username)
	if errors.Is(err, ErrInvalidCredentials) {
		_, _ = VerifyPassword(password, dummyHash) // do not reveal unknown users by timing
		return nil, err
	}
	if err != nil {
		return nil, err
	}
	if err := conn.Bind(entry.DN, password); err != nil {
		if ldap.IsErrorWithCode(err, ldap.LDAPResultInvalidCredentials) {
			return nil, ErrInvalidCredentials
		}
		return nil, fmt.Errorf("user bind: %w", err)
	}
	return entry, nil
}

// TestLDAP connects and binds with the given settings and optionally finds a user.
func (s *Service) TestLDAP(ctx context.Context, in LDAPInput, username string) (*LDAPEntry, error) {
	c, err := s.ValidateLDAP(ctx, in)
	if err != nil {
		return nil, err
	}
	conn, err := ldapConnect(c)
	if err != nil {
		return nil, err
	}
	defer func() { _ = conn.Close() }()
	if strings.TrimSpace(username) == "" {
		return nil, nil
	}
	e, err := ldapFind(conn, c, strings.TrimSpace(username))
	if errors.Is(err, ErrInvalidCredentials) {
		return nil, fmt.Errorf("no single user matches the filter for %q", username)
	}
	return e, err
}

// loginLDAP signs a directory user in, creating the Streamero account on first use.
func (s *Service) loginLDAP(ctx context.Context, c LDAPSettings, username, password, totpCode string, dev Device) (Principal, string, error) {
	entry, err := s.ldapAuthenticate(ctx, c, username, password)
	if err != nil {
		if errors.Is(err, ErrInvalidCredentials) {
			_ = audit(ctx, s.pool, nil, "login.failed", map[string]any{"username": username, "reason": "ldap_rejected"})
		}
		return Principal{}, "", err
	}
	if !entry.Allowed {
		_ = audit(ctx, s.pool, nil, "login.failed", map[string]any{"username": username, "reason": "ldap_not_in_group"})
		return Principal{}, "", ErrInvalidCredentials
	}
	p, token, err := s.externalSignIn(ctx, ldapAccount(c, entry, username), dev, func(u User) error {
		return s.requireSecondFactor(ctx, u, totpCode)
	})
	if errors.Is(err, ErrAccountDisabled) {
		return Principal{}, "", ErrInvalidCredentials
	}
	return p, token, err
}

func ldapAccount(c LDAPSettings, e *LDAPEntry, typed string) externalAccount {
	a := externalAccount{Kind: "ldap", Subject: e.Subject, Label: e.DN, Username: e.Username, Email: e.Email,
		DisplayName: e.DisplayName, EmailVerified: e.Email != "", AutoCreate: true}
	if a.Username == "" {
		a.Username = typed
	}
	if c.AdminGroup != "" {
		v := e.IsAdmin
		a.IsAdmin = &v
	}
	return a
}

// externalSignIn resolves the account and creates a session in one transaction.
// check (optional) runs on the resolved user before the session is created.
func (s *Service) externalSignIn(ctx context.Context, a externalAccount, dev Device, check func(User) error) (Principal, string, error) {
	var p Principal
	var token string
	now := s.now()
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		u, err := s.resolveExternal(ctx, tx, a)
		if err != nil {
			return err
		}
		if check != nil {
			if err := check(u); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE users SET failed_logins = 0, locked_until = NULL, last_login_at = $2 WHERE id = $1`,
			u.ID, now); err != nil {
			return err
		}
		u.LastLoginAt = &now
		if p, token, err = createSession(ctx, tx, u, dev, now); err != nil {
			return err
		}
		return audit(ctx, tx, &u.ID, "login.succeeded", map[string]any{"session_id": p.SessionID, "device": dev.Name,
			"method": a.Kind, "provider": a.ProviderID})
	})
	return p, token, err
}
