package ig

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	apiHost  = "i.instagram.com"
	caaHost  = "b.i.instagram.com"
	appID    = "567067343352427"
	formType = "application/x-www-form-urlencoded; charset=UTF-8"
)

var retryDelays = []time.Duration{2 * time.Second, 4 * time.Second, 8 * time.Second}

type Client struct {
	mu       sync.Mutex
	s        *Session
	path     string
	http     *http.Client
	web      *http.Client
	password string
	csrf     string
	clientIP string
	aac      string

	usdidCache   string
	usdidExpires int64
}

func New(s *Session, path string) *Client {
	h2 := new(http.Protocols)
	h2.SetHTTP2(true)
	api := &http.Transport{
		Proxy:           http.ProxyFromEnvironment,
		Protocols:       h2,
		TLSClientConfig: &tls.Config{CurvePreferences: []tls.CurveID{tls.X25519MLKEM768, tls.X25519, tls.CurveP256, tls.CurveP384}},
	}
	web := &http.Transport{
		Proxy:           http.ProxyFromEnvironment,
		TLSClientConfig: &tls.Config{NextProtos: []string{"http/1.1"}},
		TLSNextProto:    map[string]func(string, *tls.Conn) http.RoundTripper{},
	}
	return &Client{
		s:    s,
		path: path,
		http: &http.Client{Timeout: 30 * time.Second, Transport: api},
		web:  &http.Client{Timeout: 30 * time.Second, Transport: web},
	}
}

func (c *Client) LoggedIn() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.s.LoggedIn()
}

func (c *Client) UserID() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.s.UserID
}

func (c *Client) Username() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.s.Username
}

func (c *Client) Save() error {
	return c.save()
}

func (c *Client) ClearAuth() error {
	c.mu.Lock()
	c.s.Authorization = ""
	c.s.UserID = ""
	c.s.Cookies = map[string]string{}
	c.mu.Unlock()
	return c.save()
}

func (c *Client) save() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.s.Save(c.path)
}

type APIError struct {
	StatusCode int
	Message    string
	ErrorType  string
	Body       []byte
}

func (e *APIError) Error() string {
	msg := e.Message
	if msg == "" {
		msg = e.ErrorType
	}
	if msg == "" {
		msg = http.StatusText(e.StatusCode)
	}
	return fmt.Sprintf("instagram: %d %s", e.StatusCode, msg)
}

type RateLimitedError struct {
	IP string
}

func (e *RateLimitedError) Error() string {
	ip := ""
	if e.IP != "" {
		ip = " (IP " + e.IP + ")"
	}
	return "Instagram is rate-limiting this network" + ip + ". Wait a few minutes, then retry."
}

func (c *Client) throttled(err error) error {
	var e *APIError
	if errors.As(err, &e) && (e.StatusCode == http.StatusTooManyRequests || strings.Contains(e.Message, "Please wait a few minutes")) {
		return &RateLimitedError{IP: c.clientIP}
	}
	return nil
}

func IsLoginRequired(err error) bool {
	var e *APIError
	if !errors.As(err, &e) {
		return false
	}
	return e.Message == "login_required" || e.Message == "user_has_logged_out" || e.StatusCode == http.StatusUnauthorized
}

type request struct {
	method  string
	host    string
	path    string
	rawPath bool
	query   url.Values
	body    string
	headers map[string]string
}

func (c *Client) get(ctx context.Context, endpoint string, query url.Values) ([]byte, error) {
	return c.do(ctx, request{method: http.MethodGet, path: endpoint, query: query})
}

func (c *Client) postForm(ctx context.Context, endpoint string, form url.Values) ([]byte, error) {
	return c.do(ctx, request{method: http.MethodPost, path: endpoint, body: form.Encode()})
}

func (c *Client) postSigned(ctx context.Context, endpoint string, data map[string]any) ([]byte, error) {
	raw, err := compactJSON(data)
	if err != nil {
		return nil, err
	}
	return c.do(ctx, request{method: http.MethodPost, path: endpoint, body: "signed_body=SIGNATURE." + url.QueryEscape(string(raw))})
}

func (c *Client) do(ctx context.Context, r request) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for attempt := 0; ; attempt++ {
		body, status, err := c.send(ctx, r)
		if err != nil {
			return nil, err
		}
		if status >= 500 && attempt < len(retryDelays) {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(retryDelays[attempt]):
			}
			continue
		}
		return body, checkResponse(status, body)
	}
}

