package runner

import (
	"errors"
	"fmt"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// recordingWriter keeps each Write call separately, so a test can check that
// every call is one whole line.
type recordingWriter struct {
	mu    sync.Mutex
	calls []string
}

func (w *recordingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.calls = append(w.calls, string(p))
	return len(p), nil
}

func (w *recordingWriter) text() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return strings.Join(w.calls, "")
}

func (w *recordingWriter) lines() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]string(nil), w.calls...)
}

func TestLiveOutputPrefixesStripsAndRedactsEachLine(t *testing.T) {
	var out recordingWriter
	attempt := newLiveSink(&out).startAttempt("job-001")
	_, _ = attempt.Stdout().Write([]byte("hello \x1b[31mred\x1b[0m \x1b]0;title\x07token=fixture-secret-value\r\nsecond"))
	_, _ = attempt.Stdout().Write([]byte(" half\n"))
	_, _ = attempt.Stderr().Write([]byte("Authorization: Bearer abcdefghijklmnop\x07\n"))
	attempt.close()
	got := out.text()
	for _, want := range []string{
		"[job-001 stdout] hello red token=[REDACTED]\n",
		"[job-001 stdout] second half\n",
		"[job-001 stderr] Authorization: Bearer [REDACTED]\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%q", want, got)
		}
	}
	if strings.Contains(got, "fixture-secret-value") || strings.Contains(got, "abcdefghijklmnop") || strings.ContainsAny(got, "\x1b\x07\r") {
		t.Fatalf("secret or control character reached live output: %q", got)
	}
}

func TestLiveOutputSplitsLongLinesAndFlushesThePartialLine(t *testing.T) {
	var out recordingWriter
	attempt := newLiveSink(&out).startAttempt("job-001")
	long := strings.Repeat("x", 2*liveLineLimit+100)
	// Written in pieces, as a pipe delivers it.
	for start := 0; start < len(long); start += 5000 {
		_, _ = attempt.Stdout().Write([]byte(long[start:min(start+5000, len(long))]))
	}
	_, _ = attempt.Stdout().Write([]byte("\nunterminated"))
	attempt.close()
	lines := out.lines()
	if len(lines) != 4 {
		t.Fatalf("want 3 pieces and the partial line, got %d: %q", len(lines), lines)
	}
	total := 0
	for _, line := range lines[:3] {
		body := strings.TrimSuffix(strings.TrimPrefix(line, "[job-001 stdout] "), "\n")
		if len(body) > liveLineLimit || strings.Trim(body, "x") != "" {
			t.Fatalf("piece of %d bytes: %.40q", len(body), line)
		}
		total += len(body)
	}
	if total != len(long) || lines[3] != "[job-001 stdout] unterminated\n" {
		t.Fatalf("total %d of %d, last %q", total, len(long), lines[3])
	}
}

func TestLiveOutputDoesNotSplitAUTF8Sequence(t *testing.T) {
	var out recordingWriter
	attempt := newLiveSink(&out).startAttempt("job-001")
	_, _ = attempt.Stdout().Write([]byte(strings.Repeat("x", liveLineLimit-1) + "é tail"))
	attempt.close()
	if got := out.text(); strings.Contains(got, "�") || !strings.Contains(got, "] é tail\n") {
		t.Fatalf("split a rune: %.80q", got[len(got)-40:])
	}
}

type failingWriter struct {
	mu    sync.Mutex
	calls int
}

func (w *failingWriter) Write([]byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.calls++
	return 0, errors.New("closed terminal")
}

func TestLiveOutputNeverFailsAndStopsWritingAfterAnError(t *testing.T) {
	sink := &failingWriter{}
	live := newLiveSink(sink)
	attempt := live.startAttempt("job-001")
	for i := 0; i < 50; i++ {
		if n, err := attempt.Stdout().Write([]byte("line\n")); n != 5 || err != nil {
			t.Fatalf("write returned %d %v", n, err)
		}
	}
	attempt.close()
	next := live.startAttempt("job-002")
	_, _ = next.Stderr().Write([]byte("more\n"))
	next.close()
	if sink.calls != 1 {
		t.Fatalf("a failed writer was called %d times", sink.calls)
	}
}

// blockingWriter blocks every Write until release is closed.
type blockingWriter struct {
	release chan struct{}
	recordingWriter
}

func (w *blockingWriter) Write(p []byte) (int, error) {
	<-w.release
	return w.recordingWriter.Write(p)
}

var droppedMarker = regexp.MustCompile(`^\[job-001\] \[9l: (\d+) bytes of output dropped\]\n$`)

func TestLiveOutputDropsWhatDoesNotFitAndCountsIt(t *testing.T) {
	// This test counts bytes, so the drain must finish: a slow runner under
	// the race detector can take longer than the normal close bound.
	previous := liveCloseWait
	liveCloseWait = time.Minute
	t.Cleanup(func() { liveCloseWait = previous })
	sink := &blockingWriter{release: make(chan struct{})}
	attempt := newLiveSink(sink).startAttempt("job-001")
	line := strings.Repeat("x", 999) + "\n"
	const writes = 2000 // 2 MB, twice the queue
	started := time.Now()
	for i := 0; i < writes; i++ {
		_, _ = attempt.Stdout().Write([]byte(line))
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("writes waited for a blocked writer: %s", elapsed)
	}
	close(sink.release)
	attempt.close()
	written, dropped, markers := 0, 0, 0
	for _, call := range sink.lines() {
		if match := droppedMarker.FindStringSubmatch(call); match != nil {
			n, _ := strconv.Atoi(match[1])
			dropped += n
			markers++
			continue
		}
		if call != "[job-001 stdout] "+line {
			t.Fatalf("unexpected line %.60q", call)
		}
		written += len(line)
	}
	if markers == 0 || dropped == 0 || written+dropped != writes*len(line) {
		t.Fatalf("written %d + dropped %d (%d markers) != %d", written, dropped, markers, writes*len(line))
	}
}

