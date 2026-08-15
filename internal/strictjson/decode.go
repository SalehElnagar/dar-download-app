// Package strictjson decodes small security-sensitive JSON documents.
package strictjson

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"unicode/utf8"
)

var errInvalidDocument = errors.New("invalid JSON document")

// Decode rejects oversized documents, invalid UTF-8, duplicate object keys,
// unknown struct fields, trailing data, and multiple top-level values.
func Decode(raw []byte, maxBytes int, destination any) error {
	if len(raw) == 0 || len(raw) > maxBytes || !utf8.Valid(raw) {
		return errInvalidDocument
	}
	if err := rejectDuplicateKeys(raw); err != nil {
		return errInvalidDocument
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return errInvalidDocument
	}
	if err := ensureEOF(decoder); err != nil {
		return errInvalidDocument
	}
	return nil
}

func ensureEOF(decoder *json.Decoder) error {
	var extra any
	err := decoder.Decode(&extra)
	if errors.Is(err, io.EOF) {
		return nil
	}
	return errInvalidDocument
}

func rejectDuplicateKeys(raw []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if err := walkValue(decoder); err != nil {
		return err
	}
	return ensureEOF(decoder)
}

func walkValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}

	switch delimiter {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, keyErr := decoder.Token()
			if keyErr != nil {
				return keyErr
			}
			key, keyOK := keyToken.(string)
			if !keyOK {
				return errInvalidDocument
			}
			if _, duplicate := seen[key]; duplicate {
				return fmt.Errorf("%w: duplicate object key", errInvalidDocument)
			}
			seen[key] = struct{}{}
			if valueErr := walkValue(decoder); valueErr != nil {
				return valueErr
			}
		}
		end, endErr := decoder.Token()
		if endErr != nil || end != json.Delim('}') {
			return errInvalidDocument
		}
	case '[':
		for decoder.More() {
			if valueErr := walkValue(decoder); valueErr != nil {
				return valueErr
			}
		}
		end, endErr := decoder.Token()
		if endErr != nil || end != json.Delim(']') {
			return errInvalidDocument
		}
	default:
		return errInvalidDocument
	}
	return nil
}
