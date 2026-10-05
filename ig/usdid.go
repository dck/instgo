package ig

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	usdidDocID         = "124930351917786857261002920888"
	usdidTTL           = 3600
	usdidRefreshMargin = 300
)

var b64u = base64.RawURLEncoding

func (c *Client) usdidKey() (*ecdsa.PrivateKey, error) {
	u := &c.s.USDID
	if u.PrivateKey == "" {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return nil, err
		}
		der, err := x509.MarshalPKCS8PrivateKey(key)
		if err != nil {
			return nil, err
		}
		kid := make([]byte, 32)
		if _, err := rand.Read(kid); err != nil {
			return nil, err
		}
		*u = USDID{
			ID:         newUUID(),
			KID:        b64u.EncodeToString(kid),
			PrivateKey: string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})),
		}
		c.usdidCache, c.usdidExpires = "", 0
		return key, nil
	}
	block, _ := pem.Decode([]byte(u.PrivateKey))
	if block == nil {
		return nil, errors.New("usdid: invalid private key")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	key, ok := parsed.(*ecdsa.PrivateKey)
	if !ok {
		return nil, errors.New("usdid: not an ECDSA key")
	}
	return key, nil
}

func (c *Client) usdidSign(msg string) (string, error) {
	key, err := c.usdidKey()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(msg))
	sig, err := ecdsa.SignASN1(rand.Reader, key, sum[:])
	if err != nil {
		return "", err
	}
	return b64u.EncodeToString(sig), nil
}

func (c *Client) usdidHeader() (string, error) {
	now := time.Now().Unix()
	if c.usdidCache != "" && c.usdidExpires-now > usdidRefreshMargin {
		return c.usdidCache, nil
	}
	if _, err := c.usdidKey(); err != nil {
		return "", err
	}
	expires := now + usdidTTL
	signed := c.s.USDID.ID + "." + strconv.FormatInt(expires, 10)
	sig, err := c.usdidSign(signed)
	if err != nil {
		return "", err
	}
	c.usdidCache, c.usdidExpires = signed+"."+sig, expires
	return c.usdidCache, nil
}

func (c *Client) usdidRegistrationToken() (string, error) {
	key, err := c.usdidKey()
	if err != nil {
		return "", err
	}
	pub, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		return "", err
	}
	now := time.Now().Unix()
	payload := b64u.EncodeToString([]byte(dumps(obj{
		{"sub", c.s.USDID.ID},
		{"iat", now},
		{"aud", appID},
		{"exp", now + usdidTTL},
		{"pub", base64.StdEncoding.EncodeToString(pub)},
		{"alg", "ES256"},
	})))
	protected := b64u.EncodeToString([]byte(dumps(obj{
		{"typ", "JWT"},
		{"alg", "ES256"},
		{"kid", c.s.USDID.KID},
		{"aid", appID},
		{"ver", "1"},
	})))
	sig, err := c.usdidSign(protected + "." + payload)
	if err != nil {
		return "", err
	}
	token := obj{
		{"payload", payload},
		{"signatures", []obj{{{"protected", protected}, {"signature", sig}}}},
	}
	return b64u.EncodeToString([]byte(dumps(token))), nil
}

func (c *Client) usdidRegister(ctx context.Context) error {
	if c.s.USDID.Registered {
		return nil
	}
	token, err := c.usdidRegistrationToken()
	if err != nil {
		return err
	}
	variables := obj{{"input", obj{
		{"usdid_token", obj{{"sensitive_string_value", token}}},
		{"fdid", obj{{"sensitive_string_value", c.s.PhoneID}}},
	}}}
	body, err := c.graphqlWWW(ctx, "IGUSDIDRegistrationMutation", variables, usdidDocID, map[string]string{
		"X-Root-Field-Name":        "usdid_registration",
		"X-Graphql-Client-Library": "pando",
	})
	if err != nil {
		return err
	}
	var r struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return err
	}
	for k, v := range r.Data {
		if !strings.Contains(k, "usdid_registration") {
			continue
		}
		var reg struct {
			Success bool `json:"success"`
		}
		if json.Unmarshal(v, &reg) == nil && reg.Success {
			c.s.USDID.Registered = true
			return nil
		}
	}
	return errors.New("device registration (usdid) was not accepted")
}

func (c *Client) graphqlWWW(ctx context.Context, name string, variables any, docID string, extra map[string]string) ([]byte, error) {
	f := form{
		{"method", "post"},
		{"pretty", "false"},
		{"format", "json"},
		{"server_timestamps", "true"},
		{"locale", "user"},
		{"fb_api_req_friendly_name", name},
		{"enable_canonical_naming", "true"},
		{"enable_canonical_variable_overrides", "true"},
		{"enable_canonical_naming_ambiguous_type_prefixing", "true"},
		{"variables", dumps(variables)},
		{"purpose", "fetch"},
		{"client_doc_id", docID},
	}
	headers := map[string]string{
		"X-FB-Friendly-Name": name,
		"X-Client-Doc-Id":    docID,
	}
	for k, v := range extra {
		headers[k] = v
	}
	body, err := c.do(ctx, request{method: http.MethodPost, host: caaHost, path: "/graphql_www", rawPath: true, body: f.encode(), headers: headers})
	if err != nil {
		return nil, err
	}
	var r struct {
		Errors json.RawMessage `json:"errors"`
	}
	if json.Unmarshal(body, &r) == nil && len(r.Errors) > 0 && string(r.Errors) != "null" {
		return nil, errors.New("graphql error: " + truncate(string(r.Errors)))
	}
	return body, nil
}
