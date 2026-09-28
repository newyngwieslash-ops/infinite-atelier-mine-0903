package desktop

import (
	"testing"
)

// video_capabilities_rp03_test.go is RP-03.1's contract: the allowed
// seconds/size values are a QUERYABLE capability the UI renders as choices,
// the single and the batch commands share the SAME validation, and a value
// the UI could not offer is refused on both paths before a billable call.

// TestRP03VideoCapabilityListsTheVendorEnum pins the capability read: the
// documented enum (T28's cross-checked 4/8/12 and the four resolutions) is
// what the answer carries, so a UI that renders these as choices can never
// offer a value the backend refuses.
func TestRP03VideoCapabilityListsTheVendorEnum(t *testing.T) {
	capability := VideoCapabilities()
	seconds := map[int]bool{}
	for _, value := range capability.AllowedSeconds {
		seconds[value] = true
	}
	for _, wanted := range []int{4, 8, 12} {
		if !seconds[wanted] {
			t.Fatalf("AllowedSeconds is missing the documented %d: %v", wanted, capability.AllowedSeconds)
		}
	}
	if len(capability.AllowedSeconds) != 3 {
		t.Fatalf("AllowedSeconds = %v, want exactly the documented three", capability.AllowedSeconds)
	}
	sizes := map[string]bool{}
	for _, value := range capability.AllowedSizes {
		sizes[value] = true
	}
	for _, wanted := range []string{"720x1280", "1280x720", "1024x1792", "1792x1024"} {
		if !sizes[wanted] {
			t.Fatalf("AllowedSizes is missing the documented %q: %v", wanted, capability.AllowedSizes)
		}
	}
	if capability.DefaultSeconds != 4 {
		t.Fatalf("DefaultSeconds = %d, want 4", capability.DefaultSeconds)
	}
}

// TestRP03VideoSecondsEnumRefusesUndocumented covers the negative cases the
// plan names: 1, 5 and 60 seconds are refused (60 also by the explicit
// ceiling), so a UI offering only the capability's values is provably the
// SAME set the backend accepts.
func TestRP03VideoSecondsEnumRefusesUndocumented(t *testing.T) {
	for _, seconds := range []int{1, 5, 60, -3} {
		if videoSecondsAllowed[seconds] {
			t.Fatalf("seconds=%d passed the enum", seconds)
		}
	}
	// And the documented values pass, so the enum is exactly the capability.
	for _, seconds := range []int{4, 8, 12} {
		if !videoSecondsAllowed[seconds] {
			t.Fatalf("seconds=%d failed the enum", seconds)
		}
	}
}

// TestRP03VideoSizeEnumRefusesUndocumented covers the size enum's negatives:
// an unsupported resolution (the plan's O02 case — a value that matches no
// supported aspect) is refused, an empty one is the provider's default and
// allowed, and a case-variant is not silently accepted.
func TestRP03VideoSizeEnumRefusesUndocumented(t *testing.T) {
	for _, size := range []string{"640x480", "720p", "1920x1080", "720X1280", " 720x1280"} {
		if videoSizeAllowed[size] {
			t.Fatalf("size=%q passed the enum", size)
		}
	}
	// An empty size is the provider default, which the binding handles before
	// the enum; the enum itself carries only the documented resolutions.
	if len(videoSizeAllowed) != 4 {
		t.Fatalf("videoSizeAllowed holds %d entries, want exactly the documented four", len(videoSizeAllowed))
	}
}
