package compress

import "math"

// Estimate returns a normalized compressibility estimate of block b.
// Values close to zero are likely uncompressible.
// Values above 0.1 are likely to be compressible.
// Values above 0.5 are very compressible.
// Very small lengths will return 0.
func Estimate(b []byte) float64 {
	if len(b) < 16 {
		return 0
	}

	// Correctly predicted order 1
	hits := 0
	lastMatch := false
	var o1 [256]byte
	var hist [256]int
	c1 := byte(0)
	for _, c := range b {
		if c == o1[c1] {
			// We only count a hit if there was two correct predictions in a row.
			if lastMatch {
				hits++
			}
			lastMatch = true
		} else {
			lastMatch = false
		}
		o1[c1] = c
		c1 = c
		hist[c]++
	}

	// Use x^0.6 to give better spread
	prediction := math.Pow(float64(hits)/float64(len(b)), 0.6)

	// Calculate histogram distribution
	variance := float64(0)
	avg := float64(len(b)) / 256

	for _, v := range hist {
		Δ := float64(v) - avg
		variance += Δ * Δ
	}

	stddev := math.Sqrt(float64(variance)) / float64(len(b))
	exp := math.Sqrt(1 / float64(len(b)))

	// Subtract expected stddev
	stddev -= exp
	if stddev < 0 {
		stddev = 0
	}
	stddev *= 1 + exp

	// Use x^0.4 to give better spread
	entropy := math.Pow(stddev, 0.4)

	// 50/50 weight between prediction and histogram distribution
	return math.Pow((prediction+entropy)/2, 0.9)
}
