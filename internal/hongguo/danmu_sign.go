package hongguo

// Simon/Ladon 基础算法改编自 ssovit/x-gorogn-khronos-argus-ladon（MIT）。
// 许可见 danmu_sign.LICENSE；封装参数属于红果 App 协议，不与旧下载签名共用。
import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"math/bits"
	"net/http"
	"strconv"
	"time"

	"github.com/emmansun/gmsm/sm3"
	"google.golang.org/protobuf/encoding/protowire"
)

func signDanmuRequest(req *http.Request, stub []byte, now time.Time) error {
	var entropy [11]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		return err
	}
	ts := uint32(now.Unix())
	req.Header.Set("X-Khronos", strconv.FormatUint(uint64(ts), 10))
	req.Header.Set("X-SS-Req-Ticket", strconv.FormatInt(now.UnixMilli(), 10))
	req.Header.Set("X-Gorgon", danmuGorgon(req.URL.RawQuery, req.Header.Get("Cookie"), stub, ts, (entropy[0]%7+1)<<5, 0xe0|entropy[1]&15))
	req.Header.Set("X-Argus", danmuArgus(req.URL.RawQuery, stub, ts, binary.LittleEndian.Uint32(entropy[2:6]), uint64(2+entropy[6]%49*2)))
	req.Header.Set("X-Ladon", danmuLadon(ts, req.URL.Query().Get("aid"), entropy[7:]))
	return nil
}

func danmuGorgon(query, cookie string, stub []byte, ts uint32, h2, h3 byte) string {
	var data [20]byte
	q := md5.Sum([]byte(query))
	copy(data[:4], q[:4])
	copy(data[4:8], stub)
	if cookie != "" {
		c := md5.Sum([]byte(cookie))
		copy(data[8:12], c[:4])
	}
	copy(data[12:16], []byte{0, 1, 7, 4})
	binary.BigEndian.PutUint32(data[16:], ts)
	key := [8]byte{0x4a, 0, 0x16, h3, 0x47, 0x6c, 0, h2}
	var table [256]byte
	for n := range table {
		table[n] = byte(n)
	}
	var cursor byte
	for n := range table {
		cursor += table[n] + key[n%8]
		table[n] = table[cursor]
	}
	cursor = 0
	for n := range data {
		cursor += table[n+1]
		table[n+1] = table[cursor]
		data[n] ^= table[table[cursor]*2]
	}
	for n := range data {
		data[n] = bits.Reverse8(bits.RotateLeft8(data[n], 4)^data[(n+1)%20]) ^ 0xeb
	}
	return hex.EncodeToString(append([]byte{0x84, 4, h2, h3, 0, 0}, data[:]...))
}

func danmuPad(data []byte) []byte {
	return append(data, bytes.Repeat([]byte{byte(16 - len(data)%16)}, 16-len(data)%16)...)
}

