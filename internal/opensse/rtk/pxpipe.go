package rtk

// PxpipeTransform is optional host-injected transform (Claude body → compressed).
// Matches 9router: open-sse stays free of install concerns.
type PxpipeTransform func(body map[string]any, model string, minChars int) (map[string]any, bool)

// GlobalPxpipeTransform set by host process when pxpipe is installed.
var GlobalPxpipeTransform PxpipeTransform

// PxpipeSummary stats for logging.
type PxpipeSummary struct {
	Applied       bool
	Reason        string
	OriginalChars int
	ImageCount    int
}

// CompressWithPxpipe applies pxpipe if installed and format is claude. Fail-open.
func CompressWithPxpipe(body map[string]any, enabled bool, format, model string, minChars int) (map[string]any, *PxpipeSummary) {
	if !enabled {
		return nil, &PxpipeSummary{Reason: "disabled"}
	}
	if GlobalPxpipeTransform == nil {
		return nil, &PxpipeSummary{Reason: "not_installed"}
	}
	if body == nil {
		return nil, &PxpipeSummary{Reason: "missing_body"}
	}
	if format != "claude" {
		return nil, &PxpipeSummary{Reason: "unsupported_format"}
	}
	if minChars <= 0 {
		minChars = 25000
	}
	defer func() { recover() }()
	out, ok := GlobalPxpipeTransform(body, model, minChars)
	if !ok || out == nil {
		return nil, &PxpipeSummary{Reason: "passthrough"}
	}
	return out, &PxpipeSummary{Applied: true, Reason: "applied"}
}
