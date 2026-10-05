package ig

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

func (c *Client) passwordKey(ctx context.Context) (int, *rsa.PublicKey, error) {
	if c.s.PasswordPubKey != "" {
		if key, err := parsePublicKey(c.s.PasswordPubKey); err == nil {
			return c.s.PasswordKeyID, key, nil
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+apiHost+"/api/v1/qe/sync/", nil)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Accept-Encoding", "gzip,deflate")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Connection", "Keep-Alive")
	req.Header.Set("Accept-Language", "en-US")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_13_6) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/11.1.2 Safari/605.1.15")
	resp, err := c.web.Do(req)
	if err != nil {
		return 0, nil, err
	}
	_ = resp.Body.Close()
	debugLog.Printf("← %d GET /api/v1/qe/sync/ (password key, %s), key id header %q", resp.StatusCode, resp.Proto, resp.Header.Get("ig-set-password-encryption-key-id"))
	id, err := strconv.Atoi(resp.Header.Get("ig-set-password-encryption-key-id"))
	if err != nil {
		return 0, nil, fmt.Errorf("instagram did not return the password encryption key (HTTP %d), likely rate-limited", resp.StatusCode)
	}
	pub := resp.Header.Get("ig-set-password-encryption-pub-key")
	key, err := parsePublicKey(pub)
	if err != nil {
		return 0, nil, err
	}
	c.s.PasswordKeyID, c.s.PasswordPubKey = id, pub
	return id, key, nil
}

func parsePublicKey(b64 string) (*rsa.PublicKey, error) {
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, fmt.Errorf("password key: %w", err)
	}
	der := raw
	if block, _ := pem.Decode(raw); block != nil {
		der = block.Bytes
	}
	pub, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		return nil, fmt.Errorf("password key: %w", err)
	}
	key, ok := pub.(*rsa.PublicKey)
	if !ok {
		return nil, errors.New("password key: not RSA")
	}
	return key, nil
}

func encryptPassword(password string, keyID int, key *rsa.PublicKey, now time.Time) (string, error) {
	sessionKey := make([]byte, 32)
	iv := make([]byte, 12)
	if _, err := rand.Read(sessionKey); err != nil {
		return "", err
	}
	if _, err := rand.Read(iv); err != nil {
		return "", err
	}
	rsaEnc, err := rsa.EncryptPKCS1v15(rand.Reader, key, sessionKey)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(sessionKey)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	ts := strconv.FormatInt(now.Unix(), 10)
	sealed := gcm.Seal(nil, iv, []byte(password), []byte(ts))
	ct, tag := sealed[:len(sealed)-gcm.Overhead()], sealed[len(sealed)-gcm.Overhead():]

	payload := []byte{1, byte(keyID)}
	payload = append(payload, iv...)
	payload = binary.LittleEndian.AppendUint16(payload, uint16(len(rsaEnc)))
	payload = append(payload, rsaEnc...)
	payload = append(payload, tag...)
	payload = append(payload, ct...)
	return "#PWD_INSTAGRAM:4:" + ts + ":" + base64.StdEncoding.EncodeToString(payload), nil
}
