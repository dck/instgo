package ig

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
)

type caaStub struct {
	pub     string
	twoStep bool
	paths   *[]string
}

func bloksResult(key, expr string) map[string]any {
	return map[string]any{"layout": map[string]any{"bloks_payload": map[string]any{key: expr}}, "status": "ok"}
}

func (s caaStub) RoundTrip(r *http.Request) (*http.Response, error) {
	*s.paths = append(*s.paths, r.URL.Host+r.URL.Path)
	login, _ := json.Marshal(map[string]string{
		"login_response": `{"logged_in_user":{"pk":42,"username":"tester"},"status":"ok"}`,
		"headers":        `{"IG-Set-Authorization":"Bearer IGT:2:` + base64.StdEncoding.EncodeToString([]byte(`{"ds_user_id":"42","sessionid":"42%3Asecret"}`)) + `"}`,
		"cookies":        "Set-Cookie: sessionid=42%3Asecret; Path=/",
	})
	h := http.Header{}
	var body any = map[string]any{"status": "ok"}
	p := r.URL.Path
	switch {
	case strings.Contains(p, "qe/sync"):
		h.Set("ig-set-password-encryption-key-id", "159")
		h.Set("ig-set-password-encryption-pub-key", s.pub)
	case strings.Contains(p, "graphql_www"):
		body = map[string]any{"data": map[string]any{"xig_usdid_registration": map[string]any{"success": true}}}
	case strings.Contains(p, "process_client_data_and_redirect"):
		body = map[string]any{"layout": map[string]any{"bloks_payload": map[string]any{"data": []any{map[string]any{"data": map[string]any{"key": "CAA_ACCOUNT_ACCESS_CONTEXT:aac", "initial": "AAC"}}}}}}
	case strings.Contains(p, "create_android_keystore"):
		body = map[string]any{"challenge_nonce": "NONCE", "status": "ok"}
	case strings.Contains(p, "send_login_request") && s.twoStep:
		body = bloksResult("action", `(a "com.bloks.www.ap.two_step_verification.entrypoint_async" (f4i (dkc "server_params") (dkc (f4i (dkc "context_data") (dkc "CTX_ENTRY")))))`)
	case strings.Contains(p, "send_login_request"), strings.Contains(p, "code_entry_async"):
		body = bloksResult("action", "(t "+dumps(string(login))+")")
	case strings.Contains(p, "entrypoint_async"):
		body = bloksResult("action", `(x "com.bloks.www.ap.two_step_verification.code_entry" (f4i (dkc "server_params") (dkc (f4i (dkc "context_data") (dkc "CTX_CODE")))))`)
	case strings.Contains(p, "two_step_verification.code_entry"):
		body = bloksResult("tree", `(y "com.bloks.www.ap.two_step_verification.code_entry_async" (f4i (dkc "server_params") (dkc (f4i (dkc "context_data") (dkc "CTX_SUBMIT")))))`)
	}
	raw, _ := json.Marshal(body)
	return &http.Response{StatusCode: 200, Header: h, Body: io.NopCloser(bytes.NewReader(raw)), Request: r}, nil
}

func runStubLogin(t *testing.T, twoStep bool) ([]string, *Client, string) {
	t.Helper()
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	der, _ := x509.MarshalPKIXPublicKey(&key.PublicKey)
	pub := base64.StdEncoding.EncodeToString(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
	var paths []string
	var logBuf bytes.Buffer
	SetDebugOutput(&logBuf)
	defer SetDebugOutput(io.Discard)
	c := New(NewSession(), t.TempDir()+"/s.json")
	stub := caaStub{pub: pub, twoStep: twoStep, paths: &paths}
	c.http.Transport, c.web.Transport = stub, stub
	err := c.Login(context.Background(), "tester", "pw")
	if twoStep {
		var cr *CodeRequired
		if !errors.As(err, &cr) || cr.TwoFactor {
			t.Fatalf("expected email code step, got %v", err)
		}
		err = c.SubmitCode(context.Background(), cr, "123456")
	}
	if err != nil || c.UserID() != "42" || !c.LoggedIn() {
		t.Fatalf("login failed: err=%v user=%q", err, c.UserID())
	}
	return paths, c, logBuf.String()
}

func TestCAALoginSequence(t *testing.T) {
	paths, _, log := runStubLogin(t, false)
	want := []string{
		"b.i.instagram.com/graphql_www",
		"b.i.instagram.com/api/v1/bloks/async_action/com.bloks.www.bloks.caa.login.process_client_data_and_redirect/",
		"b.i.instagram.com/api/v1/attestation/create_android_keystore/",
		"b.i.instagram.com/api/v1/bloks/async_action/com.bloks.www.caa.login.oauth.token.fetch.async/",
		"i.instagram.com/api/v1/qe/sync/",
		"b.i.instagram.com/api/v1/bloks/async_action/com.bloks.www.bloks.caa.login.async.send_login_request/",
		"i.instagram.com/api/v1/feed/reels_tray/",
		"i.instagram.com/api/v1/feed/timeline/",
	}
	if !slices.Equal(paths, want) {
		t.Fatalf("request sequence:\n%s", strings.Join(paths, "\n"))
	}
	if strings.Contains(log, "secret") || strings.Contains(log, base64.StdEncoding.EncodeToString([]byte(`{"ds_user_id":"42"`))[:20]) {
		t.Fatal("debug log leaks session credentials")
	}
}

func TestCAALoginEmailCode(t *testing.T) {
	paths, _, _ := runStubLogin(t, true)
	if !slices.Contains(paths, "b.i.instagram.com/api/v1/bloks/async_action/com.bloks.www.ap.two_step_verification.code_entry_async/") {
		t.Fatalf("code was not submitted:\n%s", strings.Join(paths, "\n"))
	}
}
