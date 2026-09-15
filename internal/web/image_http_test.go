package web

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func sentImagePNG(t *testing.T) []byte {
	t.Helper()
	var out bytes.Buffer
	if err := png.Encode(&out, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

type imageCatalogFixture struct {
	fakeCatalog
	result     protocol.RPCCatalogImage
	imageCalls int
	after      func()
}

func (f *imageCatalogFixture) Image(_ context.Context, _ Project, p protocol.RPCCatalogImageParams) (protocol.RPCCatalogImage, error) {
	f.imageCalls++
	if f.after != nil {
		f.after()
	}
	return f.result, nil
}

type imageRuntimeFixture struct {
	fakeRuntime
	result     protocol.RPCCatalogImage
	imageCalls int
	after      func()
}

func (f *imageRuntimeFixture) MessageImage(_ context.Context, _, _, _, _ string, _ int) (protocol.RPCCatalogImage, error) {
	f.imageCalls++
	if f.after != nil {
		f.after()
	}
	return f.result, nil
}

func sentImageShell(t *testing.T) (*shell, *http.Cookie, Project, *imageCatalogFixture, *imageRuntimeFixture) {
	t.Helper()
	s, cookie, _ := projectShell(t)
	project, err := s.registry.Add(t.Context(), "images", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	result := protocol.RPCCatalogImage{SessionID: "session", MessageID: "durable", Index: 3, MIMEType: "image/png", Data: sentImagePNG(t)}
	catalog := &imageCatalogFixture{result: result}
	runtime := &imageRuntimeFixture{result: result, fakeRuntime: fakeRuntime{snapshot: RuntimeSnapshot{ProjectID: project.ID, InstanceID: "instance", SessionID: "session", Status: "running", Messages: []RuntimeMessage{{ID: "row", Role: "user", SourceID: "durable", Images: []MessageImage{{Index: 3, MIMEType: "image/png"}}}}}}}
	s.catalog, s.runtimes = catalog, runtime
	return s, cookie, project, catalog, runtime
}

func TestSentImageHTTPAuthenticatedRasterAndCSP(t *testing.T) {
	for _, live := range []bool{false, true} {
		t.Run(strconv.FormatBool(live), func(t *testing.T) {
			s, cookie, project, catalog, runtime := sentImageShell(t)
			runtime.live = live
			path := "/projects/" + project.ID + "/sessions/session/images/durable/3"
			if live {
				path = displaySnapshot(runtime.snapshot).Messages[0].Images[0].URL
			}
			w := request(t, s, "GET", path, nil)
			if w.Code != http.StatusUnauthorized || catalog.imageCalls+runtime.imageCalls != 0 {
				t.Fatalf("unauthenticated: %d", w.Code)
			}
			w = request(t, s, "GET", path, nil, cookie)
			if w.Code != 200 || !bytes.Equal(w.Body.Bytes(), catalog.result.Data) {
				t.Fatalf("image: %d %s", w.Code, w.Body.String())
			}
			for header, want := range map[string]string{"Content-Type": "image/png", "Cache-Control": "no-store", "X-Content-Type-Options": "nosniff", "Cross-Origin-Resource-Policy": "same-origin"} {
				if w.Header().Get(header) != want {
					t.Errorf("%s=%q", header, w.Header().Get(header))
				}
			}
			csp := w.Header().Get("Content-Security-Policy")
			if !strings.Contains(csp, "img-src 'self' blob:;") || strings.Contains(csp, "data:") || strings.Contains(csp, "https:") {
				t.Fatalf("CSP=%s", csp)
			}
			if len(runtime.calls) != 0 || catalog.calls != 0 {
				t.Fatal("image triggered mutation or history lookup")
			}
		})
	}
}

func TestSentImageHTTPRejectsInvalidRequestsBeforeRead(t *testing.T) {
	s, cookie, project, catalog, runtime := sentImageShell(t)
	runtime.live = true
	base := "/projects/" + project.ID + "/runtime/images/row/3"
	for _, query := range []string{"", "?instance_id=instance", "?session_id=session", "?instance_id=instance&session_id=session&url=https://evil", "?instance_id=instance&instance_id=instance&session_id=session", "?instance_id=stale&session_id=session", "?instance_id=instance&session_id=stale", "?instance_id=instance&session_id=session&turn_id=wrong", "?instance_id=instance&session_id=session&x=%zz"} {
		if w := request(t, s, "GET", base+query, nil, cookie); w.Code < 400 {
			t.Fatalf("accepted %q", query)
		}
	}
	query := "?instance_id=instance&session_id=session"
	for _, suffix := range []string{"row/-1", "row/01", "row/10001", "row/no", "unknown/3", "row/4"} {
		if w := request(t, s, "GET", "/projects/"+project.ID+"/runtime/images/"+suffix+query, nil, cookie); w.Code < 400 {
			t.Fatalf("accepted %q", suffix)
		}
	}
	for _, method := range []string{"POST", "HEAD"} {
		if w := request(t, s, method, base+query, url.Values{}, cookie); w.Code < 400 {
			t.Fatalf("accepted %s", method)
		}
	}
	if runtime.imageCalls+catalog.imageCalls != 0 {
		t.Fatal("invalid read reached backend")
	}
}

func TestSentImageHTTPRejectsWrongOrUnsafeResponse(t *testing.T) {
	for name, mutate := range map[string]func(*protocol.RPCCatalogImage){
		"session":   func(v *protocol.RPCCatalogImage) { v.SessionID = "other" },
		"message":   func(v *protocol.RPCCatalogImage) { v.MessageID = "other" },
		"index":     func(v *protocol.RPCCatalogImage) { v.Index = 0 },
		"mime":      func(v *protocol.RPCCatalogImage) { v.MIMEType = "image/jpeg" },
		"svg":       func(v *protocol.RPCCatalogImage) { v.MIMEType = "image/svg+xml"; v.Data = []byte("<svg/>") },
		"html":      func(v *protocol.RPCCatalogImage) { v.Data = []byte("<script>alert(1)</script>") },
		"empty":     func(v *protocol.RPCCatalogImage) { v.Data = nil },
		"oversized": func(v *protocol.RPCCatalogImage) { v.Data = make([]byte, composerMaxImageBytes+1) },
	} {
		for _, live := range []bool{false, true} {
			t.Run(name+strconv.FormatBool(live), func(t *testing.T) {
				s, cookie, project, catalog, runtime := sentImageShell(t)
				runtime.live = live
				mutate(&catalog.result)
				runtime.result = catalog.result
				path := "/projects/" + project.ID + "/sessions/session/images/durable/3"
				if live {
					path = displaySnapshot(runtime.snapshot).Messages[0].Images[0].URL
				}
				if w := request(t, s, "GET", path, nil, cookie); w.Code < 400 || strings.HasPrefix(w.Header().Get("Content-Type"), "image/") {
					t.Fatalf("unsafe response=%d", w.Code)
				}
			})
		}
	}
}

func TestSentImageHTTPRejectsStaleOwnershipAndProject(t *testing.T) {
	for _, change := range []string{"instance", "session", "source", "turn", "row", "project", "revoke"} {
		t.Run(change, func(t *testing.T) {
			s, cookie, project, _, runtime := sentImageShell(t)
			runtime.live = true
			path := displaySnapshot(runtime.snapshot).Messages[0].Images[0].URL
			runtime.after = func() {
				switch change {
				case "instance":
					runtime.snapshot.InstanceID = "new"
				case "session":
					runtime.snapshot.SessionID = "new"
				case "source":
					runtime.snapshot.Messages[0].SourceID = "new"
				case "turn":
					runtime.snapshot.Messages[0].SourceTurnID = "new"
				case "row":
					runtime.snapshot.Messages = nil
				case "project":
					if err := os.Rename(project.Path, project.Path+"-moved"); err != nil {
						t.Fatal(err)
					}
				case "revoke":
					s.access.mu.Lock()
					clear(s.access.sessions)
					s.access.mu.Unlock()
				}
			}
			if w := request(t, s, "GET", path, nil, cookie); w.Code < 400 {
				t.Fatalf("stale read=%d", w.Code)
			}
		})
	}
}

func TestSavedImageRejectsAnyLiveOwnerBeforeAndAfterRead(t *testing.T) {
	for _, status := range []string{"running", "idle", "failed", "closing"} {
		s, cookie, project, catalog, runtime := sentImageShell(t)
		runtime.live = true
		runtime.snapshot.Status = status
		runtime.snapshot.SessionID = "different"
		path := "/projects/" + project.ID + "/sessions/session/images/durable/3"
		if w := request(t, s, "GET", path, nil, cookie); w.Code != 409 || catalog.imageCalls != 0 {
			t.Fatalf("live %s read=%d", status, w.Code)
		}
	}
	s, cookie, project, catalog, runtime := sentImageShell(t)
	catalog.after = func() { runtime.live = true }
	path := "/projects/" + project.ID + "/sessions/session/images/durable/3"
	if w := request(t, s, "GET", path, nil, cookie); w.Code != 409 {
		t.Fatalf("new live owner read=%d", w.Code)
	}
	runtime.live = false
	catalog.after = nil
	if w := request(t, s, "GET", path+"?instance_id=instance", nil, cookie); w.Code != 400 {
		t.Fatalf("saved query=%d", w.Code)
	}
}

func TestSentImageHTTPNonqueuedGlobalLimit(t *testing.T) {
	s, cookie, _, _, runtime := sentImageShell(t)
	runtime.live = true
	for range cap(s.imageSlots) {
		s.imageSlots <- struct{}{}
	}
	path := displaySnapshot(runtime.snapshot).Messages[0].Images[0].URL
	if w := request(t, s, "GET", path, nil, cookie); w.Code != 429 || runtime.imageCalls != 0 {
		t.Fatalf("limit=%d", w.Code)
	}
}

func TestSavedImageReadNeverHoldsProjectMutationGate(t *testing.T) {
	s, cookie, project, catalog, _ := sentImageShell(t)
	catalog.after = func() {
		if !s.projectControl.TryLock() {
			t.Fatal("saved image read blocked project controls")
		}
		s.projectControl.Unlock()
	}
	path := "/projects/" + project.ID + "/sessions/session/images/durable/3"
	if w := request(t, s, "GET", path, nil, cookie); w.Code != http.StatusOK {
		t.Fatalf("saved read = %d", w.Code)
	}
}
