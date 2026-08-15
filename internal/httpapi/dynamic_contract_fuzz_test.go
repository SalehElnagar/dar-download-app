package httpapi

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func FuzzDownloadTarget(f *testing.F) {
	for _, seed := range [][2]string{
		{"v26.8.31.01", "canton_dars.zip"},
		{"release", "package.dar"},
		{"..", "package.dar"},
		{"release/nested", "package.dar"},
		{"release", "nested/package.dar"},
		{"release%2Fnested", "package.dar"},
		{"release", "package%252Fnested.dar"},
		{"résumé", "package.dar"},
	} {
		f.Add(seed[0], seed[1])
	}

	f.Fuzz(func(t *testing.T, version, fileName string) {
		path := "/v1/releases/" + version + "/download/" + fileName
		target, accepted := downloadTargetFromRequest(&http.Request{
			Method:     http.MethodGet,
			URL:        &url.URL{Path: path},
			RequestURI: path,
		})
		if !accepted {
			return
		}
		if target.blobName != version+"/"+fileName || target.fileName != fileName {
			t.Fatalf("accepted target = %#v for %q and %q", target, version, fileName)
		}
		if strings.Count(target.blobName, "/") != 1 ||
			len(version) > maxVersionBytes || len(fileName) > maxFileNameBytes {
			t.Fatalf("accepted ambiguous or oversized target %#v", target)
		}
		for _, value := range []string{version, fileName} {
			if value == "" || strings.Trim(value, ".") == "" {
				t.Fatalf("accepted empty or dot-only segment %q", value)
			}
			for index := 0; index < len(value); index++ {
				character := value[index]
				if (character < 'A' || character > 'Z') &&
					(character < 'a' || character > 'z') &&
					(character < '0' || character > '9') &&
					character != '.' && character != '_' && character != '-' {
					t.Fatalf("accepted unsafe byte %#x in %q", character, value)
				}
			}
		}
	})
}
