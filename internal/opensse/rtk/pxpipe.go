package rtk

// PxpipeTransform is optional host-injected transform (Claude body → compressed).
// Matches 9router: open-sse stays free of install concerns.
type PxpipeTransform func(body map[string]any, model string, minChars int) (map[string]any, bool)

// GlobalPxpipeTransform set by host process when pxpipe is installed.
var GlobalPxpipeTransform PxpipeTransform

// PxpipeSummary stats for logging.
type PxpipeSummary struct {
	Reason        string
	OriginalChars int
	ImageCount    int
	Applied       bool
}

// CompressWithPxpipe applies pxpipe if installed and format is claude. Fail-open.
func CompressWithPxpipe(body map[string]any, enabled bool, format, model string, minChars int) (map[string]any, *PxpipeSummary) {
	if !enabled {
		return nil, &PxpipeSummary{Reason: "disabled", OriginalChars: 0, ImageCount: 0, Applied: false}
	}

	if GlobalPxpipeTransform == nil {
		return nil, &PxpipeSummary{Reason: "not_installed", OriginalChars: 0, ImageCount: 0, Applied: false}
	}

	if body == nil {
		return nil, &PxpipeSummary{Reason: "missing_body", OriginalChars: 0, ImageCount: 0, Applied: false}
	}

	if format != "claude" {
		return nil, &PxpipeSummary{Reason: "unsupported_format", OriginalChars: 0, ImageCount: 0, Applied: false}
	}

	if minChars <= 0 {
		minChars = 25000
	}

	defer func() {
		//nolint:errcheck // recovery cleanup
		_ = recover()
	}()

	out, ok := GlobalPxpipeTransform(body, model, minChars)
	if !ok || out == nil {
		return nil, &PxpipeSummary{Reason: "passthrough", OriginalChars: 0, ImageCount: 0, Applied: false}
	}

	return out, &PxpipeSummary{Applied: true, Reason: "applied", OriginalChars: 0, ImageCount: 0}
}
