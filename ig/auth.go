package ig

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"strconv"
	"strings"
	"time"
)

var ErrWrongCode = errors.New("wrong code, try again")

type TwoFactorRequired struct {
	Identifier string
	TOTP       bool
	Phone      string
}

func (e *TwoFactorRequired) Error() string { return "two-factor authentication required" }

type ChallengeRequired struct {
	APIPath string
	Context string
}

func (e *ChallengeRequired) Error() string { return "instagram requires verification" }

func (c *Client) Login(ctx context.Context, username, password string) error {
	c.mu.Lock()
	c.s.Username = username
	c.password = password
	c.mu.Unlock()
	debugLog.Printf("login start user=%s device=%s uuid=%s", username, c.s.AndroidDeviceID, c.s.UUID)
	_, err := c.postSigned(ctx, "launcher/sync/", map[string]any{
		"id":                      c.s.UUID,
		"server_config_retrieval": "1",
	})
	if rl := c.throttled(err); rl != nil {
		err = rl
	} else {
		err = c.ResumeLogin(ctx)
	}
	debugLog.Printf("login result: %v", err)
	if saveErr := c.save(); saveErr != nil {
		debugLog.Printf("save session: %v", saveErr)
	}
	return err
}

func (c *Client) ResumeLogin(ctx context.Context) error {
	keyID, key, err := c.passwordKey(ctx)
	if err != nil {
		return err
	}
	enc, err := encryptPassword(c.password, keyID, key, time.Now())
	if err != nil {
		return err
	}
	s := c.s
	body, err := c.postSigned(ctx, "accounts/login/", map[string]any{
		"jazoest":             jazoest(s.PhoneID),
		"country_codes":       fmt.Sprintf(`[{"country_code":"%d","source":["default"]}]`, s.CountryCode),
		"phone_id":            s.PhoneID,
		"enc_password":        enc,
		"username":            s.Username,
		"adid":                s.AdvertisingID,
		"guid":                s.UUID,
		"device_id":           s.AndroidDeviceID,
		"google_tokens":       "[]",
		"login_attempt_count": "0",
	})
	if err != nil {
		if rl := c.throttled(err); rl != nil {
			return rl
		}
		return loginError(err)
	}
	return c.completeLogin(ctx, body)
}

func (c *Client) TwoFactorLogin(ctx context.Context, tf *TwoFactorRequired, code string) error {
	method := "1"
	if tf.TOTP {
		method = "3"
	}
	s := c.s
	body, err := c.postSigned(ctx, "accounts/two_factor_login/", map[string]any{
		"verification_code":     strings.TrimSpace(code),
		"phone_id":              s.PhoneID,
		"_csrftoken":            c.token(),
		"two_factor_identifier": tf.Identifier,
		"username":              s.Username,
		"trust_this_device":     "0",
		"guid":                  s.UUID,
		"device_id":             s.AndroidDeviceID,
		"waterfall_id":          newUUID(),
		"verification_method":   method,
	})
	if err != nil {
		lerr := loginError(err)
		var e *APIError
		if errors.As(lerr, &e) && e.StatusCode == 400 {
			return ErrWrongCode
		}
		return lerr
	}
	return c.completeLogin(ctx, body)
}

func loginError(err error) error {
	var e *APIError
	if !errors.As(err, &e) {
		return err
	}
	var b struct {
		Message       string `json:"message"`
		ErrorType     string `json:"error_type"`
		TwoFactorInfo *struct {
			Identifier string `json:"two_factor_identifier"`
			TOTP       bool   `json:"totp_two_factor_on"`
			Phone      string `json:"obfuscated_phone_number"`
		} `json:"two_factor_info"`
		Challenge *struct {
			APIPath string `json:"api_path"`
			Context string `json:"challenge_context"`
		} `json:"challenge"`
	}
	_ = json.Unmarshal(e.Body, &b)
	switch {
	case b.TwoFactorInfo != nil:
		return &TwoFactorRequired{Identifier: b.TwoFactorInfo.Identifier, TOTP: b.TwoFactorInfo.TOTP, Phone: b.TwoFactorInfo.Phone}
	case b.Challenge != nil && b.Challenge.APIPath != "":
		return &ChallengeRequired{APIPath: b.Challenge.APIPath, Context: b.Challenge.Context}
	case b.ErrorType == "bad_password":
		return errors.New("incorrect password")
	case b.ErrorType == "invalid_user":
		return errors.New("user not found")
	}
	return err
}

func (c *Client) completeLogin(ctx context.Context, body []byte) error {
	var r struct {
		LoggedInUser struct {
			PK       FlexString `json:"pk"`
			Username string     `json:"username"`
		} `json:"logged_in_user"`
	}
	_ = json.Unmarshal(body, &r)
	c.mu.Lock()
	if r.LoggedInUser.PK != "" {
		c.s.UserID = string(r.LoggedInUser.PK)
	}
	if r.LoggedInUser.Username != "" {
		c.s.Username = r.LoggedInUser.Username
	}
	ok := c.s.LoggedIn()
	c.mu.Unlock()
	if !ok {
		return errors.New("login succeeded but no session was issued")
	}
	c.postLoginFlow(ctx)
	return c.save()
}

func (c *Client) postLoginFlow(ctx context.Context) {
	s := c.s
	_, _ = c.postSigned(ctx, "feed/reels_tray/", map[string]any{
		"supported_capabilities_new": []map[string]string{
			{"value": "119.0,120.0,121.0,122.0,123.0,124.0,125.0,126.0,127.0,128.0,129.0,130.0,131.0,132.0,133.0,134.0,135.0,136.0,137.0,138.0,139.0,140.0,141.0,142.0", "name": "SUPPORTED_SDK_VERSIONS"},
			{"value": "14", "name": "FACE_TRACKER_VERSION"},
			{"value": "ETC2_COMPRESSION", "name": "COMPRESSION"},
			{"value": "gyroscope_enabled", "name": "gyroscope"},
		},
		"reason":                "cold_start",
		"timezone_offset":       strconv.Itoa(s.TimezoneOffset),
		"tray_session_id":       s.TraySessionID,
		"request_id":            s.RequestID,
		"page_size":             50,
		"_uuid":                 s.UUID,
		"reel_tray_impressions": map[string]string{},
	})
	feed, _ := json.Marshal(map[string]any{
		"has_camera_permission": "1",
		"feed_view_info":        "[]",
		"phone_id":              s.PhoneID,
		"reason":                "cold_start_fetch",
		"battery_level":         100,
		"timezone_offset":       strconv.Itoa(s.TimezoneOffset),
		"device_id":             s.UUID,
		"request_id":            s.RequestID,
		"_uuid":                 s.UUID,
		"is_charging":           rand.IntN(2),
		"is_dark_mode":          1,
		"will_sound_on":         rand.IntN(2),
		"session_id":            s.ClientSessionID,
		"bloks_versioning_id":   bloksVersionID,
		"is_pull_to_refresh":    "0",
	})
	_, _ = c.do(ctx, request{
		method: "POST",
		path:   "feed/timeline/",
		body:   string(feed),
		headers: map[string]string{
			"X-Ads-Opt-Out":       "0",
			"X-DEVICE-ID":         s.UUID,
			"X-CM-Bandwidth-KBPS": "-1.000",
			"X-CM-Latency":        strconv.Itoa(1 + rand.IntN(5)),
		},
	})
}

func jazoest(s string) string {
	sum := 0
	for _, r := range s {
		sum += int(r)
	}
	return "2" + strconv.Itoa(sum)
}
