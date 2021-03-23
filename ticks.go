// Copyright 2021 Intuitive Labs GmbH. All rights reserved.
//
// Use of this source code is governed by a BSD-style license
// that can be found in the LICENSE.txt file in the root of the source
// tree.

package wtimer

import (
	"strconv"
)

const (
	//TicksBits    = 48
	TicksBits    = W0Bits + W1Bits + W2Bits + W3Bits
	MaxTicksDiff = 1 << (TicksBits - 1)
	TicksMask    = (MaxTicksDiff - 1) | MaxTicksDiff
	// special value, when returned from a timer handler the timer will be
	// re-added with the initial interval (periodic)
)

// Ticks is the type used for the timer ticks.
// It represents monotonically increased timer ticks.
// It has no 0 or reference value.
// 2 Ticks can be compared as long as the difference between them is strictly
// less then MaxTicksDiff (as long as the difference between them produces no
// "sign" when represented on TicksBits bits).
//
// Operation on Ticks should be performed only using its methods
// (especially comparisons).
type Ticks struct {
	v uint64
}

// NewTicks creates a new tick value from an uint64.
func NewTicks(u uint64) Ticks {
	return Ticks{u & TicksMask}
}

// diffWrap returns true if t interpreted as a Ticks difference
// would wrap-arround.
func (t Ticks) diffWrap() bool {
	return (t.v & MaxTicksDiff) != 0 // or t >= MaxTicksDiff
}

// Val returns the ticks value as a uint64.
func (t Ticks) Val() uint64 {
	return t.v & TicksMask
}

// EQ returns if t == u, taking into account wraparound
// (eg. for a 8-bits Tikcs: 0000 0001 will be equal to 1 0000 0001).
func (t Ticks) EQ(u Ticks) bool {
	return (t.v-u.v)&TicksMask == 0
}

// EQ returns if t != u, taking into account wraparound.
func (t Ticks) NE(u Ticks) bool {
	return !t.EQ(u)
}

// LT returns if t < u.
func (t Ticks) LT(u Ticks) bool {
	return (t.v-u.v)&MaxTicksDiff != 0
}

// GT returns if t > u.
func (t Ticks) GT(u Ticks) bool {
	return !t.LT(u) && t.NE(u)
}

// GE returns if t <= u.
func (t Ticks) GE(u Ticks) bool {
	return (t.v-u.v)&MaxTicksDiff == 0
}

// LE returns if t <= u.
func (t Ticks) LE(u Ticks) bool {
	return t.LT(u) || t.EQ(u)
}

// Add  adds another ticks value and returns the result.
func (t Ticks) Add(u Ticks) Ticks {
	return Ticks{(t.v + u.v) & TicksMask}
}

// Sub subtracts another ticks value and returns the result.
func (t Ticks) Sub(u Ticks) Ticks {
	return Ticks{(t.v - u.v) & TicksMask}
}

// Add  adds an uint64 value and returns the result.
func (t Ticks) AddUint64(u uint64) Ticks {
	return Ticks{(t.v + u) & TicksMask}
}

// Sub subtracts an uint64 value and return the result.
func (t Ticks) SubUint64(u uint64) Ticks {
	return Ticks{(t.v - u) & TicksMask}
}

// String converts a tick value to a string.
func (t Ticks) String() string {
	return strconv.FormatUint(t.v, 10)
}
