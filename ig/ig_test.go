package ig

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/binary"
	"strings"
	"testing"
	"time"
)

func TestEncryptPasswordRoundTrip(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1700000000, 0)
	enc, err := encryptPassword("hunter2", 41, &key.PublicKey, now)
	if err != nil {
		t.Fatal(err)
	}
	prefix := "#PWD_INSTAGRAM:4:1700000000:"
	if !strings.HasPrefix(enc, prefix) {
		t.Fatalf("unexpected prefix: %s", enc)
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(enc, prefix))
	if err != nil {
		t.Fatal(err)
	}
	if raw[0] != 1 || raw[1] != 41 {
		t.Fatalf("bad header %v", raw[:2])
	}
	iv := raw[2:14]
	size := int(binary.LittleEndian.Uint16(raw[14:16]))
	rest := raw[16:]
	sessionKey, err := rsa.DecryptPKCS1v15(nil, key, rest[:size])
	if err != nil {
		t.Fatal(err)
	}
	tag, ct := rest[size:size+16], rest[size+16:]
	block, _ := aes.NewCipher(sessionKey)
	gcm, _ := cipher.NewGCM(block)
	plain, err := gcm.Open(nil, iv, append(ct, tag...), []byte("1700000000"))
	if err != nil {
		t.Fatal(err)
	}
	if string(plain) != "hunter2" {
		t.Fatalf("got %q", plain)
	}
}

func TestAuthorizationUserID(t *testing.T) {
	payload := base64.StdEncoding.EncodeToString([]byte(`{"ds_user_id":"12345","sessionid":"x"}`))
	if got := authorizationUserID("Bearer IGT:2:" + payload); got != "12345" {
		t.Fatalf("got %q", got)
	}
	if got := authorizationUserID("Bearer IGT:2:"); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestJazoest(t *testing.T) {
	if got := jazoest("ab"); got != "2195" {
		t.Fatalf("got %q", got)
	}
}
