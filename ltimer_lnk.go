// Copyright 2026 Frafos GmbH. All rights reserved.
//
// Use of this source code is governed by a BSD-style license
// that can be found in the LICENSE.txt file in the root of the source
// tree.

package wtimer

import "time"

// LTimerHandlerF is a callback called when a local timer expires.
// The callback should return true and a new expire delta (time.Duration) if
// the timer should be re-added and false if the timer should finish.
// For re-arming with the initial timeout use wtimer.Periodic.
type LTimerHandlerF func(lt *LTimer, tl *LTimerLnk,
	arg interface{}) (bool, time.Duration)

// LTimerLnk is the timer entry for use with LTimer.
// It is a lightweight version of TimerLnk with no atomic operations.
type LTimerLnk struct {
	next   *LTimerLnk
	prev   *LTimerLnk
	expire Ticks         // absolute expire "time" in ticks
	info   lInfo         // wheel no, idx, flags (non-atomic)
	intvl  time.Duration // initial expire interval in ns

	f   LTimerHandlerF // callback function
	arg interface{}    // callback function parameter
}

// Detached checks if the LTimerLnk entry is part of a list.
func (tl *LTimerLnk) Detached() bool {
	return tl == nil || (tl.next == nil && tl.prev == nil) ||
		(tl.next != nil && tl.next == tl)
}

// Exp returns the set expire "time" in ticks.
func (tl *LTimerLnk) Exp() Ticks {
	return tl.expire
}

// Intvl returns the original expire interval.
func (tl *LTimerLnk) Intvl() time.Duration {
	return tl.intvl
}

func lIsDetached(e *LTimerLnk) bool {
	return e.Detached()
}
