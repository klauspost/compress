package zstd

import "testing"

// predefinedFSEInputs returns the three RFC default distributions
// (litLength, offset, matchLength) as fseDecoders, ready for buildDtable.
// Defined without build constraints so the benchmark can run under both the
// asm and -tags noasm (pure-Go) builds for an apples-to-apples comparison.
func predefinedFSEInputs() map[string]func() *fseDecoder {
	return map[string]func() *fseDecoder{
		"litLength": func() *fseDecoder {
			s := &fseDecoder{actualTableLog: 6, symbolLen: 36}
			copy(s.norm[:], []int16{4, 3, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 1, 1, 1,
				2, 2, 2, 2, 2, 2, 2, 2, 2, 3, 2, 1, 1, 1, 1, 1,
				-1, -1, -1, -1})
			return s
		},
		"offset": func() *fseDecoder {
			s := &fseDecoder{actualTableLog: 5, symbolLen: 29}
			copy(s.norm[:], []int16{
				1, 1, 1, 1, 1, 1, 2, 2, 2, 1, 1, 1, 1, 1, 1, 1,
				1, 1, 1, 1, 1, 1, 1, 1, -1, -1, -1, -1, -1})
			return s
		},
		"matchLength": func() *fseDecoder {
			s := &fseDecoder{actualTableLog: 6, symbolLen: 53}
			copy(s.norm[:], []int16{
				1, 4, 3, 2, 2, 2, 2, 2, 2, 1, 1, 1, 1, 1, 1, 1,
				1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1,
				1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, -1, -1,
				-1, -1, -1, -1, -1})
			return s
		},
	}
}

func BenchmarkBuildDtable(b *testing.B) {
	for name, mk := range predefinedFSEInputs() {
		b.Run(name, func(b *testing.B) {
			s := mk()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				// Reset the mutated parts so each iteration does real work.
				for j := range s.dt {
					s.dt[j] = 0
				}
				for j := range s.stateTable {
					s.stateTable[j] = 0
				}
				if err := s.buildDtable(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
