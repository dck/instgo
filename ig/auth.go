package ig

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"net/url"
	"regexp"
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
	err := c.caaLogin(ctx)
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
	tray := obj{
		{"supported_capabilities_new", []obj{
			{{"value", "119.0,120.0,121.0,122.0,123.0,124.0,125.0,126.0,127.0,128.0,129.0,130.0,131.0,132.0,133.0,134.0,135.0,136.0,137.0,138.0,139.0,140.0,141.0,142.0"}, {"name", "SUPPORTED_SDK_VERSIONS"}},
			{{"value", "14"}, {"name", "FACE_TRACKER_VERSION"}},
			{{"value", "ETC2_COMPRESSION"}, {"name", "COMPRESSION"}},
			{{"value", "gyroscope_enabled"}, {"name", "gyroscope"}},
		}},
		{"reason", "cold_start"},
		{"timezone_offset", strconv.Itoa(s.TimezoneOffset)},
		{"tray_session_id", s.TraySessionID},
		{"request_id", s.RequestID},
		{"page_size", 50},
		{"_uuid", s.UUID},
		{"reel_tray_impressions", obj{}},
	}
	_, _ = c.do(ctx, request{method: http.MethodPost, path: "feed/reels_tray/", body: "signed_body=SIGNATURE." + url.QueryEscape(dumps(tray))})
	now := strconv.FormatInt(time.Now().UnixMilli(), 10)
	feed := obj{
		{"app_start_time", now},
		{"has_camera_permission", "1"},
		{"feed_view_info", "[]"},
		{"client_recorded_request_time_ms", now},
		{"client_seen_store_media_list", ""},
		{"client_view_state_media_list", "[]"},
		{"device_timezone_name", timezoneName(s.TimezoneOffset)},
		{"feed_reshare_info", ""},
		{"phone_id", s.PhoneID},
		{"reason", []string{"cold_start_fetch"}},
		{"battery_level", 100},
		{"timezone_offset", strconv.Itoa(s.TimezoneOffset)},
		{"device_id", s.UUID},
		{"include_attribution_ui_data", "true"},
		{"push_disabled", "true"},
		{"request_id", s.RequestID},
		{"request_build_time", now},
		{"_uuid", s.UUID},
		{"is_charging", rand.IntN(2)},
		{"is_dark_mode", 1},
		{"will_sound_on", rand.IntN(2)},
		{"session_id", s.ClientSessionID},
		{"session_level_signals", sessionLevelSignals},
		{"bloks_versioning_id", s.Device.BloksVersionID},
		{"is_pull_to_refresh", "0"},
	}
	_, _ = c.do(ctx, request{
		method: http.MethodPost,
		path:   "feed/timeline/",
		body:   pyDumps(feed),
		headers: map[string]string{
			"X-Ads-Opt-Out":       "0",
			"X-DEVICE-ID":         s.UUID,
			"X-CM-Bandwidth-KBPS": "-1.000",
			"X-CM-Latency":        strconv.Itoa(1 + rand.IntN(5)),
		},
	})
}

const sessionLevelSignals = `{"time_since_current_surface_session_start":0,"time_since_fg_session_start":0,"time_since_last_background":0,"num_ad_seen_current_surface_current_session":0,"app_entry":"normal","last_surfaces_visited_current_session":[],"video_play_count":0,"video_pause_count":0,"video_dwell_time_sum":0,"video_dwell_time_max":0,"video_view_count":0,"video_intentional_audio_on":0,"video_intentional_audio_off":0,"video_audio_on_count":0,"feed_to_reels_iv_entry":0,"time_since_last_ad_click":-1,"time_since_last_ad_like":-1,"time_since_last_organic_like":-1,"time_since_last_like":-1,"time_since_last_organic_business_profile_visit":-1,"time_since_last_ad_imp":-1,"time_since_last_search":-1,"time_since_last_organic_engagement_event":-1,"time_since_last_ad_profile_visit":-1,"time_since_last_ad_cta":-1,"time_since_last_ad_caption_more_click":-1,"time_since_last_ad_comment_button":-1,"time_since_last_ad_share":-1,"time_since_last_ad_media_tap":-1,"time_since_last_ad_gesture":-1,"time_since_last_search_result_click":-1,"time_since_last_serp_click":-1,"time_since_last_organic_share":-1,"time_since_last_organic_comment":-1,"time_since_last_organic_caption_click":-1,"time_since_last_organic_media_tap":-1,"time_since_last_organic_gesture":-1,"num_search_clicks_current_session":0}`

func timezoneName(offset int) string {
	sign := "+"
	if offset < 0 {
		sign, offset = "-", -offset
	}
	return fmt.Sprintf("GMT%s%02d:%02d", sign, offset/3600, offset%3600/60)
}

func pyDumps(o obj) string {
	parts := make([]string, len(o))
	for i, p := range o {
		parts[i] = dumps(p.k) + ": " + dumps(p.v)
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

func jazoest(s string) string {
	sum := 0
	for _, r := range s {
		sum += int(r)
	}
	return "2" + strconv.Itoa(sum)
}

var sessionIDPrefix = regexp.MustCompile(`^\d+`)

func (c *Client) LoginBySessionID(ctx context.Context, sessionID string) error {
	sessionID = strings.TrimSpace(sessionID)
	if decoded, err := url.QueryUnescape(sessionID); err == nil {
		sessionID = url.QueryEscape(decoded)
	}
	userID := sessionIDPrefix.FindString(sessionID)
	if len(sessionID) <= 30 || userID == "" {
		return errors.New("that does not look like an Instagram sessionid cookie")
	}
	auth := dumps(obj{{"ds_user_id", userID}, {"sessionid", sessionID}, {"should_use_header_over_cookies", true}})
	c.mu.Lock()
	c.s.Cookies = map[string]string{"sessionid": sessionID, "ds_user_id": userID}
	c.s.Authorization = "Bearer IGT:2:" + base64.StdEncoding.EncodeToString([]byte(auth))
	c.s.UserID = userID
	c.mu.Unlock()
	debugLog.Printf("login by browser session start user_id=%s", userID)
	body, err := c.get(ctx, "users/"+userID+"/info/", nil)
	if err != nil {
		debugLog.Printf("login by browser session result: %v", err)
		_ = c.ClearAuth()
		if rl := c.throttled(err); rl != nil {
			return rl
		}
		if IsLoginRequired(err) {
			return errors.New("instagram rejected that session, log in again in Firefox and copy a fresh sessionid")
		}
		return err
	}
	var r struct {
		User struct {
			Username string `json:"username"`
		} `json:"user"`
	}
	_ = json.Unmarshal(body, &r)
	c.mu.Lock()
	c.s.Username = r.User.Username
	c.mu.Unlock()
	debugLog.Printf("login by browser session result: ok username=%s", r.User.Username)
	return c.save()
}
