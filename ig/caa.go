package ig

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"
)

const (
	offlineGroup       = "caa_iteration_v3_perf_ig_4"
	homepageGroup      = "Deploy: Not in Experiment"
	qplMarker          = 36707139
	ap2svEntrypoint    = "com.bloks.www.ap.two_step_verification.entrypoint_async"
	ap2svCodeEntry     = "com.bloks.www.ap.two_step_verification.code_entry"
	ap2svCodeEntrySend = "com.bloks.www.ap.two_step_verification.code_entry_async"
)

var backupCode = regexp.MustCompile(`^\d{8}$`)

type CodeRequired struct {
	TwoFactor     bool
	submitContext string
	twoStep       string
	result        map[string]any
}

func (e *CodeRequired) Error() string {
	if e.TwoFactor {
		return "two-factor code required"
	}
	return "Instagram sent a security code to your email"
}

func (c *Client) mid() any {
	if c.s.Mid == "" {
		return nil
	}
	return c.s.Mid
}

func (c *Client) bloksForm(params any) string {
	bv := c.s.Device.BloksVersionID
	return form{
		{"params", dumps(params)},
		{"_uuid", c.s.UUID},
		{"bk_client_context", dumps(obj{{"bloks_version", bv}, {"styles_id", "instagram"}})},
		{"bloks_versioning_id", bv},
	}.encode()
}

func (c *Client) bloksCall(ctx context.Context, host, path string, params any, headers map[string]string) (map[string]any, error) {
	body, err := c.do(ctx, request{method: http.MethodPost, host: host, path: path, body: c.bloksForm(params), headers: headers})
	if err != nil {
		if rl := c.throttled(err); rl != nil {
			return nil, rl
		}
		return nil, loginError(err)
	}
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) bloksAsync(ctx context.Context, host, action string, params any, extra map[string]string) (map[string]any, error) {
	path := "bloks/async_action/" + action + "/"
	headers := map[string]string{"X-FB-Friendly-Name": "IgApi: " + path}
	for k, v := range extra {
		headers[k] = v
	}
	return c.bloksCall(ctx, host, path, params, headers)
}

func (c *Client) bloksApp(ctx context.Context, host, app string, params any) (map[string]any, error) {
	return c.bloksCall(ctx, host, "bloks/apps/"+app+"/", params, nil)
}

func networkInfo() obj {
	return obj{
		{"active_subscriptions_info", nil},
		{"default_subscription_info", obj{
			{"network_type", nil},
			{"is_data_roaming", 1},
			{"is_esim", nil},
			{"is_gsm_roaming", 0},
			{"is_sim_sms_capable", nil},
			{"is_mobile_data_enabled", 1},
			{"sim_carrier_id", 1},
			{"sim_carrier_id_name", nil},
			{"sim_state", 5},
			{"sim_operator", "310260"},
			{"sim_operator_name", "T-Mobile"},
			{"signal_strength", nil},
			{"group_id_level_1", nil},
			{"network_operator", "310260"},
		}},
		{"is_airplane_mode", 0},
		{"is_active_network_cellular", 0},
		{"is_device_sms_capable", 1},
		{"sim_count", 1},
		{"is_wifi", 1},
	}
}

