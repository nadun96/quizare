package directory

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/go-ldap/ldap/v3"

	"github.com/nadun96/quizplatform/internal/auth"
)

const (
	dialTimeout = 5 * time.Second
	opTimeout   = 10 * time.Second
	// inChain is Active Directory's rule for membership through nested groups.
	inChain = "1.2.840.113556.1.4.1941"
)

// errBind: the service account couldn't sign in (a setting, not the user).
var errBind = errors.New("the service account couldn't sign in")

// connect opens an encrypted connection: LDAPS, or LDAP upgraded with
// StartTLS. Certificates are checked against the system's CAs plus the
// configured one; there is no option to skip the check.
func connect(c Config) (*ldap.Conn, error) {
	u, err := url.Parse(c.URL)
	if err != nil {
		return nil, err
	}
	roots, err := x509.SystemCertPool()
	if err != nil || roots == nil {
		roots = x509.NewCertPool()
	}
	if c.CAPEM != "" && !roots.AppendCertsFromPEM([]byte(c.CAPEM)) {
		return nil, errors.New("the CA certificate can't be read")
	}
	tc := &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots, ServerName: u.Hostname()}
	d := &net.Dialer{Timeout: dialTimeout}
	var conn *ldap.Conn
	switch u.Scheme {
	case "ldaps":
		conn, err = ldap.DialURL(c.URL, ldap.DialWithDialer(d), ldap.DialWithTLSConfig(tc))
	case "ldap":
		if !c.StartTLS {
			return nil, errors.New("an unencrypted connection isn't allowed")
		}
		conn, err = ldap.DialURL(c.URL, ldap.DialWithDialer(d))
		if err == nil {
			if err = conn.StartTLS(tc); err != nil {
				conn.Close()
			}
		}
	default:
		return nil, fmt.Errorf("unknown scheme %q", u.Scheme)
	}
	if err != nil {
		return nil, err
	}
	conn.SetTimeout(opTimeout)
	return conn, nil
}

func serviceBind(conn *ldap.Conn, c Config, password string) error {
	if c.BindDN == "" {
		return nil // anonymous search (some LDAP servers allow it)
	}
	if err := conn.Bind(c.BindDN, password); err != nil {
		return fmt.Errorf("%w: %v", errBind, err)
	}
	return nil
}

// found is an entry the user filter matched.
type found struct {
	DN, ID, Email, Name string
	MemberOf            []string
}

func attrs(c Config) []string {
	return []string{c.IDAttr, c.EmailAttr, c.NameAttr, "userPrincipalName", "cn", "memberOf", "userAccountControl"}
}

// find looks the sign-in name up with the user filter. It must match
// exactly one entry: none or several is a failed sign-in.
func find(conn *ldap.Conn, c Config, login string) (found, error) {
	filter := strings.ReplaceAll(c.UserFilter, "{login}", ldap.EscapeFilter(login))
	res, err := conn.Search(ldap.NewSearchRequest(c.BaseDN, ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 2, int(opTimeout/time.Second), false, filter, attrs(c), nil))
	if ldap.IsErrorWithCode(err, ldap.LDAPResultSizeLimitExceeded) {
		return found{}, auth.ErrDirectoryCredentials // ambiguous: two accounts answer to this name
	}
	if err != nil {
		return found{}, err
	}
	if len(res.Entries) != 1 {
		return found{}, auth.ErrDirectoryCredentials
	}
	return describeEntry(res.Entries[0], c, login), nil
}

func describeEntry(e *ldap.Entry, c Config, login string) found {
	f := found{DN: e.DN, MemberOf: e.GetAttributeValues("memberOf")}
	if raw := e.GetRawAttributeValue(c.IDAttr); len(raw) > 0 {
		f.ID = idString(raw)
	} else {
		f.ID = "dn:" + strings.ToLower(e.DN)
	}
	f.Email = strings.ToLower(strings.TrimSpace(e.GetAttributeValue(c.EmailAttr)))
	if f.Email == "" {
		f.Email = strings.ToLower(strings.TrimSpace(e.GetAttributeValue("userPrincipalName")))
	}
	if a, err := mail.ParseAddress(f.Email); err != nil || a.Address != f.Email {
		f.Email = ""
	}
	f.Name = strings.TrimSpace(e.GetAttributeValue(c.NameAttr))
	if f.Name == "" {
		f.Name = strings.TrimSpace(e.GetAttributeValue("cn"))
	}
	if f.Name == "" {
		f.Name = login
	}
	if len(f.Name) > 200 {
		f.Name = f.Name[:200]
	}
	return f
}

// idString keeps a printable id (entryUUID) and writes a binary one
// (objectGUID) as hex.
func idString(raw []byte) string {
	if utf8.Valid(raw) {
		printable := true
		for _, r := range string(raw) {
			if !unicode.IsPrint(r) {
				printable = false
				break
			}
		}
		if printable {
			return strings.ToLower(string(raw))
		}
	}
	return "hex:" + hex.EncodeToString(raw)
}

