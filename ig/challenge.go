package ig

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const challengeWait = 3 * time.Second

type Challenge struct {
	Destination string
	Done        bool
	path        string
	web         bool
	webURL      string
	webCookies  map[string]string
}

func (c *Client) StartChallenge(ctx context.Context, cr *ChallengeRequired) (*Challenge, error) {
	path := strings.TrimPrefix(cr.APIPath, "/")
	query := url.Values{}
	if parts := strings.Split(strings.Trim(cr.APIPath, "/"), "/"); len(parts) >= 3 {
		challengeCtx := cr.Context
		if challengeCtx == "" {
			uid, _ := strconv.ParseInt(parts[1], 10, 64)
			raw, _ := json.Marshal(map[string]any{"step_name": "", "nonce_code": parts[2], "user_id": uid, "is_stateless": false})
			challengeCtx = string(raw)
		}
		query.Set("guid", c.s.UUID)
		query.Set("device_id", c.s.AndroidDeviceID)
		query.Set("challenge_context", challengeCtx)
	}
	body, err := c.get(ctx, path, query)
	if err != nil {
		var e *APIError
		if errors.As(err, &e) && e.Message == "challenge_required" {
			return c.startWebChallenge(ctx, cr.APIPath)
		}
		return nil, err
	}
	return c.challengeStep(ctx, path, body, 0)
}

func (c *Client) challengeStep(ctx context.Context, path string, body []byte, depth int) (*Challenge, error) {
	var st struct {
		StepName string         `json:"step_name"`
		StepData map[string]any `json:"step_data"`
		Action   string         `json:"action"`
	}
	if err := json.Unmarshal(body, &st); err != nil {
		return nil, err
	}
	ch := &Challenge{path: path}
	switch st.StepName {
	case "delta_login_review", "scraping_warning":
		_, err := c.postSigned(ctx, path, map[string]any{"choice": "0"})
		ch.Done = true
		return ch, err
	case "select_verify_method":
		if depth > 0 {
			return nil, errors.New("verification method selection loop")
		}
		choice := 1
		if _, ok := st.StepData["email"]; !ok {
			if _, ok := st.StepData["phone_number"]; !ok {
				return nil, errors.New("no email or phone available for verification")
			}
			choice = 0
		}
		next, err := c.postSigned(ctx, path, map[string]any{"choice": choice})
		if err != nil {
			return nil, err
		}
		return c.challengeStep(ctx, path, next, depth+1)
	case "verify_email", "verify_email_code", "verify_code":
		for _, k := range []string{"contact_point", "email", "phone_number"} {
			if v, ok := st.StepData[k].(string); ok && v != "" {
				ch.Destination = v
				break
			}
		}
		return ch, nil
	case "":
		if st.Action == "close" {
			ch.Done = true
			return ch, nil
		}
	}
	return nil, fmt.Errorf("unsupported verification step %q, log in with the Instagram app once", st.StepName)
}

func (c *Client) SubmitChallengeCode(ctx context.Context, ch *Challenge, code string) error {
	code = strings.TrimSpace(code)
	if ch.web {
		if err := c.submitWebCode(ctx, ch, code); err != nil {
			return err
		}
		return c.ResumeLogin(ctx)
	}
	body, err := c.postSigned(ctx, ch.path, map[string]any{"security_code": code})
	if err != nil {
		var e *APIError
		if errors.As(err, &e) && e.StatusCode == http.StatusBadRequest {
			return ErrWrongCode
		}
		return err
	}
	if c.LoggedIn() {
		return c.completeLogin(ctx, body)
	}
	return c.ResumeLogin(ctx)
}

func (c *Client) startWebChallenge(ctx context.Context, apiPath string) (*Challenge, error) {
	ch := &Challenge{web: true, webURL: "https://" + apiHost + apiPath, webCookies: map[string]string{}}
	for _, k := range []string{"mid", "csrftoken"} {
		if v := c.s.Cookies[k]; v != "" {
			ch.webCookies[k] = v
		}
	}
	if _, err := c.webDo(ctx, ch, ch.webURL, nil); err != nil {
		return nil, err
	}
	choice := "1"
	res, err := c.webDo(ctx, ch, ch.webURL, url.Values{"choice": {choice}})
	if err != nil {
		return nil, err
	}
	for range 3 {
		form := webForm(res)
		switch form.ChallengeType {
		case "VerifyEmailCodeForm", "VerifySMSCodeForm", "VerifySMSCodeFormForSMSCaptcha":
			ch.Destination = form.Fields.ContactPoint
			return ch, nil
		case "SelectContactPointRecoveryForm":
			if choice == "0" {
				return nil, errors.New("instagram asks for account recovery, log in with the Instagram app once")
			}
			choice = "0"
			res, err = c.webDo(ctx, ch, ch.webURL, url.Values{"choice": {choice}})
		case "SubmitPhoneNumberForm":
			res, err = c.webDo(ctx, ch, ch.webURL, url.Values{"phone_number": {form.Fields.PhoneNumber}, "challenge_context": {form.ChallengeContext}})
		case "RecaptchaChallengeForm":
			return nil, errors.New("instagram requires a captcha, log in with the Instagram app once")
		default:
			if form.Type == "CHALLENGE_REDIRECTION" {
				ch.Done = true
				return ch, nil
			}
			return nil, fmt.Errorf("unsupported verification form %q, log in with the Instagram app once", form.ChallengeType)
		}
		if err != nil {
			return nil, err
		}
	}
	return nil, errors.New("verification did not reach the code step")
}

