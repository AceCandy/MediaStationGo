package model

import (
	"encoding/json"
	"testing"
)

func TestEmbyPlaybackInfoRequestAcceptsStringMaxAudioChannels(t *testing.T) {
	var req EmbyPlaybackInfoRequest
	if err := json.Unmarshal([]byte(`{"DeviceProfile":{"TranscodingProfiles":[{"MaxAudioChannels":"2"}]}}`), &req); err != nil {
		t.Fatal(err)
	}
	if got := req.DeviceProfile.TranscodingProfiles[0].MaxAudioChannels; got != 2 {
		t.Fatalf("MaxAudioChannels = %d, want 2", got)
	}
}

func TestEmbyPlaybackInfoMinSegments(t *testing.T) {
	for _, tc := range []struct {
		value string
		valid bool
		want  EmbyFlexibleInt
	}{
		{`1`, true, 1}, {`"1"`, true, 1}, {`""`, true, 0}, {`null`, true, 0},
		{`"invalid"`, false, 0}, {`1.5`, false, 0}, {`true`, false, 0},
	} {
		t.Run(tc.value, func(t *testing.T) {
			var req EmbyPlaybackInfoRequest
			err := json.Unmarshal([]byte(`{"DeviceProfile":{"TranscodingProfiles":[{"MaxAudioChannels":"6","MinSegments":`+tc.value+`}]}}`), &req)
			if (err == nil) != tc.valid {
				t.Fatalf("decode error = %v, valid = %v", err, tc.valid)
			}
			if err == nil && req.DeviceProfile.TranscodingProfiles[0].MinSegments != tc.want {
				t.Fatalf("unexpected MinSegments: %+v", req.DeviceProfile.TranscodingProfiles)
			}
		})
	}
}
