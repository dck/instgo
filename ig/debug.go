package ig

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
)

const debugBodyLimit = 4096

var debugLog = log.New(io.Discard, "", log.LstdFlags|log.Lmicroseconds)

var sensitiveJSON = regexp.MustCompile(`("(?:enc_password|password|verification_code|security_code|_csrftoken|text|link_text|code)":")(?:[^"\\]|\\.)*"`)

var (
	bearerToken   = regexp.MustCompile(`IGT:2:[A-Za-z0-9+/=_-]+`)
	sessionCookie = regexp.MustCompile(`(sessionid=)[^;\\"\s]+`)
)

var sensitiveForm = map[string]bool{
	"security_code": true, "verification_code": true, "text": true, "link_text": true,
	"enc_new_password1": true, "enc_new_password2": true,
}

var sensitiveHeaders = map[string]bool{
	"authorization": true, "cookie": true, "ig-set-authorization": true, "x-csrftoken": true,
}

func SetDebugOutput(w io.Writer) {
	debugLog.SetOutput(w)
}

func Debugf(format string, args ...any) {
	debugLog.Printf(format, args...)
}

func verbosePath(path string) bool {
	for _, p := range []string{"accounts/", "challenge/", "launcher/", "qe/", "bloks/", "attestation/", "graphql_www"} {
		if strings.Contains(path, p) {
			return true
		}
	}
	return false
}

func redactBody(body string) string {
	if raw, ok := strings.CutPrefix(body, "signed_body=SIGNATURE."); ok {
		if decoded, err := url.QueryUnescape(raw); err == nil {
			body = "signed_body=SIGNATURE." + decoded
		}
	} else if form, err := url.ParseQuery(body); err == nil && len(form) > 0 && !strings.HasPrefix(body, "{") {
		keys := make([]string, 0, len(form))
		for k := range form {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			v := form.Get(k)
			if sensitiveForm[k] {
				v = "[redacted]"
			}
			parts = append(parts, k+"="+v)
		}
		body = strings.Join(parts, "&")
	}
	return truncate(redactSecrets(body))
}

func redactSecrets(s string) string {
	s = sensitiveJSON.ReplaceAllString(s, `${1}[redacted]"`)
	s = bearerToken.ReplaceAllString(s, "IGT:2:[redacted]")
	return sessionCookie.ReplaceAllString(s, "${1}[redacted]")
}

func formatHeaders(h http.Header) string {
	keys := make([]string, 0, len(h))
	for k := range h {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		for _, v := range h[k] {
			lk := strings.ToLower(k)
			switch {
			case sensitiveHeaders[lk] && v != "":
				v = fmt.Sprintf("[redacted %d bytes]", len(v))
			case lk == "set-cookie":
				name, _, _ := strings.Cut(v, "=")
				v = name + "=[redacted]"
			}
			fmt.Fprintf(&b, "    %s: %s\n", k, v)
		}
	}
	return b.String()
}

func truncate(s string) string {
	if len(s) > debugBodyLimit {
		return s[:debugBodyLimit] + "…[truncated]"
	}
	return s
}
