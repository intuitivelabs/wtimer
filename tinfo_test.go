package wtimer

import (
	"math/rand"
	"sync"
	"testing"
	"unsafe"
)

func TestTinfoConsts(t *testing.T) {
	var x tInfo
	fmask := (flgsMask << flgsBpos) | (wNoMask << wNoBpos) | wIdxMask
	maxVal := (1 << uint64(unsafe.Sizeof(x.atomicV)*8)) - 1
	if fmask != maxVal {
		t.Errorf("max val does not corresp. to full maks: 0x%x <> 0x%x\n",
			maxVal, fmask)
	}
}

func TestTinfoOps(t *testing.T) {
	const iterations = 100000
	for i := 0; i < iterations; i++ {
		var x tInfo
		f0 := rand.Intn(256)
		mset := rand.Intn(256)
		mreset := rand.Intn(256)
		w := rand.Intn(256)
		idx := rand.Intn(65536)

		fRes := uint8(f0 & ^mreset | mset)
		mix := rand.Intn(10)
		switch mix {
		case 0:
			// set flags, then wheel
			x.assignFlags(uint8(f0))
			x.resetFlags(uint8(mreset))
			x.setFlags(uint8(mset))
			x.setWheel(uint8(w), uint16(idx))
		case 1:
			// set  wheel, then flags
			x.setWheel(uint8(w), uint16(idx))
			x.assignFlags(uint8(f0))
			x.resetFlags(uint8(mreset))
			x.setFlags(uint8(mset))
		case 2:
			// mix flags, & wheel
			x.assignFlags(uint8(f0))
			x.resetFlags(uint8(mreset))
			x.setWheel(uint8(w), uint16(idx))
			x.setFlags(uint8(mset))
		case 3:
			// mix flags, & wheel
			x.assignFlags(uint8(f0))
			x.setWheel(uint8(w), uint16(idx))
			x.resetFlags(uint8(mreset))
			x.setFlags(uint8(mset))
		case 4:
			// set flags, then wheel
			x.assignFlags(uint8(f0))
			x.chgFlags(uint8(mset), uint8(mreset))
			x.setWheel(uint8(w), uint16(idx))
		case 5:
			// set  wheel then flags
			x.setWheel(uint8(w), uint16(idx))
			x.assignFlags(uint8(f0))
			x.chgFlags(uint8(mset), uint8(mreset))
		case 6:
			//mix  wheel and flags
			x.assignFlags(uint8(f0))
			x.setWheel(uint8(w), uint16(idx))
			x.chgFlags(uint8(mset), uint8(mreset))
		case 7:
			x.setAll(uint8(f0), uint8(w), uint16(idx))
			x.chgFlags(uint8(mset), uint8(mreset))
		case 8:
			var wg sync.WaitGroup
			wg.Add(2)
			go func() {
				x.assignFlags(uint8(f0))
				x.resetFlags(uint8(mreset))
				x.setFlags(uint8(mset))
				wg.Done()
			}()
			go func() {
				x.setWheel(uint8(w), uint16(idx))
				wg.Done()
			}()
			wg.Wait()
		case 9:
			var wg sync.WaitGroup
			wg.Add(2)
			go func() {
				x.setWheel(uint8(w), uint16(idx))
				wg.Done()
			}()
			go func() {
				x.assignFlags(uint8(f0))
				x.resetFlags(uint8(mreset))
				x.setFlags(uint8(mset))
				wg.Done()
			}()
			wg.Wait()
		default:
			t.Fatalf("uncovered internal test case %d\n", mix)
		}
		if x.flags() != fRes {
			t.Errorf("flags mismatch, expected 0x%x, got 0x%x"+
				" 0x%x & ^0x%x | 0x%x  (mix %d)\n",
				fRes, x.flags(), f0, mreset, mset, mix)
		}

		w1, idx1 := x.wheelPos()
		if w1 != uint8(w) || idx1 != uint16(idx) {
			t.Errorf("wheel mismatch, expected %d/%d, got %d/%d (mix %d)\n",
				w, idx, w1, idx1, mix)
		}
	}
}
