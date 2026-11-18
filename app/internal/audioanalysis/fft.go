package audioanalysis

import (
	"math"
	"math/bits"
)

// fft computes the discrete Fourier transform of x in place, using the
// textbook iterative radix-2 Cooley–Tukey algorithm (bit-reversal
// permutation, then log2(n) butterfly passes). len(x) must be a power of
// two. The transform is unnormalized: X[k] = Σ x[n]·e^(−2πikn/N).
//
// The package needs only a few thousand transforms of at most a few
// hundred thousand points per song, so clarity wins over a split-radix or
// real-input specialization; twiddles are recomputed per pass from one
// cos/sin per butterfly column, which keeps the error well below what any
// caller here can notice.
func fft(x []complex128) {
	n := len(x)
	if n <= 1 {
		return
	}
	if n&(n-1) != 0 {
		panic("audioanalysis: fft length must be a power of two")
	}
	shift := 64 - bits.TrailingZeros(uint(n))
	for i := range n {
		j := int(bits.Reverse64(uint64(i)) >> shift)
		if j > i {
			x[i], x[j] = x[j], x[i]
		}
	}
	for size := 2; size <= n; size <<= 1 {
		half := size / 2
		step := -2 * math.Pi / float64(size)
		for k := range half {
			s, c := math.Sincos(step * float64(k))
			w := complex(c, s)
			for start := 0; start < n; start += size {
				a := x[start+k]
				b := x[start+k+half] * w
				x[start+k] = a + b
				x[start+k+half] = a - b
			}
		}
	}
}

// ifft computes the inverse transform in place, normalized so that
// ifft(fft(x)) == x. It uses the conjugation identity
// IDFT(X) = conj(DFT(conj(X)))/N rather than a second butterfly loop.
func ifft(x []complex128) {
	for i, v := range x {
		x[i] = complex(real(v), -imag(v))
	}
	fft(x)
	inv := 1 / float64(len(x))
	for i, v := range x {
		x[i] = complex(real(v)*inv, -imag(v)*inv)
	}
}

// nextPow2 returns the smallest power of two ≥ n (and ≥ 1).
func nextPow2(n int) int {
	p := 1
	for p < n {
		p <<= 1
	}
	return p
}

// hann returns an n-point periodic Hann window, the usual choice for
// STFT analysis: its −31 dB first sidelobe keeps a loud partial from
// smearing into neighbouring semitones, and 50% overlap sums to a
// constant.
func hann(n int) []float64 {
	w := make([]float64, n)
	for i := range w {
		w[i] = 0.5 - 0.5*math.Cos(2*math.Pi*float64(i)/float64(n))
	}
	return w
}

// stft is a small reusable helper for the two short-time analyses in
// this package (onset envelope and chroma). It owns its window and its
// scratch buffer so per-frame work allocates nothing.
type stft struct {
	size   int
	window []float64
	buf    []complex128
	mag    []float64 // |X[k]| for k in [0, size/2]
}

func newSTFT(size int) *stft {
	return &stft{
		size:   size,
		window: hann(size),
		buf:    make([]complex128, size),
		mag:    make([]float64, size/2+1),
	}
}

// magnitudes windows x[start:start+size] (zero-padding past the end of
// x), transforms it and returns the magnitude spectrum. The returned
// slice is owned by s and overwritten by the next call.
func (s *stft) magnitudes(x []float32, start int) []float64 {
	for i := range s.size {
		v := 0.0
		if j := start + i; j >= 0 && j < len(x) {
			v = float64(x[j]) * s.window[i]
		}
		s.buf[i] = complex(v, 0)
	}
	fft(s.buf)
	for k := range s.mag {
		re, im := real(s.buf[k]), imag(s.buf[k])
		s.mag[k] = math.Sqrt(re*re + im*im)
	}
	return s.mag
}
