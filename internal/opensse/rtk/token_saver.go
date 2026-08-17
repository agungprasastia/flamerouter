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
		Enabled:                      true,
		RTK:                          true,
		Headroom:                     false,
		HeadroomURL:                  "http://localhost:8787",
		Caveman:                      false,
		CavemanLevel:                 CavemanFull,
		Ponytail:                     false,
		PonytailLevel:                PonytailFull,
		Pxpipe:                       false,
		PxpipeMinChars:               25000,
		Model:                        "",
		Format:                       "",
		HeadroomCompressUserMessages: false,
	}
}

// EmptyTokenSaver returns zero-initialized options.
func EmptyTokenSaver() TokenSaverOptions {
	return TokenSaverOptions{
		CavemanLevel:                 "",
		HeadroomURL:                  "",
		PonytailLevel:                "",
		Model:                        "",
		Format:                       "",
		PxpipeMinChars:               0,
		RTK:                          false,
		Headroom:                     false,
		HeadroomCompressUserMessages: false,
		Caveman:                      false,
		Enabled:                      false,
		Ponytail:                     false,
		Pxpipe:                       false,
	}
}

func applyPromptInjections(body map[string]any, opts TokenSaverOptions) {
	if opts.Caveman {
		level := opts.CavemanLevel
		if level == "" {
			level = CavemanFull
		}

		InjectCaveman(body, opts.Format, level)
	}

	if opts.Ponytail {
		level := opts.PonytailLevel
		if level == "" {
			level = PonytailFull
		}

		InjectPonytail(body, opts.Format, level)
	}
}

// ApplyTokenSavers runs RTK compress → headroom → caveman → ponytail → pxpipe.
// Fail-open throughout. Mutates body in place; may replace for pxpipe.
func ApplyTokenSavers(body map[string]any, opts TokenSaverOptions) map[string]any {
	if body == nil || !opts.Enabled {
		return body
	}

	defer func() {
		//nolint:errcheck // recovery cleanup
		_ = recover()
	}()

	if opts.RTK {
		CompressMessages(body, true)
	}

	if opts.Headroom && opts.HeadroomURL != "" {
		CompressWithHeadroom(body, true, opts.HeadroomURL, opts.Model, opts.Format, opts.HeadroomCompressUserMessages)
	}

	applyPromptInjections(body, opts)

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
