package download_test

import (
	"testing"

	"github.com/SalehElnagar/dar-download-app/internal/download"
)

func FuzzSelectRange(f *testing.F) {
	for _, seed := range []string{"bytes=0-0", "bytes=2-5", "bytes=-4", "bytes=", "items=0-1"} {
		f.Add(seed, `"etag-v1"`, int64(10))
	}
	f.Fuzz(func(t *testing.T, rangeHeader, ifRange string, size int64) {
		if size < 0 || size > 1024*1024 {
			return
		}
		selection, err := download.Select(
			[]string{rangeHeader}, []string{ifRange}, size, `"etag-v1"`,
		)
		if err != nil {
			return
		}
		if selection.Offset < 0 || selection.Length < 0 ||
			selection.Offset+selection.Length > size {
			t.Fatalf("selection outside object: %#v, size %d", selection, size)
		}
		if selection.Partial {
			if selection.Range == nil || selection.Length == 0 ||
				selection.Range.Start != selection.Offset ||
				selection.Range.End-selection.Range.Start+1 != selection.Length {
				t.Fatalf("invalid partial selection %#v", selection)
			}
		}
	})
}
