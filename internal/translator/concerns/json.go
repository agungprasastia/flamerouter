package concerns

import (
	"encoding/json"
	"time"
)

// SafeParseJSON unmarshals JSON data into target and returns whether it succeeded.
func SafeParseJSON(data string, target any) bool {
	return json.Unmarshal([]byte(data), target) == nil
}

// MustMarshal marshals v into a JSON string or returns an empty string on error.
func MustMarshal(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}

	return string(b)
}

// MustMarshalIndent marshals v with indentation or returns an empty string on error.
func MustMarshalIndent(v any, prefix, indent string) string {
	b, err := json.MarshalIndent(v, prefix, indent)
	if err != nil {
		return ""
	}

	return string(b)
}

// CurrentTimestamp returns current UTC timestamp formatted as RFC3339.
func CurrentTimestamp() string {
	return time.Now().UTC().Format(time.RFC3339)
}
