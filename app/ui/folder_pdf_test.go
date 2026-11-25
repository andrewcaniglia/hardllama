//go:build darwin || windows

package ui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ollama/ollama/app/store"
)

func TestFolderPDFExtraction(t *testing.T) {
	var doc bytes.Buffer
	doc.WriteString("%PDF-1.4\n")
	stream := "BT /F1 12 Tf 72 700 Td (Nested handbook) Tj ET"
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream),
	}
	offsets := []int{0}
	for i, obj := range objects {
		offsets = append(offsets, doc.Len())
		fmt.Fprintf(&doc, "%d 0 obj\n%s\nendobj\n", i+1, obj)
	}
	xref := doc.Len()
	fmt.Fprintf(&doc, "xref\n0 %d\n0000000000 65535 f \n", len(offsets))
	for _, pos := range offsets[1:] {
		fmt.Fprintf(&doc, "%010d 00000 n \n", pos)
	}
	fmt.Fprintf(&doc, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets), xref)
	s := Server{Store: &store.Store{DBPath: filepath.Join(t.TempDir(), "db.sqlite")}, Dev: true}
	defer s.Store.Close()
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest("POST", "/api/v1/folder-pdf", &doc))
	var response struct{ Text string }
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &response) != nil || !strings.Contains(response.Text, "Nested handbook") {
		t.Fatalf("PDF extraction: %d %q", rec.Code, rec.Body.String())
	}
}
