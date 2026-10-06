package huangguoai

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"testing"
)

func TestSyntheticArtworkCBCAndRuntimeParameters(t *testing.T) {
	key := bytes.Repeat([]byte("a"), 16)
	iv := bytes.Repeat([]byte("b"), 16)
	plain := append([]byte{0xff, 0xd8, 0xff}, bytes.Repeat([]byte{0}, 29)...)
	block, _ := aes.NewCipher(key)
	encrypted := make([]byte, len(plain))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(encrypted, plain)
	got, err := DecodeArtwork(encrypted, key, iv)
	if err != nil || !bytes.Equal(got, plain) {
		t.Fatal("synthetic image decryption failed")
	}
	if _, err = DecodeArtwork(encrypted[:17], key, iv); err == nil {
		t.Fatal("invalid ciphertext accepted")
	}
	script := []byte(`const i0={mode:"CBC",media_key:"97_97_97_97_97_97_97_97_97_97_97_97_97_97_97_97",media_iv:"98_98_98_98_98_98_98_98_98_98_98_98_98_98_98_98"};`)
	k, v, err := imageParameters(script)
	if err != nil || !bytes.Equal(k, key) || !bytes.Equal(v, iv) {
		t.Fatal("runtime parameter shape mismatch")
	}
}