func (c *Client) send(ctx context.Context, r request) ([]byte, int, error) {
	host := r.host
	if host == "" {
		host = apiHost
	}
	u := "https://" + host + "/api/v1/" + strings.TrimPrefix(r.path, "/")
	if r.rawPath {
		u = "https://" + host + r.path
	}
	if len(r.query) > 0 {
		u += "?" + r.query.Encode()
	}
	var body io.Reader
	if r.method == http.MethodPost {
		body = strings.NewReader(r.body)
	}
	req, err := http.NewRequestWithContext(ctx, r.method, u, body)
	if err != nil {
		return nil, 0, err
	}
	c.setHeaders(req.Header)
	if r.method == http.MethodPost {
		req.Header["Content-Type"] = []string{formType}
	}
	for k, v := range r.headers {
		req.Header[k] = []string{v}
	}
	verbose := verbosePath(r.path)
	debugLog.Printf("→ %s %s", r.method, req.URL.Path)
	if verbose {
		debugLog.Printf("  request headers:\n%s  request body: %s", formatHeaders(req.Header), redactBody(r.body))
	}
	start := time.Now()
	resp, err := c.http.Do(req)
	if err != nil {
		debugLog.Printf("← %s %s failed after %s: %v", r.method, req.URL.Path, time.Since(start).Round(time.Millisecond), err)
		return nil, 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	c.absorb(resp)
	if ip := resp.Header.Get("X-Fb-Client-Ip-Forwarded"); ip != "" {
		c.clientIP = ip
	}
	data, err := readBody(resp)
	debugLog.Printf("← %d %s %s in %s (%s)", resp.StatusCode, r.method, req.URL.Path, time.Since(start).Round(time.Millisecond), resp.Proto)
	if verbose || resp.StatusCode >= 400 {
		debugLog.Printf("  response headers:\n%s  response body: %s", formatHeaders(resp.Header), redactBody(string(data)))
	}
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return data, resp.StatusCode, nil
}

func (c *Client) setHeaders(h http.Header) {
	s := c.s
	accept := "en-US"
	if lang := strings.ReplaceAll(s.Locale, "_", "-"); lang != "en-US" {
		accept = lang + ", en-US"
	}
	set := map[string]string{
		"Accept":                      "*/*",
		"X-IG-App-Locale":             s.Locale,
		"X-IG-Device-Locale":          s.Locale,
		"X-IG-Mapped-Locale":          s.Locale,
		"X-Pigeon-Session-Id":         "UFS-" + newUUID() + "-1",
		"X-Pigeon-Rawclienttime":      strconv.FormatFloat(float64(time.Now().UnixMilli())/1000, 'f', 3, 64),
		"X-IG-Bandwidth-Speed-KBPS":   pyFloat(float64(2500000+rand.IntN(500001)) / 1000),
		"X-IG-Bandwidth-TotalBytes-B": strconv.Itoa(5000000 + rand.IntN(85000001)),
		"X-IG-Bandwidth-TotalTime-MS": strconv.Itoa(2000 + rand.IntN(7001)),
		"X-IG-App-Startup-Country":    strings.ToUpper(s.Country),
		"X-Bloks-Version-Id":          s.Device.BloksVersionID,
		"X-IG-WWW-Claim":              "0",
		"X-Bloks-Is-Layout-RTL":       "false",
		"X-Bloks-Is-Panorama-Enabled": "true",
		"X-IG-Device-ID":              s.UUID,
		"X-IG-Family-Device-ID":       s.PhoneID,
		"X-IG-Android-ID":             s.AndroidDeviceID,
		"X-IG-Timezone-Offset":        strconv.Itoa(s.TimezoneOffset),
		"X-IG-Connection-Type":        "WIFI",
		"X-IG-Capabilities":           "3brTv10=",
		"X-IG-App-ID":                 appID,
		"Priority":                    "u=3",
		"User-Agent":                  s.userAgent(),
		"Accept-Language":             accept,
		"X-MID":                       s.Mid,
		"Accept-Encoding":             "gzip, deflate",
		"X-FB-HTTP-Engine":            "Tigon/MNS/TCP",
		"X-Tigon-Is-Retry":            "False",
		"X-Zero-Balance":              "INIT",
		"X-Zero-State":                "unknown",
		"Zero-HTTP-Network-Interface": "wifi",
		"X-FB-Client-IP":              "True",
		"X-FB-Server-Cluster":         "True",
		"IG-INTENDED-USER-ID":         "0",
		"X-IG-Nav-Chain":              "9MV:self_profile:2,ProfileMediaTabFragment:self_profile:3,9Xf:self_following:4",
		"X-IG-SALT-IDS":               strconv.Itoa(1061162222 + rand.IntN(100001)),
	}
	if s.UserID != "" {
		next := strconv.FormatInt(time.Now().Unix()+31536000, 10)
		set["IG-INTENDED-USER-ID"] = s.UserID
		set["IG-U-DS-USER-ID"] = s.UserID
		set["IG-U-IG-DIRECT-REGION-HINT"] = "LLA," + s.UserID + "," + next + ":01f7bae7d8b131877d8e0ae1493252280d72f6d0d554447cb1dc9049b6b2c507c08605b7"
		set["IG-U-SHBID"] = "12695," + s.UserID + "," + next + ":01f778d9c9f7546cf3722578fbf9b85143cd6e5132723e5c93f40f55ca0459c8ef8a0d9f"
		set["IG-U-SHBTS"] = strconv.FormatInt(time.Now().Unix(), 10) + "," + s.UserID + "," + next + ":01f7ace11925d0388080078d0282b75b8059844855da27e23c90a362270fddfb3fae7e28"
		set["IG-U-RUR"] = "RVA," + s.UserID + "," + next + ":01f7f627f9ae4ce2874b2e04463efdb184340968b1b006fa88cb4cc69a942a04201e544c"
	}
	if s.IgURur != "" {
		set["IG-U-RUR"] = s.IgURur
	}
	if s.WWWClaim != "" {
		set["X-IG-WWW-Claim"] = s.WWWClaim
	}
	if s.Authorization != "" {
		set["Authorization"] = s.Authorization
	}
	if s.USDID.PrivateKey != "" {
		if v, err := c.usdidHeader(); err == nil {
			set["X-Meta-Usdid"] = v
		}
	}
	for k, v := range set {
		if v != "" {
			h[k] = []string{v}
		}
	}
	h["X-Zero-Eh"] = []string{""}
	if cookie := cookieHeader(s.Cookies); cookie != "" {
		h["Cookie"] = []string{cookie}
	}
}

func (c *Client) absorb(resp *http.Response) {
	s := c.s
	for _, ck := range resp.Cookies() {
		if ck.MaxAge < 0 || ck.Value == "" || ck.Value == `""` {
			delete(s.Cookies, ck.Name)
			continue
		}
		s.Cookies[ck.Name] = ck.Value
	}
	if id, err := strconv.Atoi(resp.Header.Get("ig-set-password-encryption-key-id")); err == nil {
		if pub := resp.Header.Get("ig-set-password-encryption-pub-key"); pub != "" {
			s.PasswordKeyID, s.PasswordPubKey = id, pub
		}
	}
	if mid := resp.Header.Get("ig-set-x-mid"); mid != "" {
		s.Mid = mid
	}
	if auth := resp.Header.Get("ig-set-authorization"); auth != "" {
		if id := authorizationUserID(auth); id != "" {
			s.Authorization = auth
			s.UserID = id
		}
	}
}

func authorizationUserID(auth string) string {
	i := strings.LastIndex(auth, ":")
	if i < 0 || i == len(auth)-1 {
		return ""
	}
	raw, err := base64.StdEncoding.DecodeString(auth[i+1:])
	if err != nil {
		return ""
	}
	var data struct {
		DSUserID string `json:"ds_user_id"`
	}
	if json.Unmarshal(raw, &data) != nil {
		return ""
	}
	return data.DSUserID
}

func readBody(resp *http.Response) ([]byte, error) {
	var r io.Reader = resp.Body
	switch strings.ToLower(resp.Header.Get("Content-Encoding")) {
	case "gzip":
		gz, err := gzip.NewReader(resp.Body)
		if err != nil {
			return nil, err
		}
		defer func() { _ = gz.Close() }()
		r = gz
	case "deflate":
		raw, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, err
		}
		if zr, err := zlib.NewReader(bytes.NewReader(raw)); err == nil {
			defer func() { _ = zr.Close() }()
			r = zr
		} else {
			r = flate.NewReader(bytes.NewReader(raw))
		}
	}
	return io.ReadAll(r)
}

func checkResponse(status int, body []byte) error {
	var parsed struct {
		Status    string `json:"status"`
		Message   string `json:"message"`
		ErrorType string `json:"error_type"`
	}
	jsonErr := json.Unmarshal(body, &parsed)
	if status >= 400 || parsed.Status == "fail" {
		return &APIError{StatusCode: status, Message: parsed.Message, ErrorType: parsed.ErrorType, Body: body}
	}
	if jsonErr != nil {
		return fmt.Errorf("instagram: invalid JSON response (%d): %w", status, jsonErr)
	}
	return nil
}

func cookieHeader(cookies map[string]string) string {
	parts := make([]string, 0, len(cookies))
	for k, v := range cookies {
		parts = append(parts, k+"="+v)
	}
	return strings.Join(parts, "; ")
}

func pyFloat(f float64) string {
	s := strconv.FormatFloat(f, 'f', -1, 64)
	if !strings.Contains(s, ".") {
		s += ".0"
	}
	return s
}

func (c *Client) token() string {
	if t := c.s.Cookies["csrftoken"]; t != "" {
		return t
	}
	if c.csrf == "" {
		c.csrf = randomToken(64)
	}
	return c.csrf
}
