// Copyright 2026 Frafos GmbH. All rights reserved.
//
// Use of this source code is governed by a BSD-style license
// that can be found in the LICENSE.txt file in the root of the source
// tree.

package wtimer

import "fmt"

// lInfo encodes wheel position and flags without atomic operations.
// Same as tInfo, but non-atomic.
// Bit Layout:
//
//	31        24           16              0
//	| flags(8) | wheelNo(8) | wheelIdx(16) |
type lInfo uint32

func (li *lInfo) setFlags(mask uint8) {
	*li |= lInfo(mask) << flgsBpos
}

func (li *lInfo) resetFlags(mask uint8) {
	*li &^= lInfo(mask) << flgsBpos
}

func (li *lInfo) chgFlags(setMask, resetMask uint8) {
	setM := lInfo(setMask) << flgsBpos
	resetM := lInfo(resetMask) << flgsBpos
	*li = (*li &^ resetM) | setM
}

func (li *lInfo) assignFlags(newVal uint8) {
	v := lInfo(newVal) << flgsBpos
	resetM := lInfo(flgsMask) << flgsBpos
	*li = (*li &^ resetM) | v
}

func (li *lInfo) setWheel(w uint8, idx uint16) {
	v := lInfo(w)<<wNoBpos | lInfo(idx)
	resetM := lInfo(wNoMask)<<wNoBpos | lInfo(wIdxMask)
	*li = (*li &^ resetM) | v
}

func (li *lInfo) setAll(flgs uint8, w uint8, idx uint16) {
	*li = lInfo(flgs)<<flgsBpos | lInfo(w)<<wNoBpos | lInfo(idx)
}

func (li *lInfo) flags() uint8 {
	return uint8(*li >> flgsBpos)
}

func (li *lInfo) wheelPos() (uint8, uint16) {
	return uint8((*li >> wNoBpos) & lInfo(wNoMask)),
		uint16(*li & lInfo(wIdxMask))
}

func (li *lInfo) getAll() (uint8, uint8, uint16) {
	f := *li >> flgsBpos
	w := (*li >> wNoBpos) & lInfo(wNoMask)
	idx := *li & lInfo(wIdxMask)
	return uint8(f), uint8(w), uint16(idx)
}

func (li lInfo) String() string {
	f, w, i := li.getAll()
	return fmt.Sprintf("%02x:%02x:%d", f, w, i)
}
