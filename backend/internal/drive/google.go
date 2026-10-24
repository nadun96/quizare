package drive

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"strings"
)

// The few Drive API calls the platform makes, over an HTTP client that adds
// the teacher's access token. Google's own SDK is far larger than this needs.

// Endpoints are Google's addresses; tests point them at a fake.
type Endpoints struct {
	AuthURL   string // OAuth consent
	TokenURL  string
	RevokeURL string
	API       string // https://www.googleapis.com: /drive/v3 and /upload/drive/v3
}

// Google is where the platform talks to Google.
var Google = Endpoints{
	AuthURL:   "https://accounts.google.com/o/oauth2/v2/auth",
	TokenURL:  "https://oauth2.googleapis.com/token",
	RevokeURL: "https://oauth2.googleapis.com/revoke",
	API:       "https://www.googleapis.com",
}

// Scope: only files the teacher picks or the platform makes, never the whole Drive.
const Scope = "https://www.googleapis.com/auth/drive.file"

const (
	MimeFolder = "application/vnd.google-apps.folder"
	MimeSheet  = "application/vnd.google-apps.spreadsheet"
)

// File is a Drive file's metadata.
type File struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	MimeType     string `json:"mimeType"`
	WebViewLink  string `json:"webViewLink"`
	IconLink     string `json:"iconLink"`
	Size         string `json:"size"`
	Trashed      bool   `json:"trashed"`
	ThumbnailURL string `json:"thumbnailLink"`
}

const fileFields = "id,name,mimeType,webViewLink,iconLink,size,trashed,thumbnailLink"

// apiError is what Google answered when it said no.
type apiError struct {
	Status  int
	Reason  string
	Message string
}

func (e *apiError) Error() string {
	return fmt.Sprintf("google drive: %d %s: %s", e.Status, e.Reason, e.Message)
}

func isStatus(err error, status int) bool {
	var ae *apiError
	return errors.As(err, &ae) && ae.Status == status
}

type api struct {
	hc   *http.Client // carries the teacher's token
	base string
}

func (a api) do(ctx context.Context, method, path string, query url.Values, body io.Reader, contentType string, out any) error {
	u := a.base + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := a.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		var e struct {
			Error struct {
				Message string `json:"message"`
				Errors  []struct {
					Reason string `json:"reason"`
				} `json:"errors"`
			} `json:"error"`
		}
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		_ = json.Unmarshal(raw, &e)
		ae := &apiError{Status: resp.StatusCode, Message: e.Error.Message}
		if len(e.Error.Errors) > 0 {
			ae.Reason = e.Error.Errors[0].Reason
		}
		return ae
	}
	if out == nil {
		return nil
	}
	if w, ok := out.(io.Writer); ok {
		_, err = io.Copy(w, resp.Body)
		return err
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (a api) about(ctx context.Context) (string, error) {
	var out struct {
		User struct {
			Email string `json:"emailAddress"`
		} `json:"user"`
	}
	err := a.do(ctx, "GET", "/drive/v3/about", url.Values{"fields": {"user(emailAddress)"}}, nil, "", &out)
	return out.User.Email, err
}

func (a api) file(ctx context.Context, id string) (File, error) {
	var f File
	err := a.do(ctx, "GET", "/drive/v3/files/"+url.PathEscape(id), url.Values{"fields": {fileFields}, "supportsAllDrives": {"true"}}, nil, "", &f)
	return f, err
}

// limitedWriter fails once more than n bytes arrive.
type limitedWriter struct {
	buf bytes.Buffer
	n   int64
}

var errTooBig = errors.New("the file is too large")

func (w *limitedWriter) Write(p []byte) (int, error) {
	if int64(w.buf.Len()+len(p)) > w.n {
		return 0, errTooBig
	}
	return w.buf.Write(p)
}

// content downloads a file, or exports a Google document in mimeType.
func (a api) content(ctx context.Context, id, exportAs string, max int64) ([]byte, error) {
	w := &limitedWriter{n: max}
	var err error
	if exportAs != "" {
		err = a.do(ctx, "GET", "/drive/v3/files/"+url.PathEscape(id)+"/export", url.Values{"mimeType": {exportAs}}, nil, "", w)
	} else {
		err = a.do(ctx, "GET", "/drive/v3/files/"+url.PathEscape(id), url.Values{"alt": {"media"}, "supportsAllDrives": {"true"}}, nil, "", w)
	}
	return w.buf.Bytes(), err
}

func (a api) createFolder(ctx context.Context, name string) (File, error) {
	body, _ := json.Marshal(map[string]any{"name": name, "mimeType": MimeFolder})
	var f File
	err := a.do(ctx, "POST", "/drive/v3/files", url.Values{"fields": {fileFields}}, bytes.NewReader(body), "application/json", &f)
	return f, err
}

// upload stores data as a new file; with convertTo, Google converts it
// (a CSV becomes a Google Sheet).
func (a api) upload(ctx context.Context, name, parent, mimeType, convertTo string, data []byte) (File, error) {
	meta := map[string]any{"name": name}
	if parent != "" {
		meta["parents"] = []string{parent}
	}
	if convertTo != "" {
		meta["mimeType"] = convertTo
	}
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	h := textproto.MIMEHeader{"Content-Type": {"application/json; charset=UTF-8"}}
	pw, _ := mw.CreatePart(h)
	_ = json.NewEncoder(pw).Encode(meta)
	pw, _ = mw.CreatePart(textproto.MIMEHeader{"Content-Type": {mimeType}})
	_, _ = pw.Write(data)
	_ = mw.Close()
	var f File
	err := a.do(ctx, "POST", "/upload/drive/v3/files", url.Values{"uploadType": {"multipart"}, "fields": {fileFields}},
		&buf, "multipart/related; boundary="+mw.Boundary(), &f)
	return f, err
}

// share adds a reader: an email, or anyone with the link (email "").
func (a api) share(ctx context.Context, fileID, email string) (string, error) {
	perm := map[string]any{"role": "reader", "type": "anyone"}
	if email != "" {
		perm = map[string]any{"role": "reader", "type": "user", "emailAddress": email}
	}
	body, _ := json.Marshal(perm)
	var out struct {
		ID string `json:"id"`
	}
	q := url.Values{"fields": {"id"}, "supportsAllDrives": {"true"}}
	if email != "" {
		q.Set("sendNotificationEmail", "false") // the classroom page is the notice
	}
	err := a.do(ctx, "POST", "/drive/v3/files/"+url.PathEscape(fileID)+"/permissions", q, bytes.NewReader(body), "application/json", &out)
	return out.ID, err
}

func (a api) unshare(ctx context.Context, fileID, permissionID string) error {
	err := a.do(ctx, "DELETE", "/drive/v3/files/"+url.PathEscape(fileID)+"/permissions/"+url.PathEscape(permissionID), url.Values{"supportsAllDrives": {"true"}}, nil, "", nil)
	if isStatus(err, http.StatusNotFound) {
		return nil // gone already
	}
	return err
}

// revoke tells Google the platform no longer holds this token.
func revoke(ctx context.Context, hc *http.Client, endpoint, token string) error {
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, strings.NewReader(url.Values{"token": {token}}.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}