func (c *Client) caaLogin(ctx context.Context) error {
	if err := c.usdidRegister(ctx); err != nil {
		if rl := c.throttled(err); rl != nil {
			return rl
		}
		return fmt.Errorf("device registration: %w", err)
	}
	s := c.s
	waterfall := newUUID()
	res, err := c.bloksAsync(ctx, caaHost, "com.bloks.www.bloks.caa.login.process_client_data_and_redirect", obj{
		{"is_from_logged_out", false},
		{"logged_out_user", ""},
		{"qpl_join_id", nil},
		{"family_device_id", s.PhoneID},
		{"device_id", s.AndroidDeviceID},
		{"offline_experiment_group", offlineGroup},
		{"waterfall_id", waterfall},
		{"logout_source", ""},
		{"show_internal_settings", false},
		{"last_auto_login_time", 0},
		{"disable_auto_login", false},
		{"qe_device_id", s.UUID},
		{"use_auto_login_interstitial", true},
		{"disable_recursive_auto_login_interstitial", true},
		{"auto_login_interstitial_experiment_group_name", ""},
		{"is_from_logged_in_switcher", false},
		{"switcher_logged_in_uid", ""},
		{"account_list", []any{}},
		{"blocked_uid", []any{}},
		{"INTERNAL_INFRA_THEME", "THREE_NEUTRAL_GRAY"},
		{"layered_homepage_experiment_group", homepageGroup},
		{"launched_url", ""},
		{"sim_phone_numbers", []any{}},
		{"is_from_registration_reminder", false},
	}, nil)
	if err != nil {
		return err
	}
	c.aac = bloksAAC(res)

	nonce, err := c.createKeystore(ctx)
	if err != nil {
		return err
	}
	if c.aac != "" {
		if _, err := c.bloksAsync(ctx, caaHost, "com.bloks.www.caa.login.oauth.token.fetch.async", obj{
			{"client_input_params", obj{
				{"username_input", s.Username},
				{"si_device_param_network_info", networkInfo()},
				{"aac", c.aac},
				{"lois_settings", obj{{"lois_token", ""}}},
				{"cloud_trust_token", nil},
				{"zero_balance_state", ""},
				{"network_bssid", nil},
			}},
			{"server_params", obj{
				{"is_from_logged_out", 0},
				{"layered_homepage_experiment_group", homepageGroup},
				{"device_id", s.AndroidDeviceID},
				{"login_surface", "login_home"},
				{"waterfall_id", waterfall},
				{"INTERNAL__latency_qpl_instance_id", time.Now().UnixMilli()},
				{"is_platform_login", 0},
				{"login_entry_point", "logged_out"},
				{"INTERNAL__latency_qpl_marker_id", qplMarker},
				{"family_device_id", s.PhoneID},
				{"offline_experiment_group", offlineGroup},
				{"access_flow_version", "pre_mt_behavior"},
				{"is_from_logged_in_switcher", 0},
				{"qe_device_id", s.UUID},
			}},
		}, nil); err != nil {
			return err
		}
	}
	if c.aac == "" || nonce == "" {
		return errors.New("instagram did not return the login preflight data (see debug.log)")
	}

	res, err = c.sendLoginRequest(ctx, waterfall, nonce)
	if err != nil {
		return err
	}
	return c.handleLoginResult(ctx, res)
}

