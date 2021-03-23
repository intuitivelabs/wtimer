// Copyright 2021 Intuitive Labs GmbH. All rights reserved.
//
// Use of this source code is governed by a BSD-style license
// that can be found in the LICENSE.txt file in the root of the source
// tree.

package wtimer

import (
	"fmt"
	"sync/atomic"
)

// tInfo encodes information about the list/wheel a timer is on and its
// current flags (which are in fact a combination of state + flags).
// The maximum wheel size is 2^16.
// It is accessed atomically.
//
// Internal encoding format:
//   31    24    16      0
//   | flgs | wNo | wIdx |
// where flgs = flags, wNo = wheel number and wIdx = index inside the wheel.
//
type tInfo struct {
	atomicV uint32
}

const (
	flgsMask = 255
	wNoMask  = 255
	wIdxMask = 65535
	flgsBpos = 24
	wNoBpos  = 16
)

func (t *tInfo) setFlags(mask uint8) {
	f := uint32(mask) << flgsBpos
	for {
		crt := atomic.LoadUint32(&t.atomicV)
		if atomic.CompareAndSwapUint32(&t.atomicV, crt, crt|f) {
			break
		}
	}
}

func (t *tInfo) resetFlags(mask uint8) {
	f := uint32(mask) << flgsBpos
	for {
		crt := atomic.LoadUint32(&t.atomicV)
		if atomic.CompareAndSwapUint32(&t.atomicV, crt, crt & ^f) {
			break
		}
	}
}

// chgFlags resets the flags in resetMask and sets the bits in setMask
func (t *tInfo) chgFlags(setMask, resetMask uint8) {
	setM := uint32(setMask) << flgsBpos
	resetM := uint32(resetMask) << flgsBpos
	for {
		crt := atomic.LoadUint32(&t.atomicV)
		if atomic.CompareAndSwapUint32(&t.atomicV, crt, (crt & ^resetM)|setM) {
			break
		}
	}
}

func (t *tInfo) assignFlags(newVal uint8) {
	v := uint32(newVal) << flgsBpos
	resetM := uint32(flgsMask) << flgsBpos
	for {
		crt := atomic.LoadUint32(&t.atomicV)
		if atomic.CompareAndSwapUint32(&t.atomicV, crt, (crt & ^resetM)|v) {
			break
		}
	}
}

func (t *tInfo) setWheel(w uint8, idx uint16) {
	v := uint32(w)<<wNoBpos | uint32(idx)
	resetM := uint32(wNoMask)<<wNoBpos | uint32(wIdxMask)
	for {
		crt := atomic.LoadUint32(&t.atomicV)
		if atomic.CompareAndSwapUint32(&t.atomicV, crt, (crt & ^resetM)|v) {
			break
		}
	}
}

func (t *tInfo) setAll(flgs uint8, w uint8, idx uint16) {
	v := uint32(flgs)<<flgsBpos | uint32(w)<<wNoBpos | uint32(idx)
	atomic.StoreUint32(&t.atomicV, v)
}

func (t *tInfo) flags() uint8 {
	/*
		crt := atomic.LoadUint32(&t.atomicV)
		return uint8(crt >> flgsBpos)
	*/
	f, _, _ := t.getAll()
	return f
}

// return wheel number and index.
func (t *tInfo) wheelPos() (uint8, uint16) {
	/*
		crt := atomic.LoadUint32(&t.atomicV)
		w := (crt >> wNoBpos) & wNoMask
		idx := crt & wIdxMask
	*/
	_, w, idx := t.getAll()
	return uint8(w), uint16(idx)
}

// returns atomically flags, wheel number and index.
func (t *tInfo) getAll() (uint8, uint8, uint16) {
	crt := atomic.LoadUint32(&t.atomicV)
	f := crt >> flgsBpos
	w := (crt >> wNoBpos) & wNoMask
	idx := crt & wIdxMask
	return uint8(f), uint8(w), uint16(idx)
}

// convert to string, usefull for debugging
func (t tInfo) String() string {
	f, w, i := t.getAll()
	return fmt.Sprintf("%02x:%02x:%d", f, w, i)
}
