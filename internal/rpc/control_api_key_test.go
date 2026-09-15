package rpc

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

type controlFakeAPIKeys struct {
	calls  []string
	secret string
	err    error
}

func (s *controlFakeAPIKeys) InspectAPIKey(_ context.Context, request protocol.HostAPIKeyInspectRequest) (protocol.HostAPIKeyStatusResponse, error) {
	s.calls = append(s.calls, "inspect")
	return protocol.HostAPIKeyStatusResponse{ProviderID: request.ProviderID, APIKeySupported: true, Revision: "missing", CheckedLocally: true, AppliesTo: "future_runtime"}, s.err
}
func (s *controlFakeAPIKeys) SetAPIKey(_ context.Context, request protocol.HostAPIKeySetRequest) (protocol.HostAPIKeyStatusResponse, error) {
	s.calls = append(s.calls, "set")
	s.secret = request.Secret
	return protocol.HostAPIKeyStatusResponse{ProviderID: request.ProviderID, APIKeySupported: true, Revision: strings.Repeat("a", 64), CheckedLocally: true, AppliesTo: "future_runtime"}, s.err
}

func TestControlAPIKeysExplicitCapabilityAndWriteOnlySecret(t *testing.T) {
	input := `{"id":"i","type":"api_key_inspect","params":{"provider_id":"openai-compatible"}}` + "\n" +
		`{"id":"s","type":"api_key_set","params":{"provider_id":"openai-compatible","expected_revision":"missing","secret":"PRIVATE_SUBMITTED_KEY","confirm_replace":false}}`
	for _, enabled := range []bool{false, true} {
		var output bytes.Buffer
		keys := &controlFakeAPIKeys{}
		var options ControlOptions
		if enabled {
			options.APIKeys = keys
		}
		if err := ServeControl(t.Context(), strings.NewReader(input), &output, t.TempDir(), "", &controlTestService{}, options); err != nil {
			t.Fatal(err)
		}
		frames := catalogFrames(t, output.String())
		caps := frames[0]["capabilities"].([]any)
		if enabled {
			if len(caps) != 4 || caps[3] != controlAPIKeyCapability || frames[1]["success"] != true || frames[2]["success"] != true || len(keys.calls) != 2 || keys.secret != "PRIVATE_SUBMITTED_KEY" {
				t.Fatalf("enabled frames=%v calls=%v", frames, keys.calls)
			}
		} else if len(caps) != 3 || len(keys.calls) != 0 || frames[1]["success"] != false || frames[2]["success"] != false {
			t.Fatalf("absent capability accepted key operations: %v", frames)
		}
		if strings.Contains(output.String(), "PRIVATE_SUBMITTED_KEY") {
			t.Fatal("submitted key echoed")
		}
	}
}

func TestControlAPIKeyStrictOriginalRequests(t *testing.T) {
	requests := []string{
		`{"type":"api_key_set","secret":"PRIVATE_TOP_LEVEL_SECRET","params":{}}`,
		`{"type":"api_key_set","params":{"provider_id":"openai-compatible","expected_revision":"missing","secret":"one","secret":"two","confirm_replace":false}}`,
		`{"type":"api_key_set","params":{"provider_id":"openai-compatible","expected_revision":"missing","secret":"key","confirm_replace":false,"endpoint":"PRIVATE_ENDPOINT"}}`,
		`{"type":"api_key_set","params":{"provider_id":"openai-compatible","expected_revision":"missing","secret":"key"}}`,
		`{"type":"api_key_set","params":{"provider_id":"openai-compatible","expected_revision":"missing","secret":null,"confirm_replace":false}}`,
		`{"type":"api_key_set","params":{"provider_id":"openai-compatible","expected_revision":"missing","secret":"` + strings.Repeat("x", 4097) + `","confirm_replace":false}}`,
		`{"type":"api_key_set","params":{"provider_id":"openai-compatible","expected_revision":"wrong","secret":"key","confirm_replace":false}}`,
		`{"type":"api_key_set","params":{"provider_id":"openai-compatible","expected_revision":"missing","secret":"key","confirm_replace":"true"}}`,
		`{"type":"api_key_inspect","params":{"provider_id":"openai-compatible","secret":"PRIVATE_INSPECT_KEY"}}`,
		`{"type":"api_key_inspect","params":{"provider_id":"PRIVATE/PATH"}}`,
		`{"type":"api_key_inspect","params":{"provider_id":"` + strings.Repeat("x", 65) + `"}}`,
	}
	for _, request := range requests {
		var output bytes.Buffer
		keys := &controlFakeAPIKeys{}
		if err := ServeControl(t.Context(), strings.NewReader(request), &output, t.TempDir(), "", &controlTestService{}, ControlOptions{APIKeys: keys}); err != nil {
			t.Fatal(err)
		}
		frames := catalogFrames(t, output.String())
		if len(keys.calls) != 0 || frames[1]["success"] != false {
			t.Fatalf("invalid key request reached service: %v", frames)
		}
		if strings.Contains(output.String(), "PRIVATE_") {
			t.Fatal("key request leaked")
		}
	}
}

func TestControlAPIKeyRawFailureNeverEchoed(t *testing.T) {
	var output bytes.Buffer
	keys := &controlFakeAPIKeys{err: errors.New("PRIVATE_SECRET /private/auth/path")}
	input := `{"id":"s","type":"api_key_set","params":{"provider_id":"openai-compatible","expected_revision":"missing","secret":"PRIVATE_SECRET","confirm_replace":true}}`
	if err := ServeControl(t.Context(), strings.NewReader(input), &output, t.TempDir(), "", &controlTestService{}, ControlOptions{APIKeys: keys}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "PRIVATE_SECRET") || strings.Contains(output.String(), "/private/auth/path") {
		t.Fatal("secret-bearing service failure echoed")
	}
	frames := catalogFrames(t, output.String())
	if frames[1]["success"] != false || frames[1]["error_code"] != "invalid" {
		t.Fatalf("response=%v", frames[1])
	}
}
