package core

const (
	// MinHotCacheTTLSeconds is the shortest allowed auto-refresh and hot-cache TTL.
	MinHotCacheTTLSeconds = 10

	// MaxHotCacheTTLSeconds is the longest allowed auto-refresh and hot-cache TTL.
	// One hour is the upper bound for this desktop app: long enough for a slow
	// refresh cadence, and short enough that quote, overview, and hot caches
	// cannot stay stale across a trading session.
	MaxHotCacheTTLSeconds = 3600
)

// ClampHotCacheTTLSeconds caps seconds at MaxHotCacheTTLSeconds.
// Values at or below the maximum, including those under MinHotCacheTTLSeconds,
// are returned unchanged so callers can keep rejecting a too-short TTL.
func ClampHotCacheTTLSeconds(seconds int) int {
	if seconds > MaxHotCacheTTLSeconds {
		return MaxHotCacheTTLSeconds
	}
	return seconds
}
