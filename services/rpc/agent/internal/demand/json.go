package demand

import (
	"bytes"
	"encoding/json"
	"io"
	"regexp"
	"unicode/utf8"
)

var fieldName = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// A small, closed input schema: reject null, duplicate/aliased fields, unknown
// fields, invalid UTF-8, deep nesting and trailing documents. Errors never echo
// a model proposal, a file path or an underlying reader's diagnostic.
func decodeBounded(r io.Reader, limit int64, dst any) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil || int64(len(data)) > limit || !utf8.Valid(data) || !unambiguousJSON(data) {
		return nil, ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if d.Decode(dst) != nil {
		return nil, ErrInvalid
	}
	return data, nil
}

func unambiguousJSON(data []byte) bool {
	d := json.NewDecoder(bytes.NewReader(data))
	var value func(int) bool
	value = func(depth int) bool {
		if depth > 8 {
			return false
		}
		token, err := d.Token()
		if err != nil || token == nil {
			return false
		}
		delim, compound := token.(json.Delim)
		if !compound {
			return true
		}
		switch delim {
		case '{':
			seen := map[string]bool{}
			for d.More() {
				token, err := d.Token()
				key, ok := token.(string)
				// Only canonical lowercase keys; Go's case-insensitive field
				// matching cannot turn a second spelling into an override.
				if err != nil || !ok || !fieldName.MatchString(key) || seen[key] {
					return false
				}
				seen[key] = true
				if !value(depth + 1) {
					return false
				}
			}
			end, err := d.Token()
			return err == nil && end == json.Delim('}')
		case '[':
			for d.More() {
				if !value(depth + 1) {
					return false
				}
			}
			end, err := d.Token()
			return err == nil && end == json.Delim(']')
		default:
			return false
		}
	}
	if !value(0) {
		return false
	}
	_, err := d.Token()
	return err == io.EOF
}
