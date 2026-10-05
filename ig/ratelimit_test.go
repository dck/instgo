package ig

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type throttleStub struct{ calls []string }

func (s *throttleStub) RoundTrip(r *http.Request) (*http.Response, error) {
	s.calls = append(s.calls, r.URL.Path)
	h := http.Header{}
	h.Set("X-Fb-Client-Ip-Forwarded", "203.0.113.7")
	body := `{"message":"Please wait a few minutes before you try again.","require_login":true,"status":"fail"}`
	return &http.Response{StatusCode: 401, Header: h, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
}

func TestLoginStopsOnThrottledLauncherSync(t *testing.T) {
	stub := &throttleStub{}
	c := New(NewSession(), t.TempDir()+"/s.json")
	c.http.Transport = stub
	err := c.Login(context.Background(), "someone", "pw")
	var rl *RateLimitedError
	if !errors.As(err, &rl) || rl.IP != "203.0.113.7" {
		t.Fatalf("err = %v", err)
	}
	if len(stub.calls) != 1 {
		t.Fatalf("expected a single request, got %v", stub.calls)
	}
}
