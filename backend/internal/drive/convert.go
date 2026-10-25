package drive

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"

	"github.com/nadun96/quizplatform/internal/platform/httpx"
)

// PowerPoint to PDF through the teacher's Google Drive (D-78, PR-FR-02,
// PR-FR-03): the file goes into their Drive as Google Slides, comes back as
// a PDF, and the copy is deleted. The browser then turns the PDF into slides
// as for any PDF, so the server needs no office software.

const (
	MimeSlides = "application/vnd.google-apps.presentation"
	MimePDF    = "application/pdf"

	MaxDeckBytes = 100 << 20 // a PowerPoint file in
	MaxPDFBytes  = 50 << 20  // a PDF out, as presentations take
)

// What Google converts to Slides, by extension.
var slideTypes = map[string]string{
	".pptx": "application/vnd.openxmlformats-officedocument.presentationml.presentation",
	".ppt":  "application/vnd.ms-powerpoint",
	".odp":  "application/vnd.oasis.opendocument.presentation",
	".key":  "application/x-iwork-keynote-sffkey",
}

var (
	errNotSlides = httpx.Invalid(map[string]string{"file": "a PowerPoint (.pptx or .ppt), OpenDocument (.odp) or Keynote file, Google Slides, or a PDF"})
	errPDFTooBig = httpx.Invalid(map[string]string{"file": "the PDF comes to more than 50 MB; make the pictures in it smaller and try again"})
)

// exportPDF exports a Google Slides file as a PDF. Drive's export answers
// up to 10 MB; bigger decks come through the file's export link instead.
func (a api) exportPDF(ctx context.Context, id string) ([]byte, error) {
	data, err := a.content(ctx, id, MimePDF, MaxPDFBytes)
	var ae *apiError
	if !errors.As(err, &ae) || ae.Reason != "exportSizeLimitExceeded" {
		return data, err
	}
	var f struct {
		ExportLinks map[string]string `json:"exportLinks"`
	}
	if err := a.do(ctx, "GET", "/drive/v3/files/"+url.PathEscape(id), url.Values{"fields": {"exportLinks"}}, nil, "", &f); err != nil {
		return nil, err
	}
	link := f.ExportLinks[MimePDF]
	if link == "" {
		return nil, err
	}
	req, rerr := http.NewRequestWithContext(ctx, "GET", link, nil)
	if rerr != nil {
		return nil, rerr
	}
	resp, rerr := a.hc.Do(req)
	if rerr != nil {
		return nil, rerr
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, &apiError{Status: resp.StatusCode, Reason: "exportFailed", Message: "the export link answered " + resp.Status}
	}
	data, rerr = io.ReadAll(io.LimitReader(resp.Body, MaxPDFBytes+1))
	if rerr != nil {
		return nil, rerr
	}
	if len(data) > MaxPDFBytes {
		return nil, errTooBig
	}
	return data, nil
}

func (a api) remove(ctx context.Context, id string) error {
	err := a.do(ctx, "DELETE", "/drive/v3/files/"+url.PathEscape(id), url.Values{"supportsAllDrives": {"true"}}, nil, "", nil)
	if isStatus(err, http.StatusNotFound) {
		return nil
	}
	return err
}

func pdfResult(data []byte, err error) ([]byte, error) {
	switch {
	case errors.Is(err, errTooBig):
		return nil, errPDFTooBig
	case err != nil:
		return nil, googleErr(err)
	case !bytes.HasPrefix(data, []byte("%PDF-")):
		return nil, httpx.NewError(http.StatusBadGateway, "drive_convert_failed", "Google Drive couldn't turn this file into a PDF")
	}
	return data, nil
}

// SlidesToPDF converts an uploaded presentation file to a PDF through the
// teacher's Drive, deleting the copy it made there.
func (s *Service) SlidesToPDF(ctx context.Context, teacherID, name string, data []byte) ([]byte, error) {
	mimeType, ok := slideTypes[strings.ToLower(path.Ext(name))]
	if !ok {
		return nil, errNotSlides
	}
	a, err := s.api(ctx, teacherID)
	if err != nil {
		return nil, err
	}
	f, err := a.upload(ctx, strings.TrimSuffix(path.Base(name), path.Ext(name))+" (converting for Classroom Quiz)", "", mimeType, MimeSlides, data)
	if err != nil {
		return nil, googleErr(err)
	}
	defer func() {
		if err := a.remove(context.WithoutCancel(ctx), f.ID); err != nil {
			s.log.Warn("removing a converted presentation from Drive", "err", err)
		}
	}()
	return pdfResult(a.exportPDF(ctx, f.ID))
}

// DrivePDF fetches a presentation picked in Drive as a PDF: Google Slides
// exported, a PDF as it is, or a PowerPoint file converted.
func (s *Service) DrivePDF(ctx context.Context, teacherID, fileID string) ([]byte, string, error) {
	a, err := s.api(ctx, teacherID)
	if err != nil {
		return nil, "", err
	}
	f, err := a.file(ctx, fileID)
	if err != nil {
		return nil, "", googleErr(err)
	}
	name := strings.TrimSuffix(f.Name, path.Ext(f.Name)) + ".pdf"
	switch {
	case f.MimeType == MimeSlides:
		data, err := pdfResult(a.exportPDF(ctx, f.ID))
		return data, name, err
	case f.MimeType == MimePDF:
		data, err := pdfResult(a.content(ctx, f.ID, "", MaxPDFBytes))
		return data, name, err
	}
	if _, ok := slideTypes[strings.ToLower(path.Ext(f.Name))]; !ok {
		return nil, "", errNotSlides
	}
	data, err := a.content(ctx, f.ID, "", MaxDeckBytes)
	if errors.Is(err, errTooBig) {
		return nil, "", httpx.Invalid(map[string]string{"file": "a presentation of at most 100 MB"})
	}
	if err != nil {
		return nil, "", googleErr(err)
	}
	out, err := s.SlidesToPDF(ctx, teacherID, f.Name, data)
	return out, name, err
}
