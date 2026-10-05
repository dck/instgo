package ig

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
)

var bloksAppRef = regexp.MustCompile(`com\.bloks\.[_a-zA-Z0-9.]+`)

func collectStrings(v any, out *[]string) {
	switch t := v.(type) {
	case string:
		*out = append(*out, t)
	case map[string]any:
		for _, child := range t {
			collectStrings(child, out)
		}
	case []any:
		for _, child := range t {
			collectStrings(child, out)
		}
	}
}

func allStrings(v any) []string {
	var out []string
	collectStrings(v, &out)
	return out
}

func jsonStringAt(s string, i int) (string, int, bool) {
	if i >= len(s) || s[i] != '"' {
		return "", 0, false
	}
	escaped := false
	for j := i + 1; j < len(s); j++ {
		switch {
		case escaped:
			escaped = false
		case s[j] == '\\':
			escaped = true
		case s[j] == '"':
			var out string
			if json.Unmarshal([]byte(s[i:j+1]), &out) != nil {
				return "", 0, false
			}
			return out, j + 1, true
		}
	}
	return "", 0, false
}

func parenExpr(s string, start int) string {
	depth := 0
	inString, escaped := false, false
	for i := start; i < len(s); i++ {
		ch := s[i]
		if inString {
			switch {
			case escaped:
				escaped = false
			case ch == '\\':
				escaped = true
			case ch == '"':
				inString = false
			}
			continue
		}
		switch ch {
		case '"':
			inString = true
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	return ""
}

func exprItems(s string) []string {
	if len(s) < 2 || s[0] != '(' || s[len(s)-1] != ')' {
		return nil
	}
	var items []string
	for i := 1; i < len(s)-1; {
		switch s[i] {
		case ' ', '\t', '\n', '\r':
			i++
			continue
		case '(':
			item := parenExpr(s, i)
			if item == "" {
				return nil
			}
			items = append(items, item)
			i += len(item)
		case '"':
			_, end, ok := jsonStringAt(s, i)
			if !ok {
				return nil
			}
			items = append(items, s[i:end])
			i = end
		default:
			j := i
			for j < len(s) && !strings.ContainsRune("() \t\n\r\"", rune(s[j])) {
				j++
			}
			if j == i {
				return nil
			}
			items = append(items, s[i:j])
			i = j
		}
	}
	return items
}

func bloksContextValue(result any, appID, key string) string {
	anchor := dumps(appID)
	for _, text := range allStrings(result) {
		for from := 0; ; {
			idx := strings.Index(text[from:], anchor)
			if idx < 0 {
				break
			}
			foundEnd := from + idx + len(anchor)
			from = foundEnd
			mapStart := strings.Index(text[foundEnd:], "(f4i")
			if mapStart < 0 {
				continue
			}
			mapStart += foundEnd
			if loc := bloksAppRef.FindStringIndex(text[foundEnd:]); loc != nil && foundEnd+loc[0] < mapStart {
				continue
			}
			expr := parenExpr(text, mapStart)
			for expr != "" {
				items := exprItems(expr)
				if len(items) != 3 || items[0] != "f4i" {
					break
				}
				keys, values := exprItems(items[1]), exprItems(items[2])
				if len(keys) == 0 || len(values) == 0 || keys[0] != "dkc" || values[0] != "dkc" || len(keys) != len(values) {
					break
				}
				expr = ""
				for n := 1; n < len(keys); n++ {
					name, _, ok := jsonStringAt(keys[n], 0)
					if !ok {
						continue
					}
					if name == key {
						v, _, ok := jsonStringAt(values[n], 0)
						if !ok {
							return ""
						}
						return v
					}
					if name == "server_params" {
						expr = values[n]
					}
				}
			}
		}
	}
	return ""
}

func findValue(data any, key string) any {
	switch t := data.(type) {
	case map[string]any:
		if v, ok := t[key]; ok && truthy(v) {
			return v
		}
		for _, child := range t {
			if v := findValue(child, key); v != nil {
				return v
			}
		}
	case []any:
		for _, child := range t {
			if v := findValue(child, key); v != nil {
				return v
			}
		}
	case string:
		if len(t) < 10000 && strings.Contains(t, key) {
			var nested any
			if json.Unmarshal([]byte(t), &nested) == nil {
				return findValue(nested, key)
			}
		}
	}
	return nil
}

func truthy(v any) bool {
	switch t := v.(type) {
	case nil:
		return false
	case string:
		return t != ""
	case bool:
		return t
	case float64:
		return t != 0
	case map[string]any:
		return len(t) > 0
	case []any:
		return len(t) > 0
	}
	return true
}

func bloksPayload(result map[string]any) map[string]any {
	layout, _ := result["layout"].(map[string]any)
	payload, _ := layout["bloks_payload"].(map[string]any)
	return payload
}

func bloksAAC(result map[string]any) string {
	nodes, _ := bloksPayload(result)["data"].([]any)
	for _, n := range nodes {
		node, _ := n.(map[string]any)
		data, _ := node["data"].(map[string]any)
		if data == nil || data["key"] != "CAA_ACCOUNT_ACCESS_CONTEXT:aac" {
			continue
		}
		if initial, ok := data["initial"].(string); ok && strings.TrimSpace(initial) != "" {
			return initial
		}
		if lispy, ok := data["initial_lispy"].(string); ok {
			if q := strings.Index(lispy, `"`); q >= 0 {
				if v, _, ok := jsonStringAt(lispy, q); ok && v != "" {
					return v
				}
			}
		}
	}
	return ""
}

func twoStepContext(result map[string]any) string {
	if v, ok := findValue(result, "two_step_verification_context").(string); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	action, _ := bloksPayload(result)["action"].(string)
	if action == "" {
		return ""
	}
	return strings.TrimSpace(bloksContextValue(map[string]any{"action": action}, "com.bloks.www.two_step_verification.entrypoint", "two_step_verification_context"))
}

type embeddedLogin struct {
	response map[string]any
	headers  map[string]string
	cookies  []*http.Cookie
}

func extractLogin(result map[string]any) *embeddedLogin {
	action, _ := bloksPayload(result)["action"].(string)
	for i := 0; i < len(action); {
		if action[i] != '"' {
			i++
			continue
		}
		value, end, ok := jsonStringAt(action, i)
		if !ok {
			i++
			continue
		}
		i = end
		if !strings.Contains(value, "login_response") {
			continue
		}
		var raw struct {
			LoginResponse string `json:"login_response"`
			Headers       string `json:"headers"`
			Cookies       string `json:"cookies"`
		}
		if json.Unmarshal([]byte(value), &raw) != nil || raw.LoginResponse == "" {
			continue
		}
		out := &embeddedLogin{headers: map[string]string{}}
		if json.Unmarshal([]byte(raw.LoginResponse), &out.response) != nil {
			continue
		}
		if raw.Headers != "" {
			var h map[string]any
			if json.Unmarshal([]byte(raw.Headers), &h) != nil {
				continue
			}
			for k, v := range h {
				if s, ok := v.(string); ok {
					out.headers[strings.ToLower(k)] = s
				}
			}
		}
		for _, line := range strings.Split(strings.ReplaceAll(raw.Cookies, "\r", ""), "\n") {
			line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "Set-Cookie:"))
			if line == "" {
				continue
			}
			if ck, err := http.ParseSetCookie(line); err == nil {
				out.cookies = append(out.cookies, ck)
			}
		}
		return out
	}
	return nil
}

func (c *Client) applyLogin(result map[string]any) (map[string]any, bool) {
	login := extractLogin(result)
	if login == nil {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	s := c.s
	auth := login.headers["ig-set-authorization"]
	if auth != "" {
		if id := authorizationUserID(auth); id != "" {
			s.Authorization, s.UserID = auth, id
		}
	}
	session := ""
	for _, ck := range login.cookies {
		s.Cookies[ck.Name] = ck.Value
		if ck.Name == "sessionid" {
			session = ck.Value
		}
		if ck.Name == "ds_user_id" && s.UserID == "" {
			s.UserID = ck.Value
		}
	}
	if v := login.headers["ig-set-ig-u-rur"]; v != "" {
		s.IgURur = v
	}
	if v := login.headers["x-ig-set-www-claim"]; v != "" {
		s.WWWClaim = v
	}
	return login.response, auth != "" || session != ""
}

func caaMarkers(result map[string]any) []string {
	var out []string
	for _, s := range allStrings(result) {
		if strings.HasPrefix(s, "CAA_") && strings.Contains(s, ":") {
			out = append(out, s)
		}
	}
	return out
}
