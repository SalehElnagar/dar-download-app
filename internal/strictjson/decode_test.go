package strictjson_test

import (
	"testing"

	"github.com/SalehElnagar/dar-download-app/internal/strictjson"
)

type document struct {
	Name   string `json:"name"`
	Nested struct {
		Value string `json:"value"`
	} `json:"nested"`
}

func TestDecodeAcceptsOneExactDocument(t *testing.T) {
	t.Parallel()

	var decoded document
	err := strictjson.Decode(
		[]byte(`{"name":"safe","nested":{"value":"exact"}}`),
		1024,
		&decoded,
	)
	if err != nil || decoded.Name != "safe" || decoded.Nested.Value != "exact" {
		t.Fatalf("Decode() = %#v, %v", decoded, err)
	}
}

func TestDecodeRejectsAmbiguousOrMalformedDocuments(t *testing.T) {
	t.Parallel()

	tests := map[string][]byte{
		"empty":               {},
		"invalid utf8":        {0xff},
		"duplicate top key":   []byte(`{"name":"one","name":"two","nested":{"value":"x"}}`),
		"duplicate nested":    []byte(`{"name":"one","nested":{"value":"x","value":"y"}}`),
		"unknown field":       []byte(`{"name":"one","nested":{"value":"x"},"extra":true}`),
		"trailing document":   []byte(`{"name":"one","nested":{"value":"x"}} {}`),
		"trailing scalar":     []byte(`{"name":"one","nested":{"value":"x"}} true`),
		"unterminated object": []byte(`{"name":"one"`),
		"top level array":     []byte(`[]`),
	}
	for name, raw := range tests {
		name, raw := name, raw
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var decoded document
			if err := strictjson.Decode(raw, 1024, &decoded); err == nil {
				t.Fatalf("Decode(%q) error = nil", raw)
			}
		})
	}
}

func TestDecodeRejectsOversizedDocument(t *testing.T) {
	t.Parallel()

	var decoded document
	if err := strictjson.Decode([]byte(`{"name":"safe","nested":{"value":"exact"}}`), 4, &decoded); err == nil {
		t.Fatal("Decode() error = nil, want size rejection")
	}
}

func TestDecodeWalksNestedArraysAndScalars(t *testing.T) {
	t.Parallel()

	var decoded struct {
		Items []map[string]any `json:"items"`
	}
	raw := []byte(`{"items":[{"string":"value","number":1,"boolean":true,"nothing":null}]}`)
	if err := strictjson.Decode(raw, 1024, &decoded); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if len(decoded.Items) != 1 || decoded.Items[0]["string"] != "value" {
		t.Fatalf("decoded = %#v", decoded)
	}
}