func (c *Client) submitWebCode(ctx context.Context, ch *Challenge, code string) error {
	res, err := c.webDo(ctx, ch, ch.webURL, url.Values{"security_code": {code}})
	if err != nil {
		return err
	}
	form := webForm(res)
	if len(form.Errors) > 0 && strings.Contains(form.Errors[0], "check the code") {
		return ErrWrongCode
	}
	switch {
	case form.Type == "CHALLENGE_REDIRECTION":
		return nil
	case form.ChallengeType == "LegacyForceSetNewPasswordForm":
		return errors.New("instagram requires a password change, do it in the Instagram app")
	case form.ChallengeType == "ReviewContactPointChangeForm":
		enc := "#PWD_INSTAGRAM_BROWSER:0:" + strconv.FormatInt(time.Now().Unix(), 10) + ":"
		res, err = c.webDo(ctx, ch, "https://"+apiHost+form.Navigation.Forward, url.Values{
			"choice":            {"0"},
			"enc_new_password1": {enc},
			"new_password1":     {""},
			"enc_new_password2": {enc},
			"new_password2":     {""},
		})
		if err != nil {
			return err
		}
		if webForm(res).Type != "CHALLENGE_REDIRECTION" {
			return errors.New("verification was not accepted")
		}
		return nil
	}
	if len(form.Errors) > 0 {
		return errors.New(form.Errors[0])
	}
	return nil
}

type webChallengeForm struct {
	Type             string   `json:"type"`
	ChallengeType    string   `json:"challengeType"`
	ChallengeContext string   `json:"challenge_context"`
	Errors           []string `json:"errors"`
	Fields           struct {
		ContactPoint string `json:"contact_point"`
		PhoneNumber  string `json:"phone_number"`
	} `json:"fields"`
	Navigation struct {
		Forward string `json:"forward"`
	} `json:"navigation"`
	Challenge *webChallengeForm `json:"challenge"`
}

func webForm(body []byte) webChallengeForm {
	var f webChallengeForm
	_ = json.Unmarshal(body, &f)
	if f.Challenge != nil {
		return *f.Challenge
	}
	return f
}

func (c *Client) webDo(ctx context.Context, ch *Challenge, target string, form url.Values) ([]byte, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(challengeWait):
	}
	method := http.MethodGet
	var body io.Reader
	if form != nil {
		method = http.MethodPost
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return nil, err
	}
	h := req.Header
	h.Set("User-Agent", "Mozilla/5.0 (Linux; Android 8.0.0; MI 5s Build/OPR1.170623.032; wv) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/80.0.3987.149 Mobile Safari/537.36 "+c.s.userAgent())
	h.Set("Accept-Encoding", "gzip, deflate")
	h.Set("Accept-Language", "en-US,en;q=0.9,en-US;q=0.8,en;q=0.7")
	h.Set("Pragma", "no-cache")
	h.Set("Cache-Control", "no-cache")
	if method == http.MethodGet {
		h.Set("Upgrade-Insecure-Requests", "1")
		h.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.9")
		h.Set("X-Requested-With", "com.instagram.android")
		h.Set("Sec-Fetch-Dest", "document")
		h.Set("Sec-Fetch-Site", "none")
		h.Set("Sec-Fetch-Mode", "navigate")
		h.Set("Sec-Fetch-User", "?1")
	} else {
		sum := sha256.Sum256([]byte("#PWD_INSTAGRAM_BROWSER:0:" + strconv.FormatInt(time.Now().Unix(), 10) + ":"))
		h.Set("Content-Type", "application/x-www-form-urlencoded")
		h.Set("Accept", "*/*")
		h.Set("X-IG-WWW-Claim", "0")
		h.Set("X-Instagram-AJAX", hex.EncodeToString(sum[:])[:12])
		h.Set("X-Requested-With", "XMLHttpRequest")
		h.Set("X-CSRFToken", ch.webCookies["csrftoken"])
		h.Set("X-IG-App-ID", appID)
		h.Set("Sec-Fetch-Dest", "empty")
		h.Set("Sec-Fetch-Site", "same-origin")
		h.Set("Sec-Fetch-Mode", "cors")
		h.Set("Referer", ch.webURL)
	}
	if cookie := cookieHeader(ch.webCookies); cookie != "" {
		h.Set("Cookie", cookie)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	for _, ck := range resp.Cookies() {
		if ck.Value != "" && ck.Value != `""` {
			ch.webCookies[ck.Name] = ck.Value
		}
	}
	data, err := readBody(resp)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests {
		return nil, &APIError{StatusCode: resp.StatusCode, Body: data}
	}
	return data, nil
}
