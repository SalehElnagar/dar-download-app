package download_test

import (
	"errors"
	"testing"

	"github.com/SalehElnagar/dar-download-app/internal/download"
)

func TestSelectReturnsFullRepresentationWithoutRange(t *testing.T) {
	t.Parallel()

	selection, err := download.Select(nil, []string{`W/"old"`}, 10, `"etag-v1"`)
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	if selection.Partial || selection.Offset != 0 || selection.Length != 10 {
		t.Fatalf("selection = %#v", selection)
	}
}

func TestSelectNormalizesSupportedRanges(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		range_ string
		start  int64
		end    int64
	}{
		{name: "closed", range_: "bytes=2-5", start: 2, end: 5},
		{name: "open ended", range_: "bytes=6-", start: 6, end: 9},
		{name: "suffix", range_: "bytes=-4", start: 6, end: 9},
		{name: "large suffix", range_: "bytes=-100", start: 0, end: 9},
		{name: "clamped end", range_: "bytes=7-99", start: 7, end: 9},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			selection, err := download.Select([]string{tt.range_}, nil, 10, `"etag-v1"`)
			if err != nil {
				t.Fatalf("Select() error = %v", err)
			}
			if !selection.Partial || selection.Offset != tt.start || selection.Length != tt.end-tt.start+1 {
				t.Fatalf("selection = %#v", selection)
			}
			if selection.Range == nil || selection.Range.Start != tt.start || selection.Range.End != tt.end {
				t.Fatalf("range = %#v", selection.Range)
			}
		})
	}
}

func TestSelectRequiresExactCurrentStrongIfRange(t *testing.T) {
	t.Parallel()

	matching, err := download.Select(
		[]string{"bytes=2-5"}, []string{`"etag-v1"`}, 10, `"etag-v1"`,
	)
	if err != nil || !matching.Partial {
		t.Fatalf("matching Select() = %#v, %v", matching, err)
	}

	for _, validator := range []string{
		`"etag-v0"`,
		`W/"etag-v1"`,
		"Wed, 21 Oct 2015 07:28:00 GMT",
		"not-an-etag",
		`"` + string(make([]byte, 129)) + `"`,
	} {
		selection, selectErr := download.Select(
			[]string{"bytes=2-5"}, []string{validator}, 10, `"etag-v1"`,
		)
		if selectErr != nil {
			t.Fatalf("Select(%q) error = %v", validator, selectErr)
		}
		if selection.Partial || selection.Offset != 0 || selection.Length != 10 {
			t.Fatalf("Select(%q) = %#v, want full", validator, selection)
		}
	}
}

func TestSelectRejectsInvalidOrUnsatisfiableRanges(t *testing.T) {
	t.Parallel()

	for _, headerValues := range [][]string{
		{"items=0-1"},
		{"bytes="},
		{"bytes=4-2"},
		{"bytes=10-11"},
		{"bytes=0-1,4-5"},
		{"bytes=-0"},
		{"bytes=0-1", "bytes=4-5"},
		{"bytes=" + string(make([]byte, 129)) + "-"},
	} {
		if _, err := download.Select(headerValues, nil, 10, `"etag-v1"`); !errors.Is(err, download.ErrInvalidRange) {
			t.Fatalf("Select(%q) error = %v, want ErrInvalidRange", headerValues, err)
		}
	}

	if _, err := download.Select([]string{"bytes=0-0"}, nil, 0, `"etag-v1"`); !errors.Is(err, download.ErrInvalidRange) {
		t.Fatalf("empty object error = %v, want ErrInvalidRange", err)
	}
}
