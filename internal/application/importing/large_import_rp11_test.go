package importing

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	appfiles "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/files"
)

// large_import_rp11_test.go is RP-11.3's scripted sample for the protocol's
// 「100k 汉字导入」 metric: a synthetic 100,000-Chinese-character novel is
// generated, imported through the REAL decoder/splitter/chapter pipeline
// (no fixture shortcuts), and the duration + chapter count are reported.
// The assertion is a generous ceiling (the protocol's own bound), not a
// tight race — the sample's job is to prove the pipeline handles the scale
// and to record the number, not to set a speed record.

// buildLargeNovel generates a synthetic 100k-Chinese-character novel with
// chapter markers every ~2,000 characters (50 chapters), deterministic
// content so the sample is reproducible.
// rp11NoopStore satisfies the DocumentStore the service composes with; the
// prepare stage never calls it, but the service refuses nil.
type rp11NoopStore struct{}

func (rp11NoopStore) Import(_ context.Context, _ string, _ []byte) (appfiles.Object, error) {
	return appfiles.Object{}, nil
}

func (rp11NoopStore) Open(_ context.Context, _ string) ([]byte, error) { return nil, nil }

type clockFunc func() time.Time

func (f clockFunc) Now() time.Time { return f() }

func buildLargeNovel(targetRunes int) string {
	var builder strings.Builder
	chapter := 1
	written := 0
	for written < targetRunes {
		fmt.Fprintf(&builder, "第%d章 长夜\n", chapter)
		written += len([]rune(fmt.Sprintf("第%d章 长夜\n", chapter)))
		// ~2,000 characters of deterministic prose per chapter.
		paragraph := "雨水顺着屋檐落下，街灯在湿漉漉的石板路上投下晃动的影子。" +
			"她推开门，老屋里弥漫着潮湿的木头气味，墙上的挂钟停在三年前的那个雨夜。" +
			"他坐在桌前，手里攥着一封没有寄出的信，信封上的字迹已经被泪水洇开。" +
			"窗外的雷声由远及近，像是多年前的回声，一层一层压在这座城市的胸口。\n\n"
		for i := 0; i < 8 && written < targetRunes; i++ {
			builder.WriteString(paragraph)
			written += len([]rune(paragraph))
		}
		chapter++
	}
	return builder.String()
}

// TestRP11LargeImportSample takes the protocol's 100k-character sample: it
// reports the imported rune count, chapter count, and wall duration, and
// asserts the generous ceiling the protocol sets. Failure modes count in the
// denominator, per protocol — a timeout or error IS the result.
func TestRP11LargeImportSample(t *testing.T) {
	novel := buildLargeNovel(100_000)
	runes := len([]rune(novel))
	if runes < 100_000 {
		t.Fatalf("the synthetic novel is %d runes, short of the 100k protocol input", runes)
	}

	started := time.Now()
	service := NewService(Options{
		Store: rp11NoopStore{},
		Story: nil, // prepare does not touch the story store
		Clock: clockFunc(func() time.Time { return time.Now().UTC() }),
	})
	document, err := service.prepare([]byte(novel), "txt")
	elapsed := time.Since(started)
	if err != nil {
		// A failure IS the sample (protocol: failures count in the
		// denominator). Report it and fail the test — the metric records it.
		t.Fatalf("the 100k import failed (recorded as a failure in the metric's denominator): %v", err)
	}

	charCount := document.RuneCount()
	chapters := len(document.Chapters)
	t.Logf("[RP11.3-import] runes=%d chapters=%d durationMS=%d", charCount, chapters, elapsed.Milliseconds())

	// The protocol's generous ceiling for the whole import: 30 seconds.
	// A result above it is a FAIL, not a skipped sample.
	if elapsed > 30*time.Second {
		t.Fatalf("the 100k import took %v, beyond the protocol's 30s ceiling", elapsed)
	}
	// The chapter detector must have found a sane number of chapters (the
	// synthetic novel names 50+; a split into 1 or 10000 would be a defect).
	if chapters < 40 || chapters > 120 {
		t.Fatalf("chapter count %d is outside the sane band for a 50-chapter synthetic novel", chapters)
	}
	// The normalized text must not have lost content.
	if charCount < 95_000 {
		t.Fatalf("the normalized text kept only %d of %d runes", charCount, runes)
	}
}
