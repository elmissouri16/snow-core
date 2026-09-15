package web

import (
	"context"
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

type composerHTTPRuntime struct {
	fakeRuntime
	project, instance, text string
	content                 []protocol.ContentBlock
	err                     error
}

func (f *composerHTTPRuntime) PromptContent(_ context.Context, project, instance, text string, content []protocol.ContentBlock) error {
	f.calls = append(f.calls, "prompt-content")
	f.project, f.instance, f.text, f.content = project, instance, text, content
	return f.err
}

func TestComposerContentHTTPStrictFields(t *testing.T) {
	backend := &composerHTTPRuntime{}
	s := &shell{runtimes: backend}
	project := Project{ID: "project"}
	valid := url.Values{"csrf": {"csrf"}, "instance_id": {"instance"}, "text": {"read note.txt"}, "content": {`[{"type":"text","text":"Attachment: note.txt\nbody"}]`}}
	invoke := func(values url.Values, query string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/?"+query, strings.NewReader(values.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		w := httptest.NewRecorder()
		s.runtimeComposerPrompt(t.Context(), w, r, project)
		return w
	}
	for _, key := range []string{"csrf", "instance_id", "text", "content"} {
		for _, change := range []string{"missing", "duplicate"} {
			bad := valid.Clone()
			if change == "missing" {
				bad.Del(key)
			} else {
				bad.Add(key, bad.Get(key))
			}
			if w := invoke(bad, ""); w.Code != http.StatusBadRequest {
				t.Fatalf("%s %s = %d", change, key, w.Code)
			}
		}
	}
	for _, key := range []string{"command", "mode", "provider", "model", "content[]", "session_id", "skill", "tools"} {
		bad := valid.Clone()
		bad.Set(key, "injected")
		if w := invoke(bad, ""); w.Code != http.StatusBadRequest {
			t.Fatalf("extra %s = %d", key, w.Code)
		}
	}
	for _, query := range []string{"csrf=other", "instance_id=other", "text=other", "content=[]", "unknown=x", "%zz"} {
		// ParseForm rejects malformed query itself, so exercise that case with
		// pre-parsed POST fields exactly as a defensive helper check.
		if query == "%zz" {
			r := httptest.NewRequest(http.MethodPost, "/?%zz", nil)
			r.PostForm = valid
			w := httptest.NewRecorder()
			s.runtimeComposerPrompt(t.Context(), w, r, project)
			if w.Code != http.StatusBadRequest {
				t.Fatal("malformed query accepted")
			}
			continue
		}
		if w := invoke(valid, query); w.Code != http.StatusBadRequest {
			t.Fatalf("query %q = %d", query, w.Code)
		}
	}
	for _, pair := range [][2]string{
		{"csrf", ""}, {"instance_id", ""}, {"instance_id", "bad\x00"}, {"text", "bad\x00"},
		{"content", "[]"}, {"content", "{"}, {"content", `[{"type":"text","text":"\u0000"}]`},
		{"content", `[{"type":"image","mime_type":"image/png","data":"bm90LXBuZw=="}]`},
		{"content", `[{"type":"text","text":"x","provider_data":null}]`},
	} {
		bad := valid.Clone()
		bad.Set(pair[0], pair[1])
		if w := invoke(bad, ""); w.Code != http.StatusBadRequest {
			t.Fatalf("bad %s = %d", pair[0], w.Code)
		}
	}
	if len(backend.calls) != 0 {
		t.Fatal("invalid content reached backend")
	}
	w := invoke(valid, "")
	if w.Code != http.StatusOK || len(backend.calls) != 1 || backend.project != project.ID || backend.instance != "instance" || backend.text != "read note.txt" || !reflect.DeepEqual(backend.content, []protocol.ContentBlock{{Type: protocol.BlockText, Text: "Attachment: note.txt\nbody"}}) {
		t.Fatalf("valid forwarding: %d %s %+v", w.Code, w.Body.String(), backend)
	}
	backend.err = ErrRuntimeBusy
	if w := invoke(valid, ""); w.Code != http.StatusConflict {
		t.Fatalf("busy status: %d", w.Code)
	}
	s.runtimes = &fakeRuntime{}
	if w := invoke(valid, ""); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("legacy fallback: %d", w.Code)
	}
}

func TestComposerContentHTTPAuthorizationAndFormLimits(t *testing.T) {
	s, cookie, _ := projectShell(t)
	project, err := s.registry.Add(t.Context(), "project", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	backend := &composerHTTPRuntime{}
	s.runtimes = backend
	base := "/projects/" + project.ID + "/runtime/"
	csrf := csrfFor(t, s, cookie)
	// Legal aggregate text is intentionally >256KiB after URL encoding. Only
	// prompt-content may use the expanded form cap.
	form := url.Values{"csrf": {csrf}, "instance_id": {"instance"}, "text": {strings.Repeat("&", composerMaxPrompt)}}
	content, err := json.Marshal([]protocol.ContentBlock{{Type: protocol.BlockText, Text: strings.Repeat("&", composerMaxText-composerMaxPrompt)}})
	if err != nil {
		t.Fatal(err)
	}
	form.Set("content", string(content))
	if len(form.Encode()) <= 256<<10 {
		t.Fatal("fixture not larger than legacy cap")
	}
	if w := request(t, s, "POST", base+"prompt-content", form); w.Code == http.StatusOK {
		t.Fatal("unpaired attachment accepted")
	}
	bad := form.Clone()
	bad.Del("csrf")
	if w := request(t, s, "POST", base+"prompt-content", bad, cookie); w.Code < 400 {
		t.Fatal("missing CSRF accepted")
	}
	bad.Set("csrf", "wrong")
	if w := request(t, s, "POST", base+"prompt-content", bad, cookie); w.Code < 400 {
		t.Fatal("wrong CSRF accepted")
	}
	if len(backend.calls) != 0 {
		t.Fatal("unauthorized prompt reached backend")
	}
	if w := request(t, s, "POST", base+"prompt-content", form, cookie); w.Code != http.StatusOK {
		t.Fatalf("expanded form: %d %s", w.Code, w.Body.String())
	}
	if w := request(t, s, "POST", base+"prompt", form, cookie); w.Code < 400 {
		t.Fatal("legacy action limit expanded")
	}
	oversized := form.Clone()
	oversized.Set("content", strings.Repeat("a", 4<<20))
	if w := request(t, s, "POST", base+"prompt-content", oversized, cookie); w.Code < 400 {
		t.Fatal("unbounded attachment form")
	}
	if len(backend.calls) != 1 {
		t.Fatalf("unexpected dispatch: %v", backend.calls)
	}
	// The original text-only HTTP surface still targets Prompt, not optional
	// composer support, when no content action is requested.
	legacy := &fakeRuntime{}
	s.runtimes = legacy
	if w := request(t, s, "POST", base+"prompt", url.Values{"csrf": {csrf}, "instance_id": {"instance"}, "text": {"legacy"}}, cookie); w.Code != http.StatusOK || !reflect.DeepEqual(legacy.calls, []string{"prompt"}) {
		t.Fatalf("legacy prompt changed: %d %v", w.Code, legacy.calls)
	}
}
