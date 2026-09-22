package database

// itoaDevice renders a device number.
//
// It is a local helper rather than `strconv.Itoa` because the value is compared for
// EQUALITY only: a caller never reads it, so it needs to be stable and distinct, not
// readable. Kept beside the taggable readers so the two cannot drift.
func itoaDevice(value uint64) string {
	if value == 0 {
		return "0"
	}
	digits := make([]byte, 0, 20)
	for value > 0 {
		digits = append([]byte{byte('0' + value%10)}, digits...)
		value /= 10
	}
	return string(digits)
}
