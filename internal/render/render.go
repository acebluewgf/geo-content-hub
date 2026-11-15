package render

import (
	"bytes"
	"html"
	"regexp"
	"strings"

	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
)

var placeholder = regexp.MustCompile(`media:([0-9A-HJKMNP-TV-Z]{26})`)

var md = goldmark.New(goldmark.WithExtensions(extension.GFM))
var ugc = bluemonday.UGCPolicy()
var strict = bluemonday.StrictPolicy()

type Result struct {
	HTML    string
	Text    string
	Images  []string
	Chars   int
	Minutes int
}

// Render converts markdown, resolving media:{uid} placeholders to absolute URLs.
func Render(body string, resolve func(string) (string, bool)) Result {
	var images []string
	seen := map[string]bool{}
	body = placeholder.ReplaceAllStringFunc(body, func(m string) string {
		uid := placeholder.FindStringSubmatch(m)[1]
		if !seen[uid] {
			seen[uid] = true
			images = append(images, uid)
		}
		if u, ok := resolve(uid); ok {
			return u
		}
		return m
	})

	var buf bytes.Buffer
	if err := md.Convert([]byte(body), &buf); err != nil {
		return Result{HTML: "", Text: "", Images: images}
	}
	safe := ugc.SanitizeBytes(buf.Bytes())
	text := html.UnescapeString(strict.Sanitize(string(safe)))
	text = strings.Join(strings.Fields(text), " ")
	chars := len([]rune(text))
	minutes := (chars + 499) / 500
	if minutes < 1 {
		minutes = 1
	}
	return Result{HTML: string(safe), Text: text, Images: images, Chars: chars, Minutes: minutes}
}