// inGroup reports whether the entry is in the group: through Active
// Directory's in-chain rule (nested groups), or its memberOf values.
func inGroup(conn *ldap.Conn, c Config, f found, group string) (bool, error) {
	if group == "" {
		return false, nil
	}
	if c.NestedGroups {
		res, err := conn.Search(ldap.NewSearchRequest(f.DN, ldap.ScopeBaseObject, ldap.NeverDerefAliases, 1, int(opTimeout/time.Second), false,
			fmt.Sprintf("(memberOf:%s:=%s)", inChain, ldap.EscapeFilter(group)), []string{"1.1"}, nil))
		if ldap.IsErrorWithCode(err, ldap.LDAPResultNoSuchObject) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		return len(res.Entries) > 0, nil
	}
	want, err := ldap.ParseDN(group)
	if err != nil {
		return false, nil
	}
	for _, m := range f.MemberOf {
		if dn, err := ldap.ParseDN(m); err == nil && dn.EqualFold(want) {
			return true, nil
		}
	}
	return false, nil
}

// roleOf: the teacher group makes a teacher; then the student group, or,
// with no student group, everyone else found makes a student.
func roleOf(conn *ldap.Conn, c Config, f found) (auth.Role, error) {
	if ok, err := inGroup(conn, c, f, c.TeacherGroup); err != nil || ok {
		return auth.RoleTeacher, err
	}
	if c.StudentGroup == "" {
		return auth.RoleStudent, nil
	}
	ok, err := inGroup(conn, c, f, c.StudentGroup)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", auth.ErrDirectoryNotAllowed
	}
	return auth.RoleStudent, nil
}

// authenticate checks a sign-in name and password: find the entry with the
// service account, then bind as it with the password.
func authenticate(c Config, bindPassword, login, password string) (auth.DirectoryUser, error) {
	// An empty password is an "unauthenticated bind", which many servers,
	// Active Directory included, accept for any name.
	if password == "" || len(password) > 1024 || login == "" || len(login) > 256 || strings.ContainsRune(login+password, 0) {
		return auth.DirectoryUser{}, auth.ErrDirectoryCredentials
	}
	conn, err := connect(c)
	if err != nil {
		return auth.DirectoryUser{}, down(err)
	}
	defer conn.Close()
	if err := serviceBind(conn, c, bindPassword); err != nil {
		return auth.DirectoryUser{}, down(err)
	}
	f, err := find(conn, c, login)
	if err != nil {
		return auth.DirectoryUser{}, down(err)
	}
	if err := conn.Bind(f.DN, password); err != nil {
		if ldap.IsErrorWithCode(err, ldap.LDAPResultInvalidCredentials) {
			return auth.DirectoryUser{}, auth.ErrDirectoryCredentials
		}
		return auth.DirectoryUser{}, down(err)
	}
	// Back to the service account: an ordinary user may not read group membership.
	if err := serviceBind(conn, c, bindPassword); err != nil {
		return auth.DirectoryUser{}, down(err)
	}
	role, err := roleOf(conn, c, f)
	if err != nil {
		return auth.DirectoryUser{}, down(err)
	}
	if f.Email == "" {
		return auth.DirectoryUser{}, auth.ErrDirectoryNoEmail
	}
	return auth.DirectoryUser{ID: f.ID, DN: f.DN, Email: f.Email, Name: f.Name, Role: role, LinkByEmail: c.LinkByEmail}, nil
}

// down keeps the sign-in outcomes and turns anything else into "the
// directory can't be reached", without the details (they go to the admin's test).
func down(err error) error {
	for _, keep := range []error{auth.ErrDirectoryCredentials, auth.ErrDirectoryNotAllowed, auth.ErrDirectoryNoEmail} {
		if errors.Is(err, keep) {
			return err
		}
	}
	return fmt.Errorf("%w (%v)", auth.ErrDirectoryDown, err)
}

// stillAllowed is the hourly check: the entry still exists, isn't disabled
// (Active Directory), and its groups still give the same role.
func stillAllowed(conn *ldap.Conn, c Config, dn string, role auth.Role) (bool, string, error) {
	res, err := conn.Search(ldap.NewSearchRequest(dn, ldap.ScopeBaseObject, ldap.NeverDerefAliases, 1, int(opTimeout/time.Second), false, "(objectClass=*)", attrs(c), nil))
	if ldap.IsErrorWithCode(err, ldap.LDAPResultNoSuchObject) {
		return false, "removed from the directory", nil
	}
	if err != nil {
		return false, "", err
	}
	if len(res.Entries) == 0 {
		return false, "removed from the directory", nil
	}
	e := res.Entries[0]
	if uac, err := strconv.Atoi(e.GetAttributeValue("userAccountControl")); err == nil && uac&2 != 0 {
		return false, "disabled in the directory", nil
	}
	now, err := roleOf(conn, c, describeEntry(e, c, ""))
	if errors.Is(err, auth.ErrDirectoryNotAllowed) {
		return false, "no longer in the allowed groups", nil
	}
	if err != nil {
		return false, "", err
	}
	if now != role {
		return false, "role changed in the directory", nil
	}
	return true, "", nil
}
