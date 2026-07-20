package usage

// Quota is a placeholder per-provider quota snapshot.
type Quota struct {
	Provider string `json:"provider"`
	Used     int64  `json:"used"`
	Limit    int64  `json:"limit"`
	Unit     string `json:"unit"`
	Note     string `json:"note,omitempty"`
}

// FetchQuota returns a stub quota for provider. Live fetch deferred.
func FetchQuota(provider string) Quota {
	return Quota{
		Provider: provider,
		Used:     0,
		Limit:    0,
		Unit:     "tokens",
		Note:     "quota fetch not implemented",
	}
}
