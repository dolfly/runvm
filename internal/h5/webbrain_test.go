package h5

import (
	"encoding/json"
	"testing"
)

func TestDecodeWebbrainPayload(t *testing.T) {
	cases := []struct {
		name    string
		payload string
		want    any
		wantErr bool
	}{
		{
			name:    "object response",
			payload: `{"title":"Example Domain","url":"https://example.com"}`,
			want:    map[string]any{"title": "Example Domain", "url": "https://example.com"},
		},
		{
			// get_interactive_elements is the one handler that returns a bare
			// JSON array; decoding it as a map used to fail every page.
			name:    "array response from get_interactive_elements",
			payload: `[{"index":0,"tag":"button","role":"button","text":"Learn more"},{"index":1,"tag":"a","role":"link","text":"Domains"}]`,
			want: []any{
				map[string]any{"index": float64(0), "tag": "button", "role": "button", "text": "Learn more"},
				map[string]any{"index": float64(1), "tag": "a", "role": "link", "text": "Domains"},
			},
		},
		{
			name:    "empty array",
			payload: `[]`,
			want:    []any{},
		},
		{
			name:    "invalid json",
			payload: `not json`,
			wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := decodeWebbrainPayload(tc.payload)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("decodeWebbrainPayload(%q) = %v, want error", tc.payload, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("decodeWebbrainPayload(%q) error: %v", tc.payload, err)
			}
			wantJSON, _ := json.Marshal(tc.want)
			gotJSON, _ := json.Marshal(got)
			if string(gotJSON) != string(wantJSON) {
				t.Errorf("decodeWebbrainPayload(%q) = %s, want %s", tc.payload, gotJSON, wantJSON)
			}
		})
	}
}
