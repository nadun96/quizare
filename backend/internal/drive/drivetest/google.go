// Package drivetest is Google, as far as the Drive integration sees it, for
// tests: OAuth consent and tokens, and the Drive API calls the platform
// makes, over files kept in memory.
package drivetest

import (
	"crypto/rand"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/nadun96/quizplatform/internal/drive"
)

// Permission is one reader of a file.
type Permission struct {
	ID, Type, Email string
}

// File is a file in the fake Drive.
type File struct {
	drive.File
	Data        []byte
	Parent      string
	Permissions []Permission
}

// Google is a running fake.
type Google struct {
	Server       *httptest.Server
	ClientID     string
	ClientSecret string
	Email        string // whose Drive this is

	mu       sync.Mutex
	files    map[string]*File
	codes    map[string]bool
	access   map[string]bool // valid access tokens
	refresh  map[string]bool // refresh tokens issued; Google keeps earlier ones valid too
	Revoked  []string
	NoScope  bool            // the next consent leaves Drive unticked
	Refuse   bool            // refresh tokens stop working (revoked in the Google account)
	NoGoogle map[string]bool // emails without a Google account
}

// New starts a fake Google; it stops when the test ends.
func New(t testing.TB) *Google {
	g := &Google{ClientID: "drive-client", ClientSecret: "drive-secret", Email: "teacher@gmail.test",
		files: map[string]*File{}, codes: map[string]bool{}, access: map[string]bool{}, refresh: map[string]bool{}, NoGoogle: map[string]bool{}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /auth", g.consent)
	mux.HandleFunc("POST /token", g.token)
	mux.HandleFunc("POST /revoke", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		g.mu.Lock()
		g.Revoked = append(g.Revoked, r.PostForm.Get("token"))
		g.mu.Unlock()
	})
	mux.HandleFunc("/drive/v3/", g.authed(g.drive))
	mux.HandleFunc("POST /upload/drive/v3/files", g.authed(g.upload))
	g.Server = httptest.NewTLSServer(mux)
	t.Cleanup(g.Server.Close)
	return g
}

// Endpoints point the platform at this fake.
func (g *Google) Endpoints() drive.Endpoints {
	return drive.Endpoints{AuthURL: g.Server.URL + "/auth", TokenURL: g.Server.URL + "/token", RevokeURL: g.Server.URL + "/revoke", API: g.Server.URL}
}

// Add puts a file in the Drive, as if the teacher had it already.
func (g *Google) Add(name, mimeType string, data []byte) string {
	g.mu.Lock()
	defer g.mu.Unlock()
	id := "f" + strings.ToLower(rand.Text()[:12])
	g.files[id] = &File{File: drive.File{ID: id, Name: name, MimeType: mimeType, WebViewLink: "https://docs.google.test/" + id, IconLink: "https://icons.test/" + mimeType}, Data: data}
	return id
}

// File returns a copy of a file.
func (g *Google) File(id string) (File, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	f, ok := g.files[id]
	if !ok {
		return File{}, false
	}
	c := *f
	c.Permissions = append([]Permission(nil), f.Permissions...)
	return c, true
}

// Files lists the files whose parent is folder.
func (g *Google) Files(folder string) []File {
	g.mu.Lock()
	defer g.mu.Unlock()
	var out []File
	for _, f := range g.files {
		if f.Parent == folder {
			out = append(out, *f)
		}
	}
	return out
}

