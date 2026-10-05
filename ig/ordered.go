package ig

import (
	"bytes"
	"encoding/json"
	"net/url"
	"strings"
)

type kv struct {
	k string
	v any
}

type obj []kv

func (o obj) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, p := range o {
		if i > 0 {
			b.WriteByte(',')
		}
		key, err := compactJSON(p.k)
		if err != nil {
			return nil, err
		}
		val, err := compactJSON(p.v)
		if err != nil {
			return nil, err
		}
		b.Write(key)
		b.WriteByte(':')
		b.Write(val)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

func compactJSON(v any) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(b.Bytes(), "\n"), nil
}

func dumps(v any) string {
	raw, err := compactJSON(v)
	if err != nil {
		panic(err)
	}
	return string(raw)
}

type form [][2]string

func (f form) encode() string {
	parts := make([]string, len(f))
	for i, p := range f {
		parts[i] = url.QueryEscape(p[0]) + "=" + url.QueryEscape(p[1])
	}
	return strings.Join(parts, "&")
}
