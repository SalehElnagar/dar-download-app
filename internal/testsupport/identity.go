package testsupport

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
)

// EasyAuthHeaders constructs synthetic platform identity evidence.
func EasyAuthHeaders(principalID, tenantID string) http.Header {
	payload := struct {
		AuthType string `json:"auth_typ"`
		Claims   []struct {
			Type  string `json:"typ"`
			Value string `json:"val"`
		} `json:"claims"`
	}{AuthType: "aad"}
	payload.Claims = append(payload.Claims,
		struct {
			Type  string `json:"typ"`
			Value string `json:"val"`
		}{Type: "oid", Value: principalID},
		struct {
			Type  string `json:"typ"`
			Value string `json:"val"`
		}{Type: "tid", Value: tenantID},
	)
	raw, err := json.Marshal(payload)
	if err != nil {
		panic("synthetic identity fixture is invalid")
	}
	return http.Header{
		"X-Ms-Client-Principal":    []string{base64.StdEncoding.EncodeToString(raw)},
		"X-Ms-Client-Principal-Id": []string{principalID},
	}
}
