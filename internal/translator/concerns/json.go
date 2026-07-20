package concerns

import (
	"encoding/json"
	"time"
)

func SafeParseJSON(data string, target any) bool {
	return json.Unmarshal([]byte(data), target) == nil
}

func MustMarshal(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

func MustMarshalIndent(v any, prefix, indent string) string {
	b, err := json.MarshalIndent(v, prefix, indent)
	if err != nil {
		return ""
	}
	return string(b)
}

func CurrentTimestamp() string {
	return time.Now().UTC().Format(time.RFC3339)
}
