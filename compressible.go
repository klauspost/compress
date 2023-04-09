package compress

import (
	"math"
)

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

// ShannonEntropyBits returns the number of bits minimum required to represent
// an entropy encoding of the input bytes.
// https://en.wiktionary.org/wiki/Shannon_entropy
func ShannonEntropyBits(b []byte) int {
	if len(b) == 0 {
		return 0
	}
	var hist [256]int
	for _, c := range b {
		hist[c]++
	}
	shannon := float64(0)
	invTotal := 1.0 / float64(len(b))
	for _, v := range hist[:] {
		if v > 0 {
			n := float64(v)
			shannon += math.Ceil(-math.Log2(n*invTotal) * n)
		}
	}
	return int(math.Ceil(shannon))
}

type DynamicEstimate struct {
	MaxBits float32

	histogram [256]float32
	histN     float32

	prevHist [256]float32
	prevN    float32
}

// Seed will clear the estimate and set the previous block to be b
func (d *DynamicEstimate) Seed(b []byte) {
	d.histogram = [256]float32{}
	d.prevHist = [256]float32{}
	d.histN = 0
	d.prevN = float32(len(b))
	for _, v := range b {
		d.prevHist[v]++
	}
}

// SeedHist will clear the estimate and set the previous block to be the histogram provided.
// The histogram is not needed to be 256 entries.
// In such case the remaining will be 0.
func (d *DynamicEstimate) SeedHist(hist []int) {
	d.histogram = [256]float32{}
	d.prevHist = [256]float32{}
	d.histN = 0
	d.prevN = 0
	for i, v := range hist {
		vf := float32(v)
		d.prevN += vf
		d.prevHist[i] = vf
	}
}

func (d *DynamicEstimate) EstByte(v byte) float32 {
	invTotal := 1.0 / (d.histN + d.prevN + 1)
	n := d.histogram[v] + d.prevHist[v] + 1
	//fmt.Printf("total: %f, n: %f res: %f\n", d.histN+d.prevN+1, d.histogram[v]+d.prevHist[v]+1, -mFastLog2(n*invTotal))
	return minFl32(d.MaxBits, -mFastLog2(n*invTotal))
}

func (d *DynamicEstimate) EstBits(b []byte) float32 {
	var bitsUsed float32
	fLen := float32(len(b))
	invTotal := 1.0 / (d.histN + d.prevN + fLen)
	var tmp [256]float32
	for _, v := range b {
		tmp[v]++
	}
	for _, v := range b {
		n := d.histogram[v] + d.prevHist[v] + tmp[v]
		bitsUsed += minFl32(d.MaxBits, -mFastLog2(n*invTotal))
	}
	return bitsUsed
}

func (d *DynamicEstimate) Cycle() {
	d.prevHist = d.histogram
	d.prevN = d.histN
	d.histogram = [256]float32{}
	d.histN = 0
}

func (d *DynamicEstimate) CycleFactor(n float32) {
	for i := range d.prevHist {
		d.prevHist[i] = d.prevHist[i]*n + d.histogram[i]
	}
	d.prevN = d.prevN*n + d.histN
	d.histogram = [256]float32{}
	d.histN = 0
}

func (d *DynamicEstimate) AddByte(v byte) {
	d.histogram[v]++
	d.histN++
}

func (d *DynamicEstimate) Add(b []byte) {
	for _, v := range b {
		d.histogram[v]++
		d.histN++
	}
}

// from https://stackoverflow.com/a/28730362
func mFastLog2(val float32) float32 {
	ux := int32(math.Float32bits(val))
	log2 := (float32)(((ux >> 23) & 255) - 128)
	ux &= -0x7f800001
	ux += 127 << 23
	uval := math.Float32frombits(uint32(ux))
	log2 += (-0.34484843*uval+2.02466578)*uval - 0.67487759
	return log2
}

func minFl32(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}
