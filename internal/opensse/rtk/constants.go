package rtk

// RTK compression thresholds and caps.
const (
	RawCap              = 10 * 1024 * 1024
	MinCompressSize     = 500
	DetectWindow        = 1024
	GitDiffHunkMaxLines = 100
	GitDiffContextKeep  = 3
	GitLogMaxLines      = 200
	DedupLineMax        = 2000
	GrepPerFileMax      = 10
	FindPerDirMax       = 10
	FindTotalDirMax     = 20
	StatusMaxFiles      = 10
	StatusMaxUntracked  = 10
	LSExtSummaryTop     = 5
	TreeMaxLines        = 200
	SearchListPerDirMax = 10
	SearchListTotalDir  = 20
	SmartTruncateHead   = 120
	SmartTruncateTail   = 60
	SmartTruncateMin    = 250
	ReadNumberedMinHit  = 0.7
)

// LSNoiseDirs contains common directory names filtered out from ls results.
var LSNoiseDirs = []string{
	"node_modules", ".git", "target", "__pycache__",
	".next", "dist", "build", ".cache", ".turbo",
	".vercel", ".pytest_cache", ".mypy_cache", ".tox",
	".venv", "venv", "env", "coverage", ".nyc_output",
	".DS_Store", "Thumbs.db", ".idea", ".vscode", ".vs",
}
