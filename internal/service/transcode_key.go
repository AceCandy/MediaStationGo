package service

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
)

// TranscodeKey 标识会影响 HLS 产物的输入选择；字幕外部交付，不进入 key。
type TranscodeKey struct {
	MediaID          string
	AudioStreamIndex int
}

func (k TranscodeKey) String() string {
	sum := sha256.Sum256([]byte(k.MediaID + "\x00" + strconv.Itoa(k.AudioStreamIndex)))
	return hex.EncodeToString(sum[:16])
}
