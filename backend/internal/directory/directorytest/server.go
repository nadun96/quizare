// Package directorytest runs a small LDAP server for tests (D-64): simple
// binds checked against entries' passwords, and searches that really
// evaluate filters (and, or, not, equality, presence, substrings, and Active
// Directory's nested-group rule) over the entries' attributes. It speaks
// LDAPS, or StartTLS on a plain port, with a certificate from its own CA.
package directorytest

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	ber "github.com/go-asn1-ber/asn1-ber"
	"github.com/go-ldap/ldap/v3"
	"github.com/jimlambrt/gldap"
)

// InChain is Active Directory's LDAP_MATCHING_RULE_IN_CHAIN: membership
// through nested groups.
const InChain = "1.2.840.113556.1.4.1941"

// Entry is one directory object. Attribute names are compared without case.
type Entry struct {
	DN       string
	Password string // for binds; empty = cannot bind
	Attrs    map[string][]string
}

type Server struct {
	// URL is ldaps://127.0.0.1:port, or ldap://… when started with StartTLS.
	URL string
	// CAPEM is the certificate authority that signed the server's certificate.
	CAPEM string

	mu       sync.Mutex
	entries  []*Entry
	binds    int
	searches []string
	srv      *gldap.Server
	tlsCfg   *tls.Config
}

