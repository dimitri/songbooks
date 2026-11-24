package stems

import (
	"bytes"
	"context"
	"fmt"
	"testing"
)

// TestProgress: Demucs's progress bar read as it's written, a bar cut
// between two writes, every 5 % at most and at 100 %; the output kept.
func TestProgress(t *testing.T) {
	var got []float64
	ctx := WithProgress(context.Background(), 0.05, func(p float64) { got = append(got, p) })
	var buf bytes.Buffer
	w := stderrFor(ctx, &buf)
	for _, chunk := range []string{
		"  0%|          | 0.0/210.6 [00:00<?, ?seconds/s]\r",
		"  2%|▏         | 5.85/210.6 [00:02<01:10, 2.9seconds/s]\r",
		"  7%|▋         | 14.6/210.6 [00:05<01:05]\r  4",
		"2%|████▏     | 87.75/210.6 [00:31<00:44]\r",
		" 43%|████▎     | 90.6/210.6 [00:32<00:43]\r",
		"100%|██████████| 210.6/210.6 [01:13<00:00]\n",
	} {
		w.Write([]byte(chunk))
	}
	if fmt.Sprint(got) != "[0 0.07 0.42 1]" {
		t.Errorf("reported %v", got)
	}
	if !bytes.Contains(buf.Bytes(), []byte("210.6/210.6")) {
		t.Error("the output lost")
	}
	// No progress asked: the writer itself.
	if stderrFor(context.Background(), &buf) != &buf {
		t.Error("a writer wrapped for nothing")
	}
}
