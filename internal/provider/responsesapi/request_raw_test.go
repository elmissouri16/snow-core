package responsesapi

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestRequestMarshalerMatchesRawJSONFormatting(t *testing.T) {
	body := responsesRequest{
		Model: "m",
		Input: []any{json.RawMessage(" \n { \"type\" : \"reasoning\", \"id\" : \"r1\", \"summary\" : [ ], \"encrypted_content\" : \"<opaque>&\" } \n ")},
		Tools: []responsesTool{{
			Type: "function", Name: "read",
			Parameters: json.RawMessage(" \n { \"type\" : \"object\", \"properties\" : { } } \n "),
		}},
	}
	want, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	got, err := marshalRequestBody(body)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("raw JSON formatting changed\n got: %s\nwant: %s", got, want)
	}
}