// Start runs a server until the test ends. startTLS: plain port with
// StartTLS instead of LDAPS.
func Start(t testing.TB, startTLS bool, entries ...*Entry) *Server {
	t.Helper()
	caPEM, serverCfg := certs(t)
	s := &Server{CAPEM: caPEM, entries: entries, tlsCfg: serverCfg}
	srv, err := gldap.NewServer(gldap.WithReadTimeout(5*time.Second), gldap.WithWriteTimeout(5*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	mux, _ := gldap.NewMux()
	_ = mux.Bind(s.bind)
	_ = mux.Search(s.search)
	_ = mux.ExtendedOperation(s.startTLS, gldap.ExtendedOperationStartTLS)
	_ = srv.Router(mux)
	s.srv = srv

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	opts := []gldap.Option{}
	if startTLS {
		s.URL = "ldap://" + addr
	} else {
		s.URL = "ldaps://" + addr
		opts = append(opts, gldap.WithTLSConfig(serverCfg))
	}
	go func() { _ = srv.Run(addr, opts...) }()
	deadline := time.Now().Add(5 * time.Second)
	for !srv.Ready() {
		if time.Now().After(deadline) {
			t.Fatal("test directory did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Cleanup(func() { _ = srv.Stop() })
	return s
}

// Set replaces an entry's attribute (e.g. to disable an account mid-test).
func (s *Server) Set(dn, attr string, values ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range s.entries {
		if strings.EqualFold(e.DN, dn) {
			e.Attrs[attr] = values
		}
	}
}

// Remove deletes an entry.
func (s *Server) Remove(dn string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := s.entries[:0]
	for _, e := range s.entries {
		if !strings.EqualFold(e.DN, dn) {
			kept = append(kept, e)
		}
	}
	s.entries = kept
}

// Searches returns the filters received so far.
func (s *Server) Searches() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.searches...)
}

func (s *Server) bind(w *gldap.ResponseWriter, r *gldap.Request) {
	resp := r.NewBindResponse(gldap.WithResponseCode(gldap.ResultInvalidCredentials))
	defer func() { _ = w.Write(resp) }()
	m, err := r.GetSimpleBindMessage()
	if err != nil || m.AuthChoice != gldap.SimpleAuthChoice {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.binds++
	for _, e := range s.entries {
		if strings.EqualFold(e.DN, m.UserName) && e.Password != "" && string(m.Password) == e.Password {
			resp.SetResultCode(gldap.ResultSuccess)
			return
		}
	}
}

func (s *Server) startTLS(w *gldap.ResponseWriter, r *gldap.Request) {
	res := r.NewExtendedResponse(gldap.WithResponseCode(gldap.ResultSuccess))
	res.SetResponseName(gldap.ExtendedOperationStartTLS)
	if err := w.Write(res); err != nil {
		return
	}
	_ = r.StartTLS(s.tlsCfg)
}

func (s *Server) search(w *gldap.ResponseWriter, r *gldap.Request) {
	done := r.NewSearchDoneResponse(gldap.WithResponseCode(gldap.ResultSuccess))
	defer func() { _ = w.Write(done) }()
	m, err := r.GetSearchMessage()
	if err != nil {
		done.SetResultCode(gldap.ResultProtocolError)
		return
	}
	f, err := ldap.CompileFilter(m.Filter)
	if err != nil {
		done.SetResultCode(gldap.ResultProtocolError)
		return
	}
	s.mu.Lock()
	s.searches = append(s.searches, m.Filter)
	entries := append([]*Entry(nil), s.entries...)
	s.mu.Unlock()

	base := strings.ToLower(m.BaseDN)
	found := 0
	for _, e := range entries {
		dn := strings.ToLower(e.DN)
		inScope := dn == base
		if m.Scope != gldap.BaseObject {
			inScope = inScope || strings.HasSuffix(dn, ","+base)
		}
		if !inScope || !s.match(f, e, entries) {
			continue
		}
		if m.SizeLimit > 0 && int64(found) >= m.SizeLimit {
			done.SetResultCode(gldap.ResultSizeLimitExceeded)
			return
		}
		found++
		res := r.NewSearchResponseEntry(e.DN)
		for k, v := range e.Attrs {
			res.AddAttribute(k, v)
		}
		if err := w.Write(res); err != nil {
			return
		}
	}
	if found == 0 && m.Scope == gldap.BaseObject {
		done.SetResultCode(gldap.ResultNoSuchObject)
	}
}

func attr(e *Entry, name string) []string {
	if strings.EqualFold(name, "distinguishedName") {
		return []string{e.DN}
	}
	for k, v := range e.Attrs {
		if strings.EqualFold(k, name) {
			return v
		}
	}
	return nil
}

// member reports whether dn is in group g, directly or (inChain) through
// groups inside it, by the groups' "member" attributes and the entry's "memberOf".
func member(dn, group string, entries []*Entry, inChain bool, seen map[string]bool) bool {
	g := strings.ToLower(group)
	if seen[g] {
		return false
	}
	seen[g] = true
	for _, e := range entries {
		if !strings.EqualFold(e.DN, group) {
			continue
		}
		for _, m := range attr(e, "member") {
			if strings.EqualFold(m, dn) || (inChain && member(dn, m, entries, true, seen)) {
				return true
			}
		}
	}
	return false
}

func (s *Server) match(p *ber.Packet, e *Entry, all []*Entry) bool {
	switch p.Tag {
	case ldap.FilterAnd:
		for _, c := range p.Children {
			if !s.match(c, e, all) {
				return false
			}
		}
		return true
	case ldap.FilterOr:
		for _, c := range p.Children {
			if s.match(c, e, all) {
				return true
			}
		}
		return false
	case ldap.FilterNot:
		return !s.match(p.Children[0], e, all)
	case ldap.FilterPresent:
		return len(attr(e, p.Data.String())) > 0 || strings.EqualFold(p.Data.String(), "objectClass")
	case ldap.FilterEqualityMatch:
		name, want := p.Children[0].Data.String(), p.Children[1].Data.String()
		for _, v := range attr(e, name) {
			if strings.EqualFold(v, want) {
				return true
			}
		}
		// memberOf also holds for groups that list the entry as a member.
		return strings.EqualFold(name, "memberOf") && member(e.DN, want, all, false, map[string]bool{})
	case ldap.FilterSubstrings:
		name := p.Children[0].Data.String()
		var parts []string
		for _, c := range p.Children[1].Children {
			parts = append(parts, strings.ToLower(c.Data.String()))
		}
		for _, v := range attr(e, name) {
			if strings.Contains(strings.ToLower(v), strings.Join(parts, "")) {
				return true
			}
		}
		return false
	case ldap.FilterExtensibleMatch:
		var rule, name, value string
		for _, c := range p.Children {
			switch c.Tag {
			case ldap.MatchingRuleAssertionMatchingRule:
				rule = c.Data.String()
			case ldap.MatchingRuleAssertionType:
				name = c.Data.String()
			case ldap.MatchingRuleAssertionMatchValue:
				value = c.Data.String()
			}
		}
		if rule == InChain && strings.EqualFold(name, "memberOf") {
			return member(e.DN, value, all, true, map[string]bool{})
		}
		// The bitwise AND rule AD uses for disabled accounts (userAccountControl & 2).
		if rule == "1.2.840.113556.1.4.803" {
			var want, have int
			fmt.Sscan(value, &want)
			for _, v := range attr(e, name) {
				fmt.Sscan(v, &have)
				if have&want == want {
					return true
				}
			}
		}
		return false
	}
	return false
}

// certs makes a CA and a certificate for 127.0.0.1 and localhost.
func certs(t testing.TB) (string, *tls.Config) {
	t.Helper()
	caKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	ca := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Test directory CA"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, ca, ca, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	caCert, _ := x509.ParseCertificate(caDER)
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	leaf := &x509.Certificate{
		SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "localhost"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour),
		DNSNames: []string{"localhost"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, KeyUsage: x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, leaf, caCert, &key.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})),
		&tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}}
}