func danmuArgus(query string, stub []byte, ts, random uint32, count uint64) string {
	if len(stub) != 16 {
		stub = make([]byte, 16)
	}
	bodyHash, queryHash := sm3.Sum(stub), sm3.Sum([]byte(query))
	var pb []byte
	number := func(field protowire.Number, value uint64) {
		pb = protowire.AppendVarint(protowire.AppendTag(pb, field, protowire.VarintType), value)
	}
	data := func(field protowire.Number, value []byte) {
		pb = protowire.AppendBytes(protowire.AppendTag(pb, field, protowire.BytesType), value)
	}
	number(1, 0x40401252)
	number(2, 2)
	number(3, uint64(random))
	data(4, []byte("3019"))
	data(6, []byte("1611921764"))
	data(7, []byte("6.8.1.32"))
	data(8, []byte("v04.07.01-ml-android"))
	number(9, 135135744)
	data(10, make([]byte, 8))
	number(12, uint64(ts)*2)
	data(13, bodyHash[:6])
	data(14, queryHash[:6])
	var counter []byte
	for n, value := range []uint64{count, 1388734, 1388734, 1388734} {
		counter = protowire.AppendVarint(protowire.AppendTag(counter, protowire.Number(n+1), protowire.VarintType), value)
	}
	data(15, counter)
	data(20, []byte("none"))
	number(21, 738)
	key, _ := hex.DecodeString("ac1adaae95a7af94a5114ab3b3a97dd80050aa0a39314c40528caec95256c28c")
	seed := append(append(append([]byte{}, key...), 0x3c, 0xcc, 0x56, 0x7b), key...)
	hash := sm3.Sum(seed)
	var rounds [72]uint64
	for n := 0; n < 4; n++ {
		rounds[n] = binary.LittleEndian.Uint64(hash[n*8:])
	}
	for n := 4; n < 72; n++ {
		x := bits.RotateLeft64(rounds[n-1], -3) ^ rounds[n-3]
		x ^= bits.RotateLeft64(x, -1)
		rounds[n] = ^rounds[n-4] ^ x ^ (uint64(0x3dc94c3a046d678b) >> ((n - 4) % 62) & 1) ^ 3
	}
	pb = danmuPad(pb)
	for n := 0; n < len(pb); n += 16 {
		a, b := binary.LittleEndian.Uint64(pb[n:]), binary.LittleEndian.Uint64(pb[n+8:])
		for _, round := range rounds {
			a, b = b, a^(bits.RotateLeft64(b, 1)&bits.RotateLeft64(b, 8))^bits.RotateLeft64(b, 2)^round
		}
		binary.LittleEndian.PutUint64(pb[n:], a)
		binary.LittleEndian.PutUint64(pb[n+8:], b)
	}
	word := []byte{0xd0, 0x4f, 0xfd, 0xff}
	for n := range pb {
		pb[n] ^= word[n%4]
	}
	inner := append(append(append([]byte{}, word...), word...), pb...)
	for l, r := 0, len(inner)-1; l < r; l, r = l+1, r-1 {
		inner[l], inner[r] = inner[r], inner[l]
	}
	prefix := []byte{0xa6, 0xe7, 0x83, 0xee, 0x70, 1, 0x10, 9, 0x18}
	container := danmuPad(append(append(prefix, inner...), 0x56, 0x7b))
	aesKey, iv := md5.Sum(key[:16]), md5.Sum(key[16:])
	block, _ := aes.NewCipher(aesKey[:])
	cipher.NewCBCEncrypter(block, iv[:]).CryptBlocks(container, container)
	return base64.StdEncoding.EncodeToString(append([]byte{0x3c, 0xcc}, container...))
}

func danmuLadon(ts uint32, aid string, random []byte) string {
	hash := md5.Sum(append(append([]byte{}, random...), []byte(aid)...))
	key := []byte(hex.EncodeToString(hash[:]))
	var rounds [34]uint64
	var queue [3]uint64
	rounds[0] = binary.LittleEndian.Uint64(key)
	for n := range queue {
		queue[n] = binary.LittleEndian.Uint64(key[(n+1)*8:])
	}
	for n := 0; n < 33; n++ {
		x := (bits.RotateLeft64(queue[n%3], -8) + rounds[n]) ^ uint64(n)
		queue[n%3] = x
		rounds[n+1] = x ^ bits.RotateLeft64(rounds[n], 3)
	}
	payload := danmuPad([]byte(strconv.FormatUint(uint64(ts), 10) + "-1611921764-3019"))
	for n := 0; n < len(payload); n += 16 {
		a, b := binary.LittleEndian.Uint64(payload[n:]), binary.LittleEndian.Uint64(payload[n+8:])
		for _, round := range rounds {
			b = round ^ (a + bits.RotateLeft64(b, -8))
			a = b ^ bits.RotateLeft64(a, 3)
		}
		binary.LittleEndian.PutUint64(payload[n:], a)
		binary.LittleEndian.PutUint64(payload[n+8:], b)
	}
	return base64.StdEncoding.EncodeToString(append(append([]byte{}, random...), payload...))
}
