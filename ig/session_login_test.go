package ig

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"strings"
	"testing"
)

type sessionStub struct {
	status int
	auth   string
}

func (s *sessionStub) RoundTrip(r *http.Request) (*http.Response, error) {
	s.auth = r.Header.Get("Authorization")
	body := `{"user":{"username":"tester"},"status":"ok"}`
	if s.status != 200 {
		body = `{"message":"login_required","status":"fail"}`
	}
	return &http.Response{StatusCode: s.status, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader([]byte(body))), Request: r}, nil
}

func TestLoginBySessionID(t *testing.T) {
	for _, cookie := range []string{"4242%3AAbCdEfGhIjKlMnOp%3A12%3AAYzzzzzzzzzzzz", "4242:AbCdEfGhIjKlMnOp:12:AYzzzzzzzzzzzz"} {
		stub := &sessionStub{status: 200}
		c := New(NewSession(), t.TempDir()+"/s.json")
		c.http.Transport = stub
		if err := c.LoginBySessionID(context.Background(), cookie); err != nil {
			t.Fatal(err)
		}
		raw, _ := base64.StdEncoding.DecodeString(strings.TrimPrefix(stub.auth, "Bearer IGT:2:"))
		want := `{"ds_user_id":"4242","sessionid":"4242%3AAbCdEfGhIjKlMnOp%3A12%3AAYzzzzzzzzzzzz","should_use_header_over_cookies":true}`
		if string(raw) != want || c.Username() != "tester" || !c.LoggedIn() {
			t.Fatalf("auth=%s user=%q", raw, c.Username())
		}
	}
}

func TestLoginBySessionIDRejected(t *testing.T) {
	c := New(NewSession(), t.TempDir()+"/s.json")
	c.http.Transport = &sessionStub{status: 403}
	if err := c.LoginBySessionID(context.Background(), "4242%3AAbCdEfGhIjKlMnOp%3A12%3AAYzzzzzzzzzzzz"); err == nil || c.LoggedIn() {
		t.Fatalf("expected rejection, err=%v loggedIn=%v", err, c.LoggedIn())
	}
}
