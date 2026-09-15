//go:build darwin || linux

package main

import (
	"bytes"
	"encoding/json/v2"
	"errors"
	"io"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

// fixturePageCSRF reads the actual paired server presentation, not the pairing
// cookie or a fabricated token. Errors must never include response/token data.
func fixturePageCSRF(body []byte) (string, error) {
	const maxPageBytes = 8 << 20
	if len(body) > maxPageBytes {
		return "", errors.New("fixture page exceeds CSRF extraction limit")
	}
	tokens := html.NewTokenizer(bytes.NewReader(body))
	tokens.SetMaxBuf(maxPageBytes)
	var csrf string
	for tokens.Next() != html.ErrorToken {
		token := tokens.Token()
		if token.Type != html.StartTagToken && token.Type != html.SelfClosingTagToken {
			continue
		}
		var id, page, props, name, inputType, value string
		for _, attr := range token.Attr {
			switch attr.Key {
			case "id":
				id = attr.Val
			case "data-react-page":
				page = attr.Val
			case "data-react-props":
				props = attr.Val
			case "name":
				name = attr.Val
			case "type":
				inputType = attr.Val
			case "value":
				value = attr.Val
			}
		}
		var candidate string
		switch {
		case token.Data == "input" && name == "csrf" && inputType == "hidden":
			candidate = value
		case token.Data == "div" && id == "shell-react-root" && page == "shell":
			if len(props) > 1<<20 {
				return "", errors.New("fixture Shell bootstrap exceeds CSRF extraction limit")
			}
			var bootstrap struct {
				CSRF string `json:"csrf"`
			}
			// The HTML tokenizer has already decoded the escaped attribute once.
			if err := json.Unmarshal([]byte(props), &bootstrap); err != nil {
				return "", errors.New("fixture Shell CSRF bootstrap is invalid")
			}
			candidate = bootstrap.CSRF
		default:
			continue
		}
		if candidate == "" || len(candidate) > 512 || csrf != "" && csrf != candidate {
			return "", errors.New("fixture page CSRF presentation is empty, oversized or conflicting")
		}
		csrf = candidate
	}
	if !errors.Is(tokens.Err(), io.EOF) || csrf == "" {
		return "", errors.New("fixture page lacks a readable CSRF presentation")
	}
	return csrf, nil
}

func TestWebFixturePageCSRF(t *testing.T) {
	for _, tc := range []struct {
		name, body, want string
	}{
		{"legacy", `<input value="fixture&amp;token" name="csrf" type="hidden">`, "fixture&token"},
		{"shell", `<div data-react-props="{&quot;csrf&quot;:&quot;fixture&amp;\&quot;雪&quot;,&quot;projects&quot;:[]}" data-react-page="shell" id="shell-react-root"></div>`, "fixture&\"雪"},
		{"both", `<input type="hidden" name="csrf" value="fixture"><div id="shell-react-root" data-react-page="shell" data-react-props='{"csrf":"fixture"}'></div>`, "fixture"},
		{"missing", `<main>No token</main>`, ""},
		{"unknown root", `<div data-react-page="other" data-react-props='{"csrf":"fixture"}'></div>`, ""},
		{"invalid JSON", `<div id="shell-react-root" data-react-page="shell" data-react-props='{broken'></div>`, ""},
		{"duplicate JSON member", `<div id="shell-react-root" data-react-page="shell" data-react-props='{"csrf":"fixture","csrf":"other"}'></div>`, ""},
		{"conflict", `<input type="hidden" name="csrf" value="fixture"><div id="shell-react-root" data-react-page="shell" data-react-props='{"csrf":"other"}'></div>`, ""},
		{"token limit", `<input type="hidden" name="csrf" value="` + strings.Repeat("x", 513) + `">`, ""},
		{"bootstrap limit", `<div id="shell-react-root" data-react-page="shell" data-react-props='` + strings.Repeat(" ", 1<<20+1) + `'></div>`, ""},
		{"page limit", strings.Repeat(" ", 8<<20+1), ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := fixturePageCSRF([]byte(tc.body))
			if tc.want == "" {
				if err == nil || got != "" {
					t.Fatal("invalid presentation supplied fixture authority")
				}
			} else if err != nil || got != tc.want {
				t.Fatal("fixture CSRF presentation did not round-trip")
			}
		})
	}
}
