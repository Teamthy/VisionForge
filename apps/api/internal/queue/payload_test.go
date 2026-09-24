package queue

import (
	"encoding/json"
	"testing"

	vtypes "github.com/visionforge/visionforge/packages/types"
)

func TestJobPayloadRoundTrip(t *testing.T) {
	in := vtypes.JobPayload{
		JobID:          "job-1",
		ProjectID:      "proj-1",
		AssetID:        "asset-1",
		ModelVersionID: "mv-1",
		Priority:       vtypes.PriorityHigh,
		Attempt:        2,
		IdempotencyKey: "idem-1",
	}
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	// jobs must never embed binary payloads — only ids/refs
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if len(m) > 10 {
		t.Errorf("payload has unexpected fields: %v", m)
	}
	for k := range m {
		for _, forbidden := range []string{"image", "bytes", "data", "blob", "file"} {
			if k == forbidden {
				t.Errorf("payload must reference assets, not embed them (found %q)", k)
			}
		}
	}
	out, err := vtypes.UnmarshalJobPayload(raw)
	if err != nil {
		t.Fatal(err)
	}
	if out != in {
		t.Errorf("round trip mismatch:\n got %+v\nwant %+v", out, in)
	}
}
