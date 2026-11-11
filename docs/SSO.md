# Single sign-on (OpenID Connect) and LDAP

Streamero can let people sign in with an identity provider (OpenID Connect: Authentik,
Keycloak, Authelia, Microsoft Entra ID, Google, or any provider with
`/.well-known/openid-configuration`) and with a directory (LDAP / Active Directory).
Both are built in; no plugin is needed. Design notes: D-037 in `DECISIONS.md`.

Everything is configured in the web client under **Settings → Sign-in** (administrators).

## 1. Public address
Set **Public address** to the URL people open Streamero at, e.g.
`https://media.example.com`. Single sign-on needs it to build the redirect URI, and
the redirect URI must match the provider's registration exactly. Until it is set, no
single sign-on buttons are shown.

Sign in through that address: the sign-in is bound to the browser by a short-lived
cookie on the public address, so starting on `http://192.168.1.10:8787` and coming back
on `https://media.example.com` fails with "The sign-in expired".

## 2. OpenID Connect providers
**Add provider**, pick a type (it pre-fills the usual values), then register Streamero at
the provider as a *confidential web application* with:

| Setting at the provider | Value |
|---|---|
| Redirect URI | shown under the provider in Streamero: `https://media.example.com/api/v1/auth/oidc/<id>/callback` |
| Grant type | Authorization code (PKCE S256 is always used) |
| Scopes | `openid profile email` (+ `groups` where the provider needs it) |

Copy the client ID and secret into Streamero. The secret is stored on the server and
never shown again (leave the field empty to keep it). **Test connection** fetches the
discovery document and the signing keys.

| Provider | Issuer | Notes |
|---|---|---|
| Authentik | `https://auth.example.com/application/o/<slug>/` | Groups are in the `groups` claim (profile scope). |
| Keycloak | `https://auth.example.com/realms/<realm>` | Add a *Group Membership* mapper (claim `groups`, full path off) or use `realm_access.roles` as the groups claim. |
| Authelia | `https://auth.example.com` | Add the `groups` scope; Authelia puts most claims in UserInfo, which Streamero reads. |
| Microsoft Entra ID | `https://login.microsoftonline.com/<tenant-id>/v2.0` | Use the tenant ID (the multi-tenant `common` endpoint is not supported). Groups come as object IDs (enable the groups claim in *Token configuration*). Entra does not send `email_verified`; see *Trust e-mail*. |
| Google | `https://accounts.google.com` | No groups. Use `email` as the username claim. |

### Claims and groups
- **Username / e-mail / display name claim**: where the new account's details come from.
- **Groups claim**: a claim name or a dotted path (`realm_access.roles`).
- **Administrator group**: members become administrators, others lose admin rights on
  their next sign-in. The last enabled administrator is never demoted this way. Empty:
  admin rights are managed in Streamero.
- **Allowed group**: only members may sign in (also already-linked users).

### Which Streamero account is used
1. The provider account is already linked → that user.
2. **Link by verified e-mail** (on by default): exactly one user has that e-mail address
   (My account → E-mail) and the provider says the address is verified
   (`email_verified: true`) → linked to that user. *Trust e-mail* treats every address
   from this provider as verified; only turn it on for a provider you control.
3. **Create accounts automatically** → a new user without a password.
4. Otherwise the sign-in is refused ("no account").

Users can link and unlink providers themselves under **My account → Linked sign-in**.
An account without a password cannot unlink its only identity.

Single sign-on does not ask for Streamero's own two-factor code: the identity provider is
responsible for MFA.

### Apps
The iOS and Android apps show the same buttons. They open the provider in the system
browser sheet (iOS `ASWebAuthenticationSession`, Android Custom Tabs) and come back
through `streamero://oidc`. Nothing has to be registered at the provider for the apps.
Third-party apps (Infuse, TV apps…) cannot use single sign-on: use Quick Connect.

## 3. LDAP / Active Directory
Directory users sign in with the normal username and password fields: web, iOS, Android
and third-party apps (compat API). Their password is checked by the directory and never
stored. The Streamero account is created on the first successful sign-in.

| Field | OpenLDAP example | Active Directory example |
|---|---|---|
| Server | `ldaps://ldap.example.com:636` | `ldaps://dc1.example.com:636` |
| Bind DN | `cn=streamero,ou=services,dc=example,dc=org` | `CN=streamero,OU=Service,DC=example,DC=com` |
| Base DN | `dc=example,dc=org` | `DC=example,DC=com` |
| User filter | `(&(objectClass=inetOrgPerson)(uid={username}))` | `(&(objectClass=user)(sAMAccountName={username}))` |
| Username attribute | `uid` | `sAMAccountName` |
| Group membership | `memberOf` (memberof overlay) or a group filter `(&(objectClass=groupOfNames)(member={dn}))` | `memberOf`, or for nested groups `(member:1.2.840.113556.1.4.1941:={dn})` as group filter |
| Administrator / allowed group | group DN | group DN |

- Use `ldaps://` or `ldap://` with **StartTLS** (default). The certificate is verified;
  paste your CA under *Trusted CA certificate* for a private CA. *Skip certificate check*
  exists for testing only.
- Usernames that are Streamero accounts without a directory link keep using their
  Streamero password. A directory user's account uses the directory, also for confirming
  the password (turning off two-factor, leaving a kids profile).
- Failed attempts count towards the normal lockout (5 attempts, 15 minutes) and the
  sign-in rate limit per IP.
- **Test connection** binds with the service account; with a username it also shows the
  DN, e-mail, groups and whether the user would be an administrator / allowed.

## 4. Turning password sign-in off
**Turn off password sign-in** hides the password form and refuses Streamero passwords
(LDAP users still sign in with the form). It can only be switched on while an enabled
administrator has a linked identity of an enabled provider or LDAP; removing or
disabling the last such provider is refused while it is on.

### Recovery (locked out)
If the identity provider is down or misconfigured and no administrator can sign in:

1. Set `STREAMERO_PASSWORD_LOGIN_RECOVERY=true` for the `streamero` service (e.g. in
   `deploy/.env` or the compose file) and restart it. The log warns while it is set.
2. Sign in as an **administrator** with the Streamero password (non-admins stay blocked).
   Administrators created by single sign-on have no password; the administrator from the
   first-run setup has one (it is kept when they link a provider). Keep at least one
   administrator with a password, or give one a password under Settings → Users while
   you still can.
3. Fix the provider or turn password sign-in back on in Settings → Sign-in.
4. Remove the variable and restart.

Quick Connect keeps working for already signed-in users throughout.
