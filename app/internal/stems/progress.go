package stems

import (
	"context"
	"io"
	"regexp"
	"strconv"
	"sync"
)

// A split's progress, for the song page's Melody view (migration 0134):
// Demucs draws a progress bar on its standard error as it separates the
// recording's segments (" 42%|████▎     | 87.75/210.6 [00:31<00:44, …]"),
// which run reads as it comes when the context asks for it.

type progressKey struct{}

// WithProgress is ctx with report called as the commands run under it
// report their progress (0..1): Demucs's bar, every step of at least
// step (and at 100%).
func WithProgress(ctx context.Context, step float64, report func(float64)) context.Context {
	return context.WithValue(ctx, progressKey{}, &progressReporter{step: step, report: report, last: -1})
}

type progressReporter struct {
	step   float64
	report func(float64)
	mu     sync.Mutex
	last   float64
}

func (p *progressReporter) seen(f float64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if f < p.last+p.step && !(f >= 1 && p.last < 1) {
		return
	}
	p.last = f
	p.report(f)
}

// tqdmPercent is a tqdm bar's percentage.
var tqdmPercent = regexp.MustCompile(`(\d{1,3})%\|`)

// progressWriter passes what's written on to w and reads the progress
// bar in it.
type progressWriter struct {
	w    io.Writer
	p    *progressReporter
	tail []byte
}

func (pw *progressWriter) Write(b []byte) (int, error) {
	n, err := pw.w.Write(b)
	// A bar can be cut between two writes: the end of the last one kept.
	buf := append(pw.tail, b...)
	for _, m := range tqdmPercent.FindAllSubmatch(buf, -1) {
		if v, e := strconv.Atoi(string(m[1])); e == nil && v <= 100 {
			pw.p.seen(float64(v) / 100)
		}
	}
	if len(buf) > 16 {
		buf = buf[len(buf)-16:]
	}
	pw.tail = append(pw.tail[:0], buf...)
	return n, err
}

// stderrFor is where a command run under ctx writes its standard error:
// w, read for a progress bar when ctx asks for its progress.
func stderrFor(ctx context.Context, w io.Writer) io.Writer {
	p, _ := ctx.Value(progressKey{}).(*progressReporter)
	if p == nil {
		return w
	}
	return &progressWriter{w: w, p: p}
}
