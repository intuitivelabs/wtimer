// Copyright 2021 Intuitive Labs GmbH. All rights reserved.
//
// Use of this source code is governed by a BSD-style license
// that can be found in the LICENSE.txt file in the root of the source
// tree.

package wtimer

import (
	"time"
)

const Periodic time.Duration = time.Duration(^int64(0))

// A TimerHandlerF is a callback called when a timer expires.
// The parameters passed are  a pointer to the timing wheel to which it
// belongs (WTimer), the handler of the expired running timer,
// and an opaque parameter passed when the timer was registered.
// The callback should return true and a new expire delta (time.Duration) if
// the timer should be re-added and false if the timer should finish
// immediately (e.g. one shot or periodic timer that stops).
// For the re-add case there is a special value for re-arming the timer with
// the initial timeout: wtimer.Periodic.
// Note that the new expire interval will be rounded up to a multiple
// of Ticks (use wt.Duration(NewTicks(1)) too see the configured
// tick interval) and it should be always at least 1 tick.
//
// Inside the timer callback the only timer operation allowed on
// the timer handler ( *TimerLnk) is wt.Del(). All the other operations cannot
// be used inside the timer callback: wt.Add() and wt.Reset() will fail
// harmlessly, wt.DelTry() will always return false and wt.DelWait() will
// deadlock (waiting for the callback running it to finish...).
// wt.Del() is not 100% equivalent to returning false. Returning false
// means that the timer handler can be freed inside the callback, the
// timer code will not touch it. Running wt.Del() and returning
// true will still terminate the timer, but the timer code will access first
// the handler, so if you use wt.Del() inside the callback instead of returning
// false, then the timer handler must still exist after the callback ends.
type TimerHandlerF func(wt *WTimer, h *TimerLnk, arg interface{}) (bool, time.Duration)

const (
	wheelNone  uint8  = 255   // sentinel value for no wheel
	wheelExp   uint8  = 254   //  no wheel, expired list
	wheelRQ    uint8  = 253   // no wheel, runq
	wheelNoIdx uint16 = 65535 // sentinel debug value for no index
)

// flags for timers
const (
	fHead    = 1  // this is the list head (debugging)
	fActive  = 2  // timer is active (added)
	fDelete  = 4  // the timer was deleted
	fRunning = 8  // timer handler is executing
	fRemoved = 16 // timer is removed
	Ffast    = 32 // "fast" timer, run in the main timer go routine
	FgoR     = 64 //  run timer handle in its own temp. go routine
	// internal flags mask (flags for internal use only)
	fInternalMask = fHead | fActive | fDelete | fRunning | fRemoved
)

// A TimerLnk is the internal structure used for registering timers.
type TimerLnk struct {
	next   *TimerLnk
	prev   *TimerLnk
	expire Ticks // absolute expire "time" in ticks
	//deltaExp0 Ticks // initial timeout offset (duration till expire)
	info  tInfo         // internal information (wheel no, idx, flags ...)
	rctx  tInfo         // running "context" info, needed for DelWait()
	intvl time.Duration // initial expire interval in ns

	f   TimerHandlerF // callback function
	arg interface{}   // callback function parameter
}

// Detached checks if the TimerLnk entry is part of a list and returns true
// if not.
func (tl *TimerLnk) Detached() bool {
	return tl == tl.next || (tl.next == nil && tl.prev == nil)
}

// Exp returns the set expire "time" in ticks (debugging use)
func (tl *TimerLnk) Exp() Ticks {
	return tl.expire
}

// Intvl returns the original expire interval in ns.
func (tl *TimerLnk) Intvl() time.Duration {
	return tl.intvl
}
