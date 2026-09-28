package media

import "testing"

// content_analysis_t16_test.go checks the PARSERS: the audit's T16 needs
// located ranges, and a parser that mis-reads ffmpeg's lines would locate
// them wrong. The engine-level calls run where ffmpeg exists; the parse half
// runs everywhere.

// TestBlackRangeParsing reads one real blackdetect line and refuses to invent
// ranges the filter did not report.
func TestBlackRangeParsing(t *testing.T) {
	output := "[blackdetect @ 0x0] black_start:1.2 black_end:3.4 black_duration:2.2\n" +
		"[blackdetect @ 0x0] black_start:10 black_end:10.5 black_duration:0.5\n"
	ranges := parseBlackRanges(output)
	if len(ranges) != 2 {
		t.Fatalf("%d ranges parsed from two blackdetect lines", len(ranges))
	}
	if ranges[0][0] != 1200 || ranges[0][1] != 3400 {
		t.Fatalf("the first range is %v; 1200..3400 was reported", ranges[0])
	}
	if ranges[1][0] != 10000 || ranges[1][1] != 10500 {
		t.Fatalf("the second range is %v", ranges[1])
	}
	if got := parseBlackRanges("no black frames here"); len(got) != 0 {
		t.Fatalf("clean output parsed as %v", got)
	}
}

// TestSilenceRangeParsing reads silencedetect's start/end pairs and drops an
// unterminated pair rather than inventing its end.
func TestSilenceRangeParsing(t *testing.T) {
	output := "[silencedetect @ 0x0] silence_start: 4.5\n" +
		"[silencedetect @ 0x0] silence_end: 7.2 | silence_duration: 2.7\n" +
		"[silencedetect @ 0x0] silence_start: 9\n"
	ranges := parseSilenceRanges(output)
	if len(ranges) != 1 {
		t.Fatalf("%d ranges parsed; the unterminated start must be dropped", len(ranges))
	}
	if ranges[0][0] != 4500 || ranges[0][1] != 7200 {
		t.Fatalf("the range is %v; 4500..7200 was reported", ranges[0])
	}
}
