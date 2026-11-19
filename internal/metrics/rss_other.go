//go:build !windows

package metrics

// RSS is not measured on non-Windows builds.
func RSS() (cur, peak float64) { return 0, 0 }