// Delete removes a file, as the teacher might in Drive.
func (g *Google) Delete(id string) {
	g.mu.Lock()
	delete(g.files, id)
	g.mu.Unlock()
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func apiErr(w http.ResponseWriter, status int, reason, msg string) {
	writeJSON(w, status, map[string]any{"error": map[string]any{"code": status, "message": msg, "errors": []map[string]string{{"reason": reason}}}})
}

func (g *Google) consent(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if q.Get("client_id") != g.ClientID || q.Get("access_type") != "offline" || q.Get("code_challenge") == "" || !strings.Contains(q.Get("scope"), drive.Scope) {
		http.Error(w, "bad consent request", http.StatusBadRequest)
		return
	}
	back, _ := url.Parse(q.Get("redirect_uri"))
	v := back.Query()
	v.Set("state", q.Get("state"))
	code := rand.Text()
	g.mu.Lock()
	g.codes[code] = true
	g.mu.Unlock()
	v.Set("code", code)
	back.RawQuery = v.Encode()
	http.Redirect(w, r, back.String(), http.StatusFound)
}

func (g *Google) token(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	if r.PostForm.Get("client_id") != g.ClientID || r.PostForm.Get("client_secret") != g.ClientSecret {
		writeJSON(w, 401, map[string]string{"error": "invalid_client"})
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	scope := drive.Scope
	if g.NoScope {
		scope = "openid"
	}
	switch r.PostForm.Get("grant_type") {
	case "authorization_code":
		if !g.codes[r.PostForm.Get("code")] || r.PostForm.Get("code_verifier") == "" {
			writeJSON(w, 400, map[string]string{"error": "invalid_grant"})
			return
		}
		delete(g.codes, r.PostForm.Get("code"))
		refresh := "refresh-" + rand.Text()
		g.refresh[refresh] = true
		access := "access-" + rand.Text()
		g.access[access] = true
		writeJSON(w, 200, map[string]any{"access_token": access, "token_type": "Bearer", "expires_in": 3599, "refresh_token": refresh, "scope": scope})
	case "refresh_token":
		if g.Refuse || !g.refresh[r.PostForm.Get("refresh_token")] {
			writeJSON(w, 400, map[string]string{"error": "invalid_grant", "error_description": "Token has been expired or revoked."})
			return
		}
		access := "access-" + rand.Text()
		g.access[access] = true
		writeJSON(w, 200, map[string]any{"access_token": access, "token_type": "Bearer", "expires_in": 3599, "scope": scope})
	default:
		writeJSON(w, 400, map[string]string{"error": "unsupported_grant_type"})
	}
}

// Expire makes every access token stop working, so the next call refreshes.
func (g *Google) Expire() {
	g.mu.Lock()
	g.access = map[string]bool{}
	g.mu.Unlock()
}

func (g *Google) authed(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		g.mu.Lock()
		ok := g.access[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")]
		g.mu.Unlock()
		if !ok {
			apiErr(w, 401, "authError", "Invalid Credentials")
			return
		}
		h(w, r)
	}
}

func (g *Google) drive(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/drive/v3/")
	parts := strings.Split(path, "/")
	g.mu.Lock()
	defer g.mu.Unlock()
	switch {
	case path == "about":
		writeJSON(w, 200, map[string]any{"user": map[string]string{"emailAddress": g.Email}})
	case path == "files" && r.Method == "POST":
		var meta struct {
			Name, MimeType string
		}
		_ = json.NewDecoder(r.Body).Decode(&meta)
		id := "d" + strings.ToLower(rand.Text()[:12])
		g.files[id] = &File{File: drive.File{ID: id, Name: meta.Name, MimeType: meta.MimeType, WebViewLink: "https://drive.google.test/folders/" + id}}
		writeJSON(w, 200, g.files[id].File)
	case len(parts) >= 2 && parts[0] == "files":
		f := g.files[parts[1]]
		if f == nil {
			apiErr(w, 404, "notFound", "File not found: "+parts[1])
			return
		}
		switch {
		case len(parts) == 2 && r.URL.Query().Get("alt") == "media":
			_, _ = w.Write(f.Data)
		case len(parts) == 2:
			writeJSON(w, 200, f.File)
		case parts[2] == "export":
			if f.MimeType != drive.MimeSheet || r.URL.Query().Get("mimeType") != "text/csv" {
				apiErr(w, 400, "badRequest", "Export only supports Docs Editors files.")
				return
			}
			_, _ = w.Write(f.Data)
		case parts[2] == "permissions" && r.Method == "POST":
			var p struct {
				Type, Role, EmailAddress string
			}
			_ = json.NewDecoder(r.Body).Decode(&p)
			if p.Type == "user" && (g.NoGoogle[p.EmailAddress] && r.URL.Query().Get("sendNotificationEmail") == "false") {
				apiErr(w, 400, "invalidSharingRequest", "Bad Request. User message: \"You are trying to invite "+p.EmailAddress+". Since there is no Google account associated with this email address...\"")
				return
			}
			perm := Permission{ID: "p" + strings.ToLower(rand.Text()[:8]), Type: p.Type, Email: p.EmailAddress}
			f.Permissions = append(f.Permissions, perm)
			writeJSON(w, 200, map[string]string{"id": perm.ID})
		case parts[2] == "permissions" && r.Method == "DELETE" && len(parts) == 4:
			for i, p := range f.Permissions {
				if p.ID == parts[3] {
					f.Permissions = append(f.Permissions[:i], f.Permissions[i+1:]...)
					w.WriteHeader(204)
					return
				}
			}
			apiErr(w, 404, "notFound", "Permission not found")
		default:
			apiErr(w, 400, "badRequest", "not in this fake")
		}
	default:
		apiErr(w, 400, "badRequest", "not in this fake")
	}
}

func (g *Google) upload(w http.ResponseWriter, r *http.Request) {
	mt, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mt != "multipart/related" || r.URL.Query().Get("uploadType") != "multipart" {
		apiErr(w, 400, "badRequest", "multipart/related uploads only")
		return
	}
	mr := multipart.NewReader(r.Body, params["boundary"])
	var meta struct {
		Name     string   `json:"name"`
		MimeType string   `json:"mimeType"`
		Parents  []string `json:"parents"`
	}
	part, err := mr.NextPart()
	if err != nil {
		apiErr(w, 400, "badRequest", "no metadata")
		return
	}
	_ = json.NewDecoder(part).Decode(&meta)
	part, err = mr.NextPart()
	if err != nil {
		apiErr(w, 400, "badRequest", "no media")
		return
	}
	data, _ := io.ReadAll(part)
	g.mu.Lock()
	defer g.mu.Unlock()
	parent := ""
	if len(meta.Parents) > 0 {
		parent = meta.Parents[0]
		if g.files[parent] == nil {
			apiErr(w, 404, "notFound", "File not found: "+parent)
			return
		}
	}
	id := "u" + strings.ToLower(rand.Text()[:12])
	g.files[id] = &File{File: drive.File{ID: id, Name: meta.Name, MimeType: meta.MimeType, WebViewLink: "https://docs.google.test/spreadsheets/" + id}, Data: data, Parent: parent}
	writeJSON(w, 200, g.files[id].File)
}
