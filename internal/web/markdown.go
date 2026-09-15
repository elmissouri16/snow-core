package web

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"html"
	"html/template"
	"net/url"
	"slices"
	"strings"
	"sync"

	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
)

const markdownHTMLLimit = 128 << 10
const markdownCacheLimit = 1 << 20

// Only explicit links can leave the manager. No images, HTML, styles, forms,
// embedded objects or remotely loaded content are admitted. The sanitizer is
// immutable after construction; only sanitized output crosses the HTML boundary.
var markdownPolicy = func() *bluemonday.Policy {
	p := bluemonday.NewPolicy()
	p.AllowElements("p", "br", "hr", "strong", "em", "del", "blockquote", "pre", "code", "h1", "h2", "h3", "h4", "h5", "h6", "ul", "ol", "li", "table", "thead", "tbody", "tr", "th", "td", "a")
	p.AllowAttrs("href").OnElements("a")
	p.RequireParseableURLs(true).AllowRelativeURLs(false)
	for _, scheme := range []string{"http", "https"} {
		p.AllowURLSchemeWithCustomPolicy(scheme, func(u *url.URL) bool { return u.Host != "" && u.User == nil })
	}
	p.RequireNoFollowOnLinks(true).RequireNoReferrerOnLinks(true)
	return p
}()

var markdownCache = struct {
	sync.Mutex
	values map[[32]byte]string
	order  [][32]byte
	bytes  int
}{values: make(map[[32]byte]string)}

type markdownOutput struct{ buf bytes.Buffer }

func (b *markdownOutput) Write(p []byte) (int, error) {
	if len(p) > markdownHTMLLimit-b.buf.Len() {
		return 0, errors.New("Markdown display limit")
	}
	return b.buf.Write(p)
}

// markdownHTML renders bounded public assistant/plan text, never provider-private
// blocks. Raw HTML and unsafe URLs stay disabled in goldmark; an independent
// explicit allowlist also strips images and every active HTML capability.
func markdownHTML(text string) template.HTML {
	text = runtimeText(text, runtimeMessageBytes)
	key := sha256.Sum256([]byte(text))
	markdownCache.Lock()
	cached, ok := markdownCache.values[key]
	markdownCache.Unlock()
	if ok {
		return template.HTML(cached)
	} // Sanitized before cache insertion.
	var out markdownOutput
	md := goldmark.New(goldmark.WithExtensions(extension.GFM))
	var rendered string
	if err := md.Convert([]byte(text), &out); err == nil {
		rendered = markdownPolicy.Sanitize(out.buf.String())
	}
	if strings.TrimSpace(rendered) == "" && strings.TrimSpace(text) != "" || len(rendered) > markdownHTMLLimit {
		clipped := runtimeText(text, 16<<10)
		if clipped != text {
			clipped += "\n[Display truncated]"
		}
		rendered = "<pre><code>" + html.EscapeString(clipped) + "</code></pre>"
	}
	markdownCache.Lock()
	if _, exists := markdownCache.values[key]; !exists {
		for len(markdownCache.order) > 0 && (len(markdownCache.order) >= 64 || markdownCache.bytes+len(rendered) > markdownCacheLimit) {
			oldest := markdownCache.order[0]
			markdownCache.order = markdownCache.order[1:]
			markdownCache.bytes -= len(markdownCache.values[oldest])
			delete(markdownCache.values, oldest)
		}
		markdownCache.values[key] = rendered
		markdownCache.order = append(markdownCache.order, key)
		markdownCache.bytes += len(rendered)
	}
	markdownCache.Unlock()
	return template.HTML(rendered) // Sole audited server-side HTML trust boundary.
}

func displaySnapshot(snapshot RuntimeSnapshot) RuntimeSnapshot {
	// RuntimeBackend promises independent snapshots; additionally clone messages
	// so presentation never mutates a backend or mock's stored text projection.
	snapshot.Messages = slices.Clone(snapshot.Messages)
	for i := range snapshot.Messages {
		snapshot.Messages[i].HTML = ""
		snapshot.Messages[i].Images = displayMessageImages(snapshot, snapshot.Messages[i])
		if len(snapshot.Messages[i].Images) > 0 {
			snapshot.Messages[i].CanEdit = false
		}
		if snapshot.Messages[i].Role == "assistant" || snapshot.Messages[i].Role == "plan" {
			snapshot.Messages[i].HTML = string(markdownHTML(snapshot.Messages[i].Text))
		}
	}
	return snapshot
}
