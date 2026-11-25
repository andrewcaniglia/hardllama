//go:build windows || darwin

package ui

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/ledongthuc/pdf"
)

// convertBytesToText converts raw file bytes to text based on file extension
func convertBytesToText(data []byte, filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))

	if ext == ".pdf" {
		text, err := extractPDFText(data)
		if err != nil {
			return fmt.Sprintf("[PDF file - %d bytes - failed to extract text: %v]", len(data), err)
		}
		if strings.TrimSpace(text) == "" {
			return fmt.Sprintf("[PDF file - %d bytes - no text content found]", len(data))
		}
		return text
	}

	binaryExtensions := []string{
		".xlsx", ".pptx", ".zip", ".tar", ".gz", ".rar",
		".jpg", ".jpeg", ".png", ".gif", ".bmp", ".svg", ".ico",
		".mp3", ".mp4", ".avi", ".mov", ".wmv", ".flv", ".webm",
		".exe", ".dll", ".so", ".dylib", ".app", ".dmg", ".pkg",
	}

	if slices.Contains(binaryExtensions, ext) {
		return fmt.Sprintf("[Binary file of type %s - %d bytes]", ext, len(data))
	}

	if utf8.Valid(data) {
		return string(data)
	}

	// If not valid UTF-8, return a placeholder
	return fmt.Sprintf("[Binary file - %d bytes - not valid UTF-8]", len(data))
}

// extractPDFText extracts text content from PDF bytes
func extractPDFText(data []byte) (string, error) {
	return readPDFText(context.Background(), data, 0)
}

func readPDFText(ctx context.Context, data []byte, maxText int) (result string, err error) {
	// Some malformed PDF objects cause the parser to panic.
	defer func() {
		if recover() != nil {
			result = ""
			err = errors.New("invalid PDF")
		}
	}()
	reader := bytes.NewReader(data)
	pdfReader, err := pdf.NewReader(reader, int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("failed to create PDF reader: %w", err)
	}

	var textBuilder strings.Builder
	numPages := pdfReader.NumPage()

	for i := 1; i <= numPages; i++ {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		page := pdfReader.Page(i)
		if page.V.IsNull() {
			continue
		}

		text, err := page.GetPlainText(nil)
		if err != nil {
			// Log the error but continue with other pages
			continue
		}

		if strings.TrimSpace(text) != "" {
			if textBuilder.Len() > 0 {
				textBuilder.WriteString("\n\n--- Page ")
				textBuilder.WriteString(fmt.Sprintf("%d", i))
				textBuilder.WriteString(" ---\n")
			}
			if maxText > 0 && len(text) > maxText-textBuilder.Len() {
				return "", errors.New("extracted text exceeds folder limit")
			}
			textBuilder.WriteString(text)
		}
	}

	return textBuilder.String(), nil
}

// extractFolderPDF runs on the desktop app, including when inference is paired.
func (s *Server) extractFolderPDF(w http.ResponseWriter, r *http.Request) error {
	if ok, err := s.requireUnlocked(w); !ok {
		return err
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 10<<20))
	if err != nil {
		return writeError(w, http.StatusBadRequest, errors.New("could not read PDF (10 MiB maximum)"))
	}
	text, err := readPDFText(r.Context(), data, 24<<20)
	if err != nil {
		return writeError(w, http.StatusUnprocessableEntity, errors.New("could not extract PDF text within the folder limit"))
	}
	if ok, err := s.requireUnlocked(w); !ok {
		return err
	}
	return writeJSON(w, http.StatusOK, map[string]string{"text": text})
}
