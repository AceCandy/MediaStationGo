package model

import "testing"

func TestDoubanPayloadIdentityAtBothLevels(t *testing.T) {
	for _, tc := range []struct {
		payload string
		want    bool
	}{
		{`{"id":"100","type":"movie","data":{"id":"100","title":"详情"}}`, true},
		{`{"subject":{"id":"0100","type":"movie"}}`, true},
		{`{"id":"200","data":{"id":"100"}}`, false},
		{`{"id":"100","subject":{"id":"200"}}`, false},
		{`{"type":"tv","subject":{"type":"movie"}}`, false},
		{`{"type":"movie","is_tv":true}`, false},
		{`null`, false},
		{`[]`, false},
	} {
		if got := DoubanPayloadMatchesIdentity([]byte(tc.payload), "100", "movie"); got != tc.want {
			t.Errorf("%s: %v want %v", tc.payload, got, tc.want)
		}
	}
}
