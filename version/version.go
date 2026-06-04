package version

// Version info set at build time via ldflags.
var (
	GitCommit = "unknown"
	GitTag    = ""
	GitDirty  = ""
)

// String returns the version string for display.
func String() string {
	if GitTag != "" && GitDirty == "" {
		return GitTag
	}
	v := GitCommit
	if len(v) > 8 {
		v = v[:8]
	}
	if GitDirty != "" {
		v += "-dirty"
	}
	if GitTag != "" {
		v = GitTag + "+" + v
	}
	return v
}
