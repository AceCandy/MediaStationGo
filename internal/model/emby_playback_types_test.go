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
