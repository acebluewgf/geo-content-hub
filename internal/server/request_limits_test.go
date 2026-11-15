package server

import (
	"bytes"
	"mime/multipart"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMultipartTotalBodyIsLimited(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	file, _ := writer.CreateFormFile("file", "small.png")
	file.Write([]byte("small"))
	extra, _ := writer.CreateFormFile("ignored", "large.bin")
	extra.Write(bytes.Repeat([]byte("a"), 2<<20))
	writer.Close()
	r := httptest.NewRequest("POST", "/media", &body)
	r.Header.Set("Content-Type", writer.FormDataContentType())
	if _, err := readUpload(r, 1024); err == nil {
		t.Fatal("ignored multipart part bypassed total-body limit")
	}
}
func TestJSONRejectsExtraValuesAndOversizedBody(t *testing.T) {
	for _, body := range []string{`{} {}`, `{} trailing`, strings.Repeat(" ", (2<<20)+1) + `{}`} {
		r := httptest.NewRequest("POST", "/test", strings.NewReader(body))
		var value map[string]any
		if err := decodeJSON(r, &value); err == nil {
			t.Fatal("invalid JSON body accepted")
		}
	}
}
