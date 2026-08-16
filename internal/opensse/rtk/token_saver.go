package rtk

import "strings"

// TokenSaverOptions controls all token-saver hooks (matches chatCore flags).
type TokenSaverOptions struct {
	CavemanLevel                 string
	HeadroomURL                  string
	PonytailLevel                string
	Model                        string
	Format                       string
	PxpipeMinChars               int
	RTK                          bool
	Headroom                     bool
	HeadroomCompressUserMessages bool
	Caveman                      bool
	Enabled                      bool
	Ponytail                     bool
	Pxpipe                       bool
}

// DefaultTokenSaver returns defaults matching 9router settingsRepo.
func DefaultTokenSaver() TokenSaverOptions {
	return TokenSaverOptions{
		Enabled:        true,
		RTK:            true,
		Headroom:       false,
		HeadroomURL:    "http://localhost:8787",
		Caveman:        false,
		CavemanLevel:   CavemanFull,
		Ponytail:       false,
		PonytailLevel:  PonytailFull,
		Pxpipe:         false,
		PxpipeMinChars: 25000,
	}
}

// ApplyTokenSavers runs RTK compress → headroom → caveman → ponytail → pxpipe.
// Fail-open throughout. Mutates body in place; may replace for pxpipe.
func ApplyTokenSavers(body map[string]any, opts TokenSaverOptions) map[string]any {
	if body == nil || !opts.Enabled {
		return body
	}

	defer func() { recover() }()

	// 1. RTK tool_result compression
	if opts.RTK {
		CompressMessages(body, true)
	}

	// 2. Headroom proxy
	if opts.Headroom && opts.HeadroomURL != "" {
		CompressWithHeadroom(body, true, opts.HeadroomURL, opts.Model, opts.Format, opts.HeadroomCompressUserMessages)
	}

	// 3. Caveman
	if opts.Caveman {
		level := opts.CavemanLevel
		if level == "" {
			level = CavemanFull
		}

		InjectCaveman(body, opts.Format, level)
	}

	// 4. Ponytail
	if opts.Ponytail {
		level := opts.PonytailLevel
		if level == "" {
			level = PonytailFull
		}

		InjectPonytail(body, opts.Format, level)
	}

	// 5. Pxpipe (may return new body)
	if opts.Pxpipe {
		if newBody, sum := CompressWithPxpipe(body, true, opts.Format, opts.Model, opts.PxpipeMinChars); sum != nil && sum.Applied && newBody != nil {
			return newBody
		}
	}

	return body
}

// ParseTokenSaverHeader returns false if client disabled token savers.
func ParseTokenSaverHeader(h string) bool {
	return strings.ToLower(strings.TrimSpace(h)) != "off"
}