func TestLiveOutputCloseIsBoundedWhenTheWriterBlocks(t *testing.T) {
	previous := liveCloseWait
	liveCloseWait = 100 * time.Millisecond
	t.Cleanup(func() { liveCloseWait = previous })
	sink := &blockingWriter{release: make(chan struct{})}
	defer close(sink.release)
	live := newLiveSink(sink)
	attempt := live.startAttempt("job-001")
	_, _ = attempt.Stdout().Write([]byte("stuck\n"))
	started := time.Now()
	attempt.close()
	next := live.startAttempt("job-002")
	_, _ = next.Stdout().Write([]byte("after\n"))
	next.close()
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("close waited %s for a blocked writer", elapsed)
	}
	if !live.disabled.Load() {
		t.Fatal("a stalled writer stays enabled")
	}
}

func TestLiveOutputParallelWorkersWriteWholeLines(t *testing.T) {
	var out recordingWriter
	live := newLiveSink(&out)
	const workers, linesEach = 8, 200
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			attempt := live.startAttempt(fmt.Sprintf("job-%03d", w))
			defer attempt.close()
			var text strings.Builder
			for i := 0; i < linesEach; i++ {
				fmt.Fprintf(&text, "worker %d line %d %s\n", w, i, strings.Repeat("y", 50))
			}
			// Cut at odd offsets so lines arrive split across writes.
			payload := text.String()
			for start := 0; start < len(payload); start += 777 {
				_, _ = attempt.Stdout().Write([]byte(payload[start:min(start+777, len(payload))]))
			}
		}(w)
	}
	wg.Wait()
	shape := regexp.MustCompile(`^\[job-(\d{3}) stdout\] worker (\d+) line (\d+) y{50}\n$`)
	next := map[string]int{}
	calls := out.lines()
	for _, call := range calls {
		match := shape.FindStringSubmatch(call)
		if match == nil {
			t.Fatalf("not one whole line: %q", call)
		}
		job, _ := strconv.Atoi(match[1])
		worker, _ := strconv.Atoi(match[2])
		index, _ := strconv.Atoi(match[3])
		if job != worker || index != next[match[1]] {
			t.Fatalf("out of order or mixed: %q", call)
		}
		next[match[1]]++
	}
	if len(calls) != workers*linesEach {
		t.Fatalf("got %d lines, want %d", len(calls), workers*linesEach)
	}
}

func TestLiveOutputLeavesNoGoroutineBehind(t *testing.T) {
	before := runtime.NumGoroutine()
	var out recordingWriter
	live := newLiveSink(&out)
	for i := 0; i < 50; i++ {
		attempt := live.startAttempt("job-001")
		_, _ = attempt.Stdout().Write([]byte("partial"))
		attempt.close()
		attempt.close() // a second close is harmless
	}
	deadline := time.Now().Add(2 * time.Second)
	for runtime.NumGoroutine() > before && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if after := runtime.NumGoroutine(); after > before {
		t.Fatalf("goroutines: %d before, %d after", before, after)
	}
	if got := strings.Count(out.text(), "[job-001 stdout] partial\n"); got != 50 {
		t.Fatalf("flushed %d partial lines, want 50", got)
	}
}

// slowWriter accepts every line, but slowly, like a terminal over a slow link.
type slowWriter struct {
	delay time.Duration
	recordingWriter
}

func (w *slowWriter) Write(p []byte) (int, error) {
	time.Sleep(w.delay)
	return w.recordingWriter.Write(p)
}

// A terminal that is slow but working loses the rest of one attempt that
// printed too much, with a note, and keeps showing the attempts after it.
func TestLiveOutputSlowWriterAbandonsOneAttemptButStaysEnabled(t *testing.T) {
	previous := liveCloseWait
	liveCloseWait = 200 * time.Millisecond
	t.Cleanup(func() { liveCloseWait = previous })
	sink := &slowWriter{delay: 5 * time.Millisecond}
	live := newLiveSink(sink)
	chatty := live.startAttempt("job-001")
	for i := 0; i < 200; i++ { // about one second of writing at 5 ms a line
		_, _ = chatty.Stdout().Write([]byte(fmt.Sprintf("line %d\n", i)))
	}
	started := time.Now()
	chatty.close()
	if elapsed := time.Since(started); elapsed > liveCloseWait+liveAbandonWait+time.Second {
		t.Fatalf("close waited %s", elapsed)
	}
	if live.disabled.Load() {
		t.Fatal("a slow but working writer was disabled")
	}
	next := live.startAttempt("job-002")
	_, _ = next.Stdout().Write([]byte("after\n"))
	next.close()
	text := sink.text()
	if !strings.Contains(text, "[job-002 stdout] after\n") {
		t.Fatalf("the next attempt's output was not shown: %q", text[max(0, len(text)-300):])
	}
	if strings.Contains(text, "[job-001 stdout] line 199\n") {
		t.Fatal("the abandoned attempt's last line was still written")
	}
	if !strings.Contains(text, "[job-001] [9l: the rest of this attempt's output was not shown in time]\n") {
		t.Fatalf("no note for the abandoned lines: %q", text[max(0, len(text)-300):])
	}
}
