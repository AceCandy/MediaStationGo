package service

import "testing"

func TestResolveAudioStreamIndexUsesDefaultAndRejectsUnknown(t *testing.T) {
	doc := &ProbeDocument{SchemaVersion: ProbeDocumentSchemaVersion, Streams: []ProbeStream{
		{Index: 0, CodecType: "video"},
		{Index: 2, CodecType: "audio"},
		{Index: 3, CodecType: "audio", Disposition: ProbeDisposition{Default: true}},
	}}
	if got, err := resolveAudioStreamIndex(doc, nil); err != nil || got != 3 {
		t.Fatalf("default index = %d, err=%v", got, err)
	}
	requested := 2
	if got, err := resolveAudioStreamIndex(doc, &requested); err != nil || got != 2 {
		t.Fatalf("requested index = %d, err=%v", got, err)
	}
	requested = 1
	if _, err := resolveAudioStreamIndex(doc, &requested); err == nil {
		t.Fatal("unknown audio index accepted")
	}
}
