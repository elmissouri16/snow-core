package responsesapi

import (
	"encoding/json"
	"testing"
)

func TestRequestMarshalerPreservesRawJSONSemantics(t *testing.T) {
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
	assertEquivalentResponseJSON(t, got, want)
}
