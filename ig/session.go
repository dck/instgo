package ig

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

type Device struct {
	AppVersion     string `json:"app_version"`
	AndroidVersion int    `json:"android_version"`
	AndroidRelease string `json:"android_release"`
	DPI            string `json:"dpi"`
	Resolution     string `json:"resolution"`
	Manufacturer   string `json:"manufacturer"`
	Device         string `json:"device"`
	Model          string `json:"model"`
	CPU            string `json:"cpu"`
	VersionCode    string `json:"version_code"`
}

var defaultDevice = Device{
	AppVersion:     "269.0.0.18.75",
	AndroidVersion: 26,
	AndroidRelease: "8.0.0",
	DPI:            "480dpi",
	Resolution:     "1080x1920",
	Manufacturer:   "OnePlus",
	Device:         "devitron",
	Model:          "6T Dev",
	CPU:            "qcom",
	VersionCode:    "314665256",
}

type Session struct {
	Username        string            `json:"username"`
	UserID          string            `json:"user_id"`
	Authorization   string            `json:"authorization"`
	Mid             string            `json:"mid"`
	Cookies         map[string]string `json:"cookies"`
	PhoneID         string            `json:"phone_id"`
	UUID            string            `json:"uuid"`
	ClientSessionID string            `json:"client_session_id"`
	AdvertisingID   string            `json:"advertising_id"`
	AndroidDeviceID string            `json:"android_device_id"`
	RequestID       string            `json:"request_id"`
	TraySessionID   string            `json:"tray_session_id"`
	Device          Device            `json:"device"`
	Locale          string            `json:"locale"`
	Country         string            `json:"country"`
	CountryCode     int               `json:"country_code"`
	TimezoneOffset  int               `json:"timezone_offset"`
	PasswordKeyID   int               `json:"password_key_id"`
	PasswordPubKey  string            `json:"password_pub_key"`
}

func NewSession() *Session {
	return &Session{
		Cookies:         map[string]string{},
		PhoneID:         newUUID(),
		UUID:            newUUID(),
		ClientSessionID: newUUID(),
		AdvertisingID:   newUUID(),
		AndroidDeviceID: newAndroidID(),
		RequestID:       newUUID(),
		TraySessionID:   newUUID(),
		Device:          defaultDevice,
		Locale:          "en_US",
		Country:         "US",
		CountryCode:     1,
		TimezoneOffset:  -14400,
	}
}

func DefaultPath() (string, error) {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "instgo", "session.json"), nil
}

func LoadSession(path string) (*Session, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return NewSession(), nil
	}
	if err != nil {
		return nil, err
	}
	s := NewSession()
	if err := json.Unmarshal(data, s); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if s.Cookies == nil {
		s.Cookies = map[string]string{}
	}
	return s, nil
}

func (s *Session) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (s *Session) LoggedIn() bool {
	return s.Authorization != "" && s.UserID != ""
}

func (s *Session) userAgent() string {
	d := s.Device
	return fmt.Sprintf("Instagram %s Android (%d/%s; %s; %s; %s; %s; %s; %s; %s; %s)",
		d.AppVersion, d.AndroidVersion, d.AndroidRelease, d.DPI, d.Resolution,
		d.Manufacturer, d.Model, d.Device, d.CPU, s.Locale, d.VersionCode)
}

func newUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	h := hex.EncodeToString(b[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}

func newAndroidID() string {
	sum := sha256.Sum256([]byte(strconv.FormatFloat(float64(time.Now().UnixNano())/1e9, 'f', -1, 64)))
	return "android-" + hex.EncodeToString(sum[:])[:16]
}

func randomToken(n int) string {
	const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, n)
	_, _ = rand.Read(b)
	for i := range b {
		b[i] = chars[int(b[i])%len(chars)]
	}
	return string(b)
}
