package runner

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"
)

// Live output (`9l run --show-output`) copies each worker's stdout and stderr
// to a caller's writer while the attempt runs. It is a view, never evidence:
// nothing here reaches the evidence stream, receipts, logs or results.
//
// The copy must never change an attempt's outcome. A process output sink that
// fails fails the command, and one that blocks blocks the drain, so the
// writers handed to the process never fail and never wait: bytes go into a
// bounded queue that a goroutine drains into the caller's writer, and what
// does not fit is dropped and counted.
const (
	// liveLineLimit bounds one written line; a longer line is split.
	liveLineLimit = 16 << 10
	// liveQueueChunks and liveQueueBytes bound what waits for the writer.
	liveQueueChunks = 256
	liveQueueBytes  = 1 << 20
)

// liveCloseWait bounds how long an attempt waits for its queued lines to reach
// the writer. When it passes, the attempt's remaining lines are abandoned; a
// writer stuck inside one write for that long is disabled for the rest of the
// run, while one that is merely slow keeps serving later attempts. It is a
// variable so tests can change it.
var liveCloseWait = 2 * time.Second

// liveSink is the caller's writer, shared by every attempt of a run. One mutex
// orders whole lines from parallel workers. The first failed write, or one
// stuck for longer than liveCloseWait, disables it for the rest of the run.
type liveSink struct {
	mu       sync.Mutex
	w        io.Writer
	disabled atomic.Bool
	// writing is when the write in progress started, in Unix nanoseconds, or
	// zero; it tells a stuck writer from a slow one.
	writing atomic.Int64
}

func newLiveSink(w io.Writer) *liveSink { return &liveSink{w: w} }

