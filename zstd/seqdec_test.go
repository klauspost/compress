package zstd

import (
	"testing"
)

func TestSequenceDecsAdjustOffset(t *testing.T) {
	type result struct {
		offset     int
		prevOffset [3]int
	}

	tc := []struct {
		offset     int
		litLen     int
		offsetB    uint8
		prevOffset [3]int

		res result
	}{{
		offset:     444,
		litLen:     0,
		offsetB:    42,
		prevOffset: [3]int{111, 222, 333},

		res: result{
			offset:     444,
			prevOffset: [3]int{444, 111, 222},
		},
	}, {
		offset:     0,
		litLen:     1,
		offsetB:    0,
		prevOffset: [3]int{111, 222, 333},

		res: result{
			offset:     111,
			prevOffset: [3]int{111, 222, 333},
		},
	}, {
		offset:     -1,
		litLen:     0,
		offsetB:    0,
		prevOffset: [3]int{111, 222, 333},

		res: result{
			offset:     111,
			prevOffset: [3]int{111, 222, 333},
		},
	}, {
		offset:     1,
		litLen:     1,
		offsetB:    0,
		prevOffset: [3]int{111, 222, 333},

		res: result{
			offset:     222,
			prevOffset: [3]int{222, 111, 333},
		},
	}, {
		offset:     2,
		litLen:     1,
		offsetB:    0,
		prevOffset: [3]int{111, 222, 333},

		res: result{
			offset:     333,
			prevOffset: [3]int{333, 111, 222},
		},
	}, {
		offset:     3,
		litLen:     1,
		offsetB:    0,
		prevOffset: [3]int{111, 222, 333},

		res: result{
			offset:     110, // s.prevOffset[0] - 1
			prevOffset: [3]int{110, 111, 222},
		},
	}, {
		offset:     3,
		litLen:     1,
		offsetB:    0,
		prevOffset: [3]int{1, 222, 333},

		res: result{
			offset:     1,
			prevOffset: [3]int{1, 1, 222},
		},
	},
	}

	for i := range tc {
		// given
		var sd sequenceDecs
		for j := 0; j < 3; j++ {
			sd.prevOffset[j] = tc[i].prevOffset[j]
		}

		// when
		offset := sd.adjustOffset(tc[i].offset, tc[i].litLen, tc[i].offsetB)

		// then
		if offset != tc[i].res.offset {
			t.Logf("result:   %d", offset)
			t.Logf("expected: %d", tc[i].res.offset)
			t.Errorf("testcase #%d: wrong function result", i)
		}

		for j := 0; j < 3; j++ {
			if sd.prevOffset[j] != tc[i].res.prevOffset[j] {
				t.Logf("result:   %v", sd.prevOffset)
				t.Logf("expected: %v", tc[i].res.prevOffset)
				t.Errorf("testcase #%d: sd.prevOffset got wrongly updated", i)
				break
			}
		}
	}
}
