package tally

import "time"

// Period is an inclusive date range.
type Period struct{ From, To time.Time }

// Chunks splits [from,to] into calendar months, or 7-day weeks if weekly is true.
// Sync must use these; a full year is never requested in one call.
func Chunks(from, to time.Time, weekly bool) []Period {
	var out []Period
	for cur := from; !cur.After(to); {
		var end time.Time
		if weekly {
			end = cur.AddDate(0, 0, 6)
		} else {
			end = time.Date(cur.Year(), cur.Month()+1, 1, 0, 0, 0, 0, cur.Location()).AddDate(0, 0, -1)
		}
		if end.After(to) {
			end = to
		}
		out = append(out, Period{cur, end})
		cur = end.AddDate(0, 0, 1)
	}
	return out
}