func (s *liveSink) writeLine(line []byte) {
	if s.disabled.Load() {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.disabled.Load() {
		return
	}
	s.writing.Store(time.Now().UnixNano())
	n, err := s.w.Write(line)
	s.writing.Store(0)
	if err != nil || n != len(line) {
		s.disabled.Store(true)
	}
}

// stuck reports whether a write has been in progress for at least d.
func (s *liveSink) stuck(d time.Duration) bool {
	started := s.writing.Load()
	return started != 0 && time.Since(time.Unix(0, started)) >= d
}

type liveChunk struct {
	stream int
	data   []byte
}

// liveAttempt is one worker's live view: a stdout and a stderr writer that
// queue bytes, and a goroutine that turns them into prefixed, sanitized lines.
type liveAttempt struct {
	sink     *liveSink
	prefixes [2]string
	queue    chan liveChunk
	queued   atomic.Int64
	dropped  atomic.Int64
	stopped  atomic.Bool
	// abandoned is set when close gave up waiting: the drain discards the
	// rest of this attempt's lines instead of writing them late.
	abandoned atomic.Bool
	stop      chan struct{}
	done      chan struct{}
	// pending holds each stream's unfinished line; only the drain uses it.
	pending [2][]byte
}

// startAttempt begins a live view for one job's worker. Call close when the
// worker's output has been drained.
func (s *liveSink) startAttempt(jobID string) *liveAttempt {
	a := &liveAttempt{
		sink:     s,
		prefixes: [2]string{"[" + jobID + " stdout] ", "[" + jobID + " stderr] "},
		queue:    make(chan liveChunk, liveQueueChunks),
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
	}
	go a.run()
	return a
}

// Stdout and Stderr are the writers to tee the worker's streams into. They
// always accept the whole write and return at once.
func (a *liveAttempt) Stdout() io.Writer { return liveStream{a, 0} }
func (a *liveAttempt) Stderr() io.Writer { return liveStream{a, 1} }

type liveStream struct {
	attempt *liveAttempt
	index   int
}

func (s liveStream) Write(p []byte) (int, error) {
	s.attempt.enqueue(s.index, p)
	return len(p), nil
}

func (a *liveAttempt) enqueue(index int, p []byte) {
	if len(p) == 0 || a.sink.disabled.Load() || a.stopped.Load() {
		return
	}
	n := int64(len(p))
	if a.queued.Add(n) > liveQueueBytes {
		a.queued.Add(-n)
		a.dropped.Add(n)
		return
	}
	select {
	case a.queue <- liveChunk{stream: index, data: bytes.Clone(p)}:
	default:
		a.queued.Add(-n)
		a.dropped.Add(n)
	}
}

func (a *liveAttempt) run() {
	defer close(a.done)
	for {
		select {
		case chunk := <-a.queue:
			a.consume(chunk)
		case <-a.stop:
			for {
				select {
				case chunk := <-a.queue:
					a.consume(chunk)
				default:
					if !a.abandoned.Load() {
						a.flush(0)
						a.flush(1)
						a.reportDropped()
					}
					return
				}
			}
		}
	}
}

func (a *liveAttempt) consume(chunk liveChunk) {
	a.queued.Add(-int64(len(chunk.data)))
	if a.abandoned.Load() {
		return
	}
	buffer := append(a.pending[chunk.stream], chunk.data...)
	for {
		end := bytes.IndexByte(buffer, '\n')
		if end < 0 {
			break
		}
		a.emit(chunk.stream, buffer[:end])
		buffer = buffer[end+1:]
	}
	for len(buffer) >= liveLineLimit {
		cut := runeBoundary(buffer, liveLineLimit)
		a.emit(chunk.stream, buffer[:cut])
		buffer = buffer[cut:]
	}
	a.pending[chunk.stream] = append([]byte(nil), buffer...)
	// The marker follows everything that was queued before the drop.
	if len(a.queue) == 0 {
		a.reportDropped()
	}
}

// flush writes a stream's unfinished line, as at the end of the process.
func (a *liveAttempt) flush(stream int) {
	if len(a.pending[stream]) > 0 {
		a.emit(stream, a.pending[stream])
		a.pending[stream] = nil
	}
}

func (a *liveAttempt) reportDropped() {
	if n := a.dropped.Swap(0); n > 0 {
		jobPrefix := strings.Replace(a.prefixes[0], " stdout] ", "] ", 1)
		a.sink.writeLine(fmt.Appendf(nil, "%s[9l: %d bytes of output dropped]\n", jobPrefix, n))
	}
}

// emit writes one line, at most liveLineLimit bytes per written line.
func (a *liveAttempt) emit(stream int, line []byte) {
	for !a.abandoned.Load() {
		piece := line
		if len(piece) > liveLineLimit {
			piece = piece[:runeBoundary(piece, liveLineLimit)]
		}
		a.sink.writeLine([]byte(a.prefixes[stream] + sanitizeLiveLine(piece) + "\n"))
		line = line[len(piece):]
		if len(line) == 0 {
			return
		}
	}
}

// close stops the view after the worker's output has been drained: queued
// lines and unfinished lines are written, then the goroutine exits. It waits
// at most liveCloseWait. Past that, the attempt's remaining lines are
// abandoned and one line says so; the writer is disabled only when a single
// write has been stuck for that long, so a slow terminal still shows the
// attempts that follow.
func (a *liveAttempt) close() {
	if !a.stopped.CompareAndSwap(false, true) {
		return
	}
	close(a.stop)
	timer := time.NewTimer(liveCloseWait)
	defer timer.Stop()
	select {
	case <-a.done:
	case <-timer.C:
		a.abandoned.Store(true)
		// One line takes a working terminal far less than half the wait.
		if a.sink.stuck(liveCloseWait / 2) {
			a.sink.disabled.Store(true)
			return
		}
		note := func() {
			jobPrefix := strings.Replace(a.prefixes[0], " stdout] ", "] ", 1)
			a.sink.writeLine([]byte(jobPrefix + "[9l: the rest of this attempt's output was not shown in time]\n"))
		}
		// The drain stops at its next line; give it a moment so the note
		// normally precedes whatever the run prints next.
		select {
		case <-a.done:
			note()
		case <-time.After(liveAbandonWait):
			go func() { <-a.done; note() }()
		}
	}
}

// liveAbandonWait bounds the extra wait for an abandoned drain to stop.
const liveAbandonWait = 250 * time.Millisecond

// runeBoundary returns a cut at or below limit that does not split a UTF-8
// sequence, or limit when no boundary is near.
func runeBoundary(b []byte, limit int) int {
	for cut := limit; cut > limit-utf8.UTFMax && cut > 0; cut-- {
		if utf8.RuneStart(b[cut]) {
			return cut
		}
	}
	return limit
}

// sanitizeLiveLine removes terminal escapes and other control characters,
// so worker output cannot move the cursor or retitle the terminal, and then
// applies the evidence redaction. Redaction is best effort.
func sanitizeLiveLine(line []byte) string {
	text := StripTerminalEscapes(strings.ToValidUTF8(string(line), "�"))
	text = strings.Map(func(r rune) rune {
		if r == '\t' || (r >= 0x20 && r != 0x7f && (r < 0x80 || r > 0x9f)) {
			return r
		}
		return -1
	}, text)
	return RedactText(text)
}