func (c *Client) createKeystore(ctx context.Context) (string, error) {
	body, err := c.do(ctx, request{
		method:  http.MethodPost,
		host:    caaHost,
		path:    "attestation/create_android_keystore/",
		body:    form{{"app_scoped_device_id", c.s.UUID}, {"key_hash", ""}}.encode(),
		headers: map[string]string{"X-FB-Friendly-Name": "IgApi: attestation/create_android_keystore/"},
	})
	if err != nil {
		if rl := c.throttled(err); rl != nil {
			return "", rl
		}
		return "", loginError(err)
	}
	var r struct {
		ChallengeNonce string `json:"challenge_nonce"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return "", err
	}
	return r.ChallengeNonce, nil
}

func (c *Client) sendLoginRequest(ctx context.Context, waterfall, nonce string) (map[string]any, error) {
	keyID, key, err := c.passwordKey(ctx)
	if err != nil {
		return nil, err
	}
	enc, err := encryptPassword(c.password, keyID, key, time.Now())
	if err != nil {
		return nil, err
	}
	s := c.s
	inputID := strings.ReplaceAll(newUUID(), "-", "")[:4] + "ig"
	nonASCII := "false"
	for _, r := range c.password {
		if r > 127 {
			nonASCII = "true"
			break
		}
	}
	params := obj{
		{"client_input_params", obj{
			{"blocked_uids", []any{}},
			{"aac", c.aac},
			{"sim_phones", []any{}},
			{"aymh_accounts", []any{}},
			{"network_bssid", nil},
			{"secure_family_device_id", ""},
			{"has_granted_read_contacts_permissions", 0},
			{"auth_secure_device_id", ""},
			{"has_whatsapp_installed", 0},
			{"si_device_param_network_info", networkInfo()},
			{"password", enc},
			{"sso_token_map_json_string", ""},
			{"block_store_machine_id", ""},
			{"ig_vetted_device_nonces", nil},
			{"cloud_trust_token", nil},
			{"event_flow", "login_manual"},
			{"password_contains_non_ascii", nonASCII},
			{"client_known_key_hash", ""},
			{"sso_accounts_auth_data", []any{}},
			{"encrypted_msisdn", ""},
			{"has_granted_read_phone_permissions", 0},
			{"app_manager_id", ""},
			{"should_show_nested_nta_from_aymh", 0},
			{"device_id", s.AndroidDeviceID},
			{"zero_balance_state", ""},
			{"login_attempt_count", 1},
			{"machine_id", c.mid()},
			{"flash_call_permission_status", obj{
				{"READ_PHONE_STATE", "DENIED"},
				{"READ_CALL_LOG", "DENIED"},
				{"ANSWER_PHONE_CALLS", "DENIED"},
			}},
			{"accounts_list", []any{}},
			{"gms_incoming_call_retriever_eligibility", "eligible"},
			{"family_device_id", s.PhoneID},
			{"fb_ig_device_id", []any{}},
			{"device_emails", []any{}},
			{"try_num", 1},
			{"lois_settings", obj{{"lois_token", ""}}},
			{"event_step", "home_page"},
			{"headers_infra_flow_id", ""},
			{"openid_tokens", obj{}},
			{"contact_point", s.Username},
		}},
		{"server_params", obj{
			{"should_trigger_override_login_2fa_action", 0},
			{"is_from_logged_out", 0},
			{"should_trigger_override_login_success_action", 0},
			{"login_credential_type", "none"},
			{"server_login_source", "login"},
			{"waterfall_id", waterfall},
			{"two_step_login_type", "one_step_login"},
			{"login_source", "Login"},
			{"is_platform_login", 0},
			{"login_entry_point", "logged_out"},
			{"INTERNAL__latency_qpl_marker_id", qplMarker},
			{"is_from_aymh", 0},
			{"offline_experiment_group", offlineGroup},
			{"is_from_landing_page", 0},
			{"left_nav_button_action", "NONE"},
			{"password_text_input_id", inputID + ":82"},
			{"is_from_empty_password", 0},
			{"is_from_msplit_fallback", 0},
			{"ar_event_source", "login_home_page"},
			{"qe_device_id", s.UUID},
			{"username_text_input_id", inputID + ":81"},
			{"layered_homepage_experiment_group", homepageGroup},
			{"device_id", s.AndroidDeviceID},
			{"login_surface", "login_home"},
			{"INTERNAL__latency_qpl_instance_id", time.Now().UnixMilli()},
			{"reg_flow_source", "login_home_native_integration_point"},
			{"is_caa_perf_enabled", 1},
			{"credential_type", "password"},
			{"is_from_password_entry_page", 0},
			{"caller", "gslr"},
			{"family_device_id", s.PhoneID},
			{"is_from_assistive_id", 0},
			{"access_flow_version", "pre_mt_behavior"},
			{"is_from_logged_in_switcher", 0},
		}},
	}
	attest := dumps(obj{{"attestation", []obj{{
		{"version", 2},
		{"type", "keystore"},
		{"errors", []int{-1013}},
		{"challenge_nonce", nonce},
		{"signed_nonce", ""},
		{"key_hash", ""},
	}}}})
	return c.bloksAsync(ctx, caaHost, "com.bloks.www.bloks.caa.login.async.send_login_request", params, map[string]string{"X-IG-Attest-Params": attest})
}

func (c *Client) handleLoginResult(ctx context.Context, res map[string]any) error {
	if resp, ok := c.applyLogin(res); ok {
		return c.finishLogin(ctx, resp)
	}
	if strings.Contains(strings.Join(allStrings(res), "\n"), ap2svEntrypoint) {
		return c.startProfileCode(ctx, res)
	}
	if ctxValue := twoStepContext(res); ctxValue != "" {
		return &CodeRequired{TwoFactor: true, twoStep: ctxValue, result: res}
	}
	markers := caaMarkers(res)
	for _, m := range markers {
		if strings.HasPrefix(m, "CAA_LOGIN_FALLBACK:") {
			debugLog.Printf("CAA asked for legacy login fallback: %s", m)
			return c.ResumeLogin(ctx)
		}
	}
	if len(markers) > 0 {
		return fmt.Errorf("login was not accepted (%s)", strings.Join(markers, ", "))
	}
	return errors.New("login was not accepted (see debug.log)")
}

func (c *Client) startProfileCode(ctx context.Context, res map[string]any) error {
	s := c.s
	entry := bloksContextValue(res, ap2svEntrypoint, "context_data")
	if entry == "" {
		return errors.New("verification step is missing its context (see debug.log)")
	}
	entryRes, err := c.bloksAsync(ctx, caaHost, ap2svEntrypoint, obj{
		{"client_input_params", obj{
			{"auth_secure_device_id", ""},
			{"accounts_list", []any{}},
			{"has_whatsapp_installed", 0},
			{"family_device_id", s.PhoneID},
			{"machine_id", c.mid()},
		}},
		{"server_params", obj{
			{"use_open_instead_of_push", 0},
			{"context_data", entry},
			{"INTERNAL__latency_qpl_marker_id", qplMarker},
			{"INTERNAL__latency_qpl_instance_id", time.Now().UnixMilli()},
			{"device_id", s.UUID},
			{"use_close_instead_of_back", 0},
		}},
	}, nil)
	if err != nil {
		return err
	}
	codeCtx := bloksContextValue(entryRes, ap2svCodeEntry, "context_data")
	if codeCtx == "" {
		return errors.New("verification code screen is missing its context (see debug.log)")
	}
	codeRes, err := c.bloksApp(ctx, caaHost, ap2svCodeEntry, obj{
		{"client_input_params", obj{{"aac", c.aac}}},
		{"server_params", obj{
			{"context_data", codeCtx},
			{"show_close_button", 0},
			{"device_id", s.UUID},
			{"INTERNAL_INFRA_screen_id", "generic_code_entry"},
			{"is_dismissable", 1},
		}},
	})
	if err != nil {
		return err
	}
	submit := bloksContextValue(codeRes, ap2svCodeEntrySend, "context_data")
	if submit == "" {
		return errors.New("verification submit step is missing its context (see debug.log)")
	}
	return &CodeRequired{submitContext: submit}
}

func (c *Client) SubmitCode(ctx context.Context, cr *CodeRequired, code string) error {
	code = strings.TrimSpace(code)
	var res map[string]any
	var err error
	if cr.TwoFactor {
		res, err = c.verifyTwoFactor(ctx, cr, code)
	} else {
		res, err = c.bloksAsync(ctx, caaHost, ap2svCodeEntrySend, obj{
			{"client_input_params", obj{
				{"auth_secure_device_id", ""},
				{"aac", c.aac},
				{"code", code},
				{"family_device_id", c.s.PhoneID},
				{"device_id", c.s.AndroidDeviceID},
				{"machine_id", c.mid()},
			}},
			{"server_params", obj{
				{"context_data", cr.submitContext},
				{"INTERNAL__latency_qpl_marker_id", qplMarker},
				{"INTERNAL__latency_qpl_instance_id", time.Now().UnixMilli()},
				{"device_id", c.s.UUID},
			}},
		}, nil)
	}
	if err != nil {
		return err
	}
	resp, ok := c.applyLogin(res)
	if !ok {
		return ErrWrongCode
	}
	return c.finishLogin(ctx, resp)
}

func (c *Client) verifyTwoFactor(ctx context.Context, cr *CodeRequired, code string) (map[string]any, error) {
	s := c.s
	method := "totp"
	switch {
	case backupCode.MatchString(strings.NewReplacer(" ", "", "-", "").Replace(code)):
		method = "backup_codes"
		code = strings.NewReplacer(" ", "", "-", "").Replace(code)
	case loginBool(cr.result, "sms_two_factor_on") && !loginBool(cr.result, "totp_two_factor_on"):
		method = "sms"
	}
	tsv := cr.twoStep
	if _, err := c.bloksApp(ctx, apiHost, "com.bloks.www.two_step_verification.entrypoint", obj{
		{"client_input_params", obj{{"device_id", s.AndroidDeviceID}, {"is_whatsapp_installed", 0}, {"machine_id", c.mid()}}},
		{"server_params", obj{
			{"should_fallback_to_sms", 0},
			{"family_device_id", s.PhoneID},
			{"device_id", s.AndroidDeviceID},
			{"two_step_verification_context", tsv},
			{"flow_source", "two_factor_login"},
		}},
	}); err != nil {
		return nil, err
	}
	if _, err := c.bloksApp(ctx, apiHost, "com.bloks.www.two_step_verification.method_picker", obj{
		{"client_input_params", obj{{"is_whatsapp_installed", 0}}},
		{"server_params", obj{
			{"should_fallback_to_sms", 0},
			{"device_id", s.AndroidDeviceID},
			{"two_step_verification_context", tsv},
			{"flow_source", "two_factor_login"},
		}},
	}); err != nil {
		return nil, err
	}
	if _, err := c.bloksAsync(ctx, apiHost, "com.bloks.www.two_step_verification.method_picker.navigation.async", obj{
		{"client_input_params", obj{{"selected_method", method}, {"cloud_trust_token", nil}, {"network_bssid", nil}}},
		{"server_params", obj{
			{"should_fallback_to_sms", 0},
			{"device_id", s.AndroidDeviceID},
			{"spectra_reg_login_data", nil},
			{"two_step_verification_context", tsv},
			{"flow_source", "two_factor_login"},
		}},
	}, nil); err != nil {
		return nil, err
	}
	if method == "backup_codes" {
		if _, err := c.bloksApp(ctx, apiHost, "com.bloks.www.two_factor_login.enter_backup_code", obj{
			{"server_params", obj{
				{"device_id", s.AndroidDeviceID},
				{"two_step_verification_context", tsv},
				{"flow_source", "two_factor_login"},
			}},
		}); err != nil {
			return nil, err
		}
	}
	return c.bloksAsync(ctx, apiHost, "com.bloks.www.two_step_verification.verify_code.async", obj{
		{"client_input_params", obj{
			{"auth_secure_device_id", ""},
			{"block_store_machine_id", ""},
			{"code", code},
			{"should_trust_device", 1},
			{"family_device_id", s.PhoneID},
			{"device_id", s.AndroidDeviceID},
			{"cloud_trust_token", nil},
			{"network_bssid", nil},
			{"machine_id", c.mid()},
		}},
		{"server_params", obj{
			{"should_fallback_to_sms", 0},
			{"device_id", s.AndroidDeviceID},
			{"spectra_reg_login_data", nil},
			{"challenge", method},
			{"two_step_verification_context", tsv},
			{"flow_source", "two_factor_login"},
		}},
	}, nil)
}

func loginBool(data map[string]any, key string) bool {
	switch v := findValue(data, key).(type) {
	case bool:
		return v
	case float64:
		return v != 0
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "1", "true", "yes":
			return true
		}
	}
	return false
}

func (c *Client) finishLogin(ctx context.Context, resp map[string]any) error {
	raw, err := json.Marshal(resp)
	if err != nil {
		return err
	}
	return c.completeLogin(ctx, raw)
}
