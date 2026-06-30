//go:build arm64 && !appengine && !noasm && gc

package zstd

import (
	"errors"
	"fmt"
	"reflect"
	"testing"
)

// buildDtableRef is a copy of the pure-Go reference algorithm
// (fse_decoder_generic.go), kept here so the arm64 asm implementation can be
// differentially tested against it even though the generic file is not
// compiled on arm64.
func buildDtableRef(s *fseDecoder) error {
	tableSize := uint32(1 << s.actualTableLog)
	highThreshold := tableSize - 1
	symbolNext := s.stateTable[:256]

	{
		for i, v := range s.norm[:s.symbolLen] {
			if v == -1 {
				s.dt[highThreshold].setAddBits(uint8(i))
				highThreshold--
				v = 1
			}
			symbolNext[i] = uint16(v)
		}
	}

	{
		tableMask := tableSize - 1
		step := tableStep(tableSize)
		position := uint32(0)
		for ss, v := range s.norm[:s.symbolLen] {
			for i := 0; i < int(v); i++ {
				s.dt[position].setAddBits(uint8(ss))
				for {
					position = (position + step) & tableMask
					if position <= highThreshold {
						break
					}
				}
			}
		}
		if position != 0 {
			return errors.New("corrupted input (position != 0)")
		}
	}

	{
		tableSize := uint16(1 << s.actualTableLog)
		for u, v := range s.dt[:tableSize] {
			symbol := v.addBits()
			nextState := symbolNext[symbol]
			symbolNext[symbol] = nextState + 1
			nBits := s.actualTableLog - byte(highBits(uint32(nextState)))
			s.dt[u&maxTableMask].setNBits(nBits)
			newState := (nextState << nBits) - tableSize
			if newState > tableSize {
				return fmt.Errorf("newState (%d) outside table size (%d)", newState, tableSize)
			}
			if newState == uint16(u) && nBits == 0 {
				return fmt.Errorf("newState (%d) == oldState (%d) and no bits", newState, u)
			}
			s.dt[u&maxTableMask].setNewState(newState)
		}
	}
	return nil
}

func TestBuildDtableARM64MatchesReference(t *testing.T) {
	for name, mk := range predefinedFSEInputs() {
		t.Run(name, func(t *testing.T) {
			got := mk()
			if err := got.buildDtable(); err != nil {
				t.Fatalf("asm buildDtable: %v", err)
			}
			want := mk()
			if err := buildDtableRef(want); err != nil {
				t.Fatalf("reference buildDtable: %v", err)
			}
			tableSize := 1 << got.actualTableLog
			if !reflect.DeepEqual(got.dt[:tableSize], want.dt[:tableSize]) {
				for i := range tableSize {
					if got.dt[i] != want.dt[i] {
						t.Errorf("dt[%d]: asm %#016x, ref %#016x", i, uint64(got.dt[i]), uint64(want.dt[i]))
					}
				}
				t.FailNow()
			}
			if got.stateTable != want.stateTable {
				t.Errorf("stateTable mismatch:\nasm %v\nref %v", got.stateTable, want.stateTable)
			}
		})
	}
}
