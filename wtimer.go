// Copyright 2021 Intuitive Labs GmbH. All rights reserved.
//
// Use of this source code is governed by a BSD-style license
// that can be found in the LICENSE.txt file in the root of the source
// tree.

// Package wtimer provides a high performance hierarchical timer wheel
// timers implementation, optimised for high number of timers (100k+)
// with relatively lower precision requirement.
package wtimer

import (
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/intuitivelabs/timestamp"
)

const NAME = "wtimer"

var BuildTags []string

const (
	WheelsNo = 4
	// note that the sums of all wheel bits must be equal with TicksBits
	// also no wheel can have more the 2^15 entries (max. 15 bits)
	W0Bits = 14
	W1Bits = 14
	W2Bits = 10
	W3Bits = 10
	/* testing values:
	W0Bits = 5
	W1Bits = 5
	W2Bits = 5
	W3Bits = 5
	*/

	W0Entries = 1 << W0Bits
	W1Entries = 1 << W1Bits
	W2Entries = 1 << W2Bits
	W3Entries = 1 << W3Bits

	W0Mask = (1 << W0Bits) - 1
	W1Mask = (1 << W1Bits) - 1
	W2Mask = (1 << W2Bits) - 1
	W3Mask = (1 << W3Bits) - 1

	wTotalEntries = W0Entries + W1Entries + W2Entries + W3Entries
)

// wheel sizes array
var wheelEntries = [WheelsNo]uint16{
	W0Entries,
	W1Entries,
	W2Entries,
	W3Entries,
}

// return t corresp. pos in wheel 0
func wheel0Pos(t uint64) uint64 {
	return t & W0Mask
}

// return t corresp. pos in wheel 1
func wheel1Pos(t uint64) uint64 {
	return (t >> W0Bits) & W1Mask
}

// return t corresp. pos in wheel 2
func wheel2Pos(t uint64) uint64 {
	return (t >> (W0Bits + W1Bits)) & W2Mask
}

// return t corresp. pos in wheel 3
func wheel3Pos(t uint64) uint64 {
	return (t >> (W0Bits + W1Bits + W2Bits)) & W3Mask
}

// return the wheel number and the index inside the wheel corresponding to
// a timer expiring at exp. now is the current time in Ticks,
// If exp == now returns wheelExp, wheelNoIdx
func getWheelPos(exp, now Ticks) (uint8, uint16) {
	delta := exp.Sub(now).Val()
	expire := exp.Val()
	switch {
	case delta < W0Entries:
		if delta == 0 {
			// already expired
			return wheelExp, wheelNoIdx
		}
		return 0, uint16(wheel0Pos(expire))
	case delta < W0Entries*W1Entries:
		return 1, uint16(wheel1Pos(expire))
	case delta < W0Entries*W1Entries*W2Entries:
		return 2, uint16(wheel2Pos(expire))
	}
	return 3, uint16(wheel3Pos(expire))
}

type wheel struct {
	no   uint8
	lsts []timerLst
}

func (w *wheel) init(n uint8, lists []timerLst) {
	w.no = n
	w.lsts = lists
	for i := 0; i < len(w.lsts); i++ {
		w.lsts[i].init(w.no, uint16(i))
	}
}

const (
	runQueuesNo        = 8 // run queues used to avoid lock contention
	runQueuesWorkersNo = 8 // workers for the runQueues
)

// WTimer implements a hierarchical timer wheel.
type WTimer struct {
	opLock sync.Mutex // operations lock
	wheels [WheelsNo]wheel
	wlists [wTotalEntries]timerLst // each wheel gets its own slice of wlists

	expired timerLst

	// ready to run entries are distributed in run queues
	// runq pos (idx) for consuming, atomic access, always ++ & <=rQhead
	// (each runq "worker" will consume from rQs[(qQtail++)%runQueusNo])
	rQtail uint32
	// runq pos  for producing, atomic access, always increasing
	rQhead  uint32
	rQs     [runQueuesNo]timerLst   // run queues
	rQlocks [runQueuesNo]sync.Mutex // extra lock for the expired list
	// channel for signaling runq workers, msg: queue index with messages
	rQch chan struct{}

	running   *TimerLnk              // current running handler in "main"
	rQrunning [runQueuesNo]*TimerLnk // current running handler

	tickDuration time.Duration
	nowTicks     uint64 // current ticks as uint64 (atomic access)

	lastTickT timestamp.TS // last time we updated the ticks
	badTime   uint32       // count time going backwards
	refTS     timestamp.TS // reference time stamp (for refTicks)
	refTicks  Ticks        // reference ticks value at start-up or re-adj.

	wg     sync.WaitGroup // wait group for all the go routines started
	cancel chan struct{}  // used to stop all go routines
}

// Init initializes the timer wheel, with td as tick duration.
// Note that tick durations that are too low would cause high cpu usage
// when idle (too many wakeups).
// Example tick values  and cpu usage when idle:
// (note that cpu frequency scaling was enabled albeit perf. governor)
//                   100ms =>       0 %cpu idle
//                    10ms =>       1-1.5% cpu idle
//                     1ms =>       6-9%  cpu
//                     0.5ms =>    11% cpu
//                     0.25ms =>    7-8%   cpu
//                     0.125ms =>  12-14% cpu
//                     0.062ms =>  20-22% cpu
//                     0.031ms =>  23-24% cpu
//                     0.015ms =>  25-26% cpu
//                     0.007ms =>  32-34% cpu
//                     0.003ms =>  65-70% cpu !!!
//                     0.001ms => 133-135% cpu !!!
//
// Under load there doesn't seem  to be too much variance on performance
// (e..g 100k active timers mostly with 1s to 32s expire, decreasing tick
//  numbers only minimally influences the total cpu usage).
func (wt *WTimer) Init(td time.Duration) error {
	if td < (time.Microsecond) {
		return errors.New("wtimer.Init: tick duration too small")
	} else if td > (time.Hour * 24) {
		// probably an error
		return errors.New("wtimer.Init: tick duration too high")
	}
	wt.tickDuration = td

	for i, pos := 0, 0; i < len(wt.wheels); i++ {
		sz := int(wheelEntries[i])
		wt.wheels[i].init(uint8(i), wt.wlists[pos:pos+sz])
		pos += sz
	}
	wt.expired.init(wheelExp, wheelNoIdx)
	for i := 0; i < len(wt.rQs); i++ {
		wt.rQs[i].init(wheelRQ, uint16(i))
	}
	wt.rQch = make(chan struct{}, runQueuesWorkersNo*4)
	return nil
}

// Now returns the current wt time in ticks.
func (wt *WTimer) Now() Ticks {
	crtTicks := atomic.LoadUint64(&wt.nowTicks)
	return NewTicks(crtTicks)
}

// internal crt. ticks++
func (wt *WTimer) incTime() {
	atomic.AddUint64(&wt.nowTicks, 1)
}

// Ticks returns the duration d converted to Ticks (round-down) and
// the rest (if the passed duration is not an integer number of ticks).
func (wt *WTimer) Ticks(d time.Duration) (Ticks, time.Duration) {
	if wt.tickDuration != 0 {
		t := d / wt.tickDuration
		return NewTicks(uint64(t)), d % wt.tickDuration
	}
	return NewTicks(0), d
}

// Duration converts a tick number to a time.Duration
// (according to the WTimer tick length)
func (wt *WTimer) Duration(t Ticks) time.Duration {
	return time.Duration(t.Val()) * wt.tickDuration
}

// TicksRoundUp converts a duration into ticks number rounding-up
// if the duration is less then 1 tick or if duration >= 0.5 ticks.
// This is also the way durations are converted to ticks internally.
func (wt *WTimer) TicksRoundUp(d time.Duration) Ticks {
	dticks, rest := wt.Ticks(d)
	if dticks.Val() == 0 || rest >= 50*wt.tickDuration/100 {
		// round-up if smaller then 1 tick or if value between ticks
		return dticks.AddUint64(1)
	}
	return dticks
}

// InitTimer() inits a TimerLnk handle before use.
// For the possible flags values, see Reset().
// Note: never call it on a running timer, only on new ones.
func (wt *WTimer) InitTimer(tl *TimerLnk, flags uint8) error {
	*tl = TimerLnk{}
	tl.info.setWheel(wheelNone, wheelNoIdx)
	return wt.Reset(tl, flags)
}

// NewTimer() allocates and returns a new  initialised timer handler
// (TimerLnk).
// For the possible flag values, see reset.
// Note that the high performance way of using the timers involves
// making a TimerLnk part of your data structure and then using InitTimer()
// on it and not using TimerLnk pointers created by NewTimer(), since this
// would involve an additional allocation and more GC work.
func (wt *WTimer) NewTimer(flags uint8) *TimerLnk {
	tl := &TimerLnk{}
	if wt.InitTimer(tl, flags) != nil {
		return nil
	}
	return tl
}

// Reset will prepare a timer for re-use or set flags on a new timer.
// Supported flags:
//   * Ffast - fast timer handler that will be executed in
//             the timer context (use with care, delays will impact all
//             the timers, the handler should execute really fast and
//             not block under any circumstance).
//   * FgoR   - run the timer handler in a new go routine (experimental,
//             useful if the handler does lot of work or some potentially
//             blocking operation). FgoR timers cannot be DelWait()-ed.
//
// Do not use on timers that were not deleted, or on timer that finished
// (returned false from the handler). A finished timer must be re-initialised.
func (wt *WTimer) Reset(tl *TimerLnk, flags uint8) error {
	f := tl.info.flags()
	if f&fActive != 0 && f&fRemoved == 0 {
		// active and not removed
		return ErrActiveTimer
	}
	if tl.next != nil || tl.prev != nil {
		return ErrInvalidTimer
	}
	// make sure the caller does not set our internal flags
	flags &= ^uint8(fInternalMask)
	tl.info.chgFlags(flags, fInternalMask)
	return nil
}

func (wt *WTimer) lock() {
	wt.opLock.Lock()
}

func (wt *WTimer) unlock() {
	wt.opLock.Unlock()
}

// appendTimer adds an _empty_ timer link to the specified wheel & idx.
// NOTE: the timer link must be detached (not part of any list).
// returns nil on success and an error  for bugs or invalid params.
func (wt *WTimer) appendTimer(tl *TimerLnk, wheel uint8, idx uint16) error {
	if wheel < WheelsNo {
		wt.wheels[wheel].lsts[idx].append(tl)
	} else if wheel == wheelExp {
		wt.expired.append(tl)
	} else {
		BUG("invalid wheel no: %d idx %d for %p\n",
			wheel, idx, tl)
		return ErrInvalidTimer
	}
	return nil
}

// addUnsafe assumes that the proper locks are held and adds a new timer.
// returns nil on success, or an error (bad params/expire...)
func (wt *WTimer) addUnsafe(tl *TimerLnk, now Ticks) error {
	//	delta := tl.deltaExp0
	delta, _ := wt.Ticks(tl.intvl)
	if delta.Val() > MaxTicksDiff {
		BUG("delta value is too high: %d ticks (%s) > max %d\n",
			delta.Val(), tl.intvl, MaxTicksDiff)
		return ErrTicksTooHigh
	}
	// adjust expire in ticks since we might be called between ticks
	// increases and the ticks might increase with more then one.
	// Small ticks (duration) combined with scheduling latencies might
	// cause the ticks numbers (wt.Now())to advance very quickly.
	// For example latencies of ~20ms with a 1ms time.Ticker are not
	// uncommon and if Add is called at the beginning of such a "pause"
	// period it would use a tick value that in the next 1ms might increase
	// with 20 instead of 1 => the timer will expire 20ms too early.
	// It's better to compute the expire in ticks using the "start"
	// time and ticks value, thus latencies would only delay timers that were
	// supposed to execute during the latency interval, but avoid
	// executing any timer too early.
	expIntvl := timestamp.Now().Sub(wt.refTS) + tl.intvl
	// round-up if 0 expire or if expire in-between ticks
	// (round-up almost always, better to expire 1 tick later then
	//   1 tick too soon)
	// A timer returning 0 expire would never leave the expired list,
	// being continuously executed (alternative: add another run list
	// and another mv between expired and run and exec only from run).
	dticks := wt.TicksRoundUp(expIntvl)
	if dticks.Val() > (MaxTicksDiff - 1) {
		BUG("adjusted delta value is too high: %d ticks > max %d\n",
			dticks.Val(), MaxTicksDiff)
		return ErrTicksTooHigh
	}
	tl.expire = wt.refTicks.Add(dticks)
	//tl.expire = now.Add(delta)
	w, idx := getWheelPos(tl.expire, now)
	if w == wheelExp && DBGon() {
		DBG("timer added with 0 expire: %p delta %d, now %d (ticks)\n",
			tl, delta, tl.expire)
	}

	return wt.appendTimer(tl, w, idx)
}

// addSanityChecks performed sanity checks for parameters of Add*() functions.
// Can be called with unlocked wt, but then the values might change.
func (wt *WTimer) addSanityChecks(tl *TimerLnk, delta time.Duration,
	f TimerHandlerF) error {
	if tl.info.flags()&fActive != 0 {
		if DBGon() {
			f, w, idx := tl.info.getAll()
			DBG("called on active timer %p 0x%0x wheel: %d/%d "+
				" n: %p p: %p\n",
				tl, f, w, idx, tl.next, tl.prev)
		}
		return ErrActiveTimer
	}
	if tl.info.flags()&fRunning != 0 {
		if DBGon() {
			DBG("Add* called on running timer: flags 0x%x \n", tl.info.flags())
		}
		return ErrNotResetTimer
	}
	if tl.info.flags()&fRemoved != 0 {
		if DBGon() {
			f, w, idx := tl.info.getAll()
			DBG("Add* fRemoved set: flags 0x%x   w/idx %d/%d"+
				" n: %p p: %p\n", f, w, idx, tl.next, tl.prev)
		}
		return ErrNotResetTimer
	}

	if tl.next != nil || tl.prev != nil {
		f, w, idx := tl.info.getAll()
		BUG("called with linked timer: %p flags 0x%x on w/idx %d/%d"+
			" n: %p p: %p\n",
			tl, f, w, idx, tl.next, tl.prev)
		return ErrInvalidTimer
	}
	w, idx := tl.info.wheelPos()
	if w != wheelNone || idx != wheelNoIdx {
		BUG("called non-init or bad timer: %p flags 0x%x on w/idx %d/%d"+
			" n: %p p: %p\n",
			tl, tl.info.flags(), w, idx, tl.next, tl.prev)
		return ErrInvalidTimer
	}
	if f == nil {
		ERR("called with 0 callback\n")
		return ErrInvalidParameters
	}
	return nil
}

// Add starts a new timer that will run f(tl, ticks, p) after the specified
// time.Duration.
// It returns whether the operation was successful (nil) or an error.
// tl is a pointer to a TimerLnk structure which should be either provided
//  or obtained from NewTimer()).
func (wt *WTimer) Add(tl *TimerLnk, d time.Duration,
	f TimerHandlerF, p interface{}) error {
	// extra sanity: could be skipped
	ticks, _ := wt.Ticks(d)
	if ticks.Val() == 0 {
		if DBGon() {
			DBG("Add() called with 0 timeout\n")
		}
		// return ErrDurationTooSmall
	}

	wt.lock()
	if err := wt.addSanityChecks(tl, d, f); err != nil {
		wt.unlock()
		return err
	}
	tl.f = f
	tl.arg = p
	tl.intvl = d

	// set fActive and clear the rest of the internal flags
	tl.info.chgFlags(fActive, fInternalMask)
	ret := wt.addUnsafe(tl, wt.Now())
	if ret != nil {
		tl.info.setFlags(fRemoved)
	}

	wt.unlock()

	return ret
}

// AddT starts a new timer that will run f(tl, ticks, p) after delta ticks.
// It returns whether the operation was successful (nil) or an error.
// tl is a pointer to TimerLnk structure which should be either provided
//  or obtained from NewTimer().
func (wt *WTimer) AddT(tl *TimerLnk, delta Ticks,
	f TimerHandlerF, p interface{}) error {
	intvl := wt.Duration(delta)
	return wt.Add(tl, intvl, f, p)
}

// AddExpire starts a new timer that will run f(tl, ticks, p) exactly at the
// specified expire value (the expire value is an absolute value and not
// relative to the current time in ticks).
// It will not try to do any adjustment to the expire.
// It returns whether the operation was successful (nil) or an error.
// tl is a pointer to TimerLnk structure which should be either provided
//  or obtained from NewTimer().
func (wt *WTimer) AddExpire(tl *TimerLnk, expire Ticks,
	f TimerHandlerF, p interface{}) error {

	now := wt.Now()
	intvl := wt.Duration(expire.Sub(now))

	wt.lock()
	if err := wt.addSanityChecks(tl, intvl, f); err != nil {
		wt.unlock()
		return err
	}
	tl.f = f
	tl.arg = p
	tl.intvl = intvl
	tl.expire = expire

	// set fActive and clear the rest of the internal flags
	tl.info.chgFlags(fActive, fInternalMask)

	w, idx := getWheelPos(tl.expire, now)
	if w == wheelExp && DBGon() {
		DBG("timer added with 0 expire: %p delta %d, now %s (ticks)\n",
			tl, intvl, tl.expire)
	}

	ret := wt.appendTimer(tl, w, idx)
	wt.unlock()
	return ret
}

// del internal flags
type delFlags uint8

const (
	fDelInactiveOk delFlags = 1 << iota
	fDelAlreadyOk
	fDelRaceOk
	fDelForce
	fDelTry // try only, if running abort (don't mark for delete)
)

// del will try to remove the corresponding timer.
// On success it returns true, nil. If the timer is running (and cannot be
// removed) it returns false, nil. If there was an error encountered it
// will return true or false and the error (true meaning don't retry).
// To force a delete, waiting for the running timer to terminate (if running)
// use DelWait().
func (wt *WTimer) del(tl *TimerLnk, delF delFlags) (bool, error) {

retry:
	wt.lock()

	// both flags & wheel should be read in the same time
	//  (they can change if wt.lock() is held, e.g. from rQ)
	flags, wheel, idx := tl.info.getAll()
	if flags&(fActive|fDelete) != fActive {
		// if fActive not set or fDelete set
		if flags&fActive == 0 {
			// not active anymore => was re-init or never added
			wt.unlock()
			if DBGon() {
				DBG("called on inactive/un-init timer: %p (n: %p, p: %p)"+
					" flags 0x%x\n",
					tl, tl.next, tl.prev, tl.info.flags())
			}
			return true, ErrInactiveTimer
		}
		// here the timer is active but marked for delete (fDelete)
		// check if not in Race or Force mode
		if delF&(fDelRaceOk|fDelForce) == 0 {
			// not in Race ok or force mode => exit on delete in progress
			wt.unlock()
			if delF&fDelAlreadyOk != 0 {
				// timer marked for delete -> return current delete strategy
				return (flags&fRemoved != 0), nil
			}
			if DBGon() {
				DBG("called on timer already delete marked: %p (n: %p, p: %p)"+
					" flags 0x%x\n",
					tl, tl.next, tl.prev, tl.info.flags())
			}
			// timer marked for delete -> return current delete statey
			return (flags&fRemoved != 0), ErrDeletedTimer
		}
	}
	// a running timer has: fRunning & wheel == wheelNone
	// a removed timer has: fRemoved & wheel == wheelNone
	// wheel can change in parallel to  wt.lock() (under wt.rQlock[...])
	// only from wheelRQ to wheelNone
	// (if wheelNone there might be parallel runq code updating the flags
	// in the same time, but always fRunning first, before setting the wheel)
	//
	// The wheel must be checked under wt.unlock() otherwise there would be an
	//  window between removing from expire lists and adding to a runq
	// where wheel == wheelNone.
	if wheel == wheelNone {
		// check for fRunning, but don't use the cached value
		// (it might have changed in parallel with wt.lock() see above)
		if tl.info.flags()&fRunning != 0 {
			// running cannot be deleted, no error
			if delF&fDelTry == 0 {
				tl.info.setFlags(fDelete)
			}
			wt.unlock()
			return false, nil
		}
		// here fRunning is not set and wheel is wheelNone => there
		// is no parallel runq code using t (it would have set first
		// fRunning and then transition from wheelRQ to wheelNone)
		wt.unlock()
		// already removed
		if (delF&(fDelRaceOk|fDelForce) == 0) && WARNon() {
			WARN("called on already removed timer: %p (n: %p, p: %p),"+
				" flags 0x%x wheel %d/%d\n",
				tl, tl.next, tl.prev, tl.info.flags(), wheel, idx)
		}
		// BUG check also for fRemoved set (even in the rQ case
		//     fRunning is reset and fRemoved set under wt.lock() so
		//     wheelNone && !fRunning && !fRemoved is invalid.
		// TODO: if 2 delete run in parallel and on rQ it would be
		//       possible: 1st delete set wheel to none holding rQ[i].Lock,
		//       2nd delete reaches this check with wt.lock() held and
		//       fails (1st delete did not set yet fRemoved) so it might be
		//       a valid not BUG case.
		if flags&fRemoved == 0 {
			BUG("timer removed but fRemoved not set: %p (n: %p, p: %p),"+
				" flags 0x%x wheel %d/%d\n",
				tl, tl.next, tl.prev, tl.info.flags(), wheel, idx)
		}
		return true, ErrAlreadyRemovedTimer
	}
	// BUG check for fRemoved not set?
	// TODO: change from PANIC to BUG
	if flags&fRemoved != 0 {
		w, i := tl.info.wheelPos()
		PANIC("timer fRemoved  SET but on wheel: %p (n: %p, p: %p),"+
			" crt flags 0x%x wheel %d/%d orig 0x%x wheel %d/%d\n",
			tl, tl.next, tl.prev, tl.info.flags(), w, i, flags, wheel, idx)
	}

	// BUG checks: if wheel != wheelNone then it should not be detached,
	//  unless wheel was wheelRQ and it did become wheelNone in parallel.
	if wheel != wheelRQ &&
		(tl.Detached() || tl.next == nil || tl.prev == nil) {
		wt.unlock()
		PANIC("invalid timer link: %p: n: %p p: %p on wheel %d/%d expire %d\n",
			tl, tl.next, tl.prev, wheel, idx, tl.expire)
		return true, ErrInvalidTimer
	}

	if wheel < WheelsNo {
		// easy case, not on the expire lists or runq => not running
		lst := &wt.wheels[wheel].lsts[idx]
		lst.rm(tl)
		tl.next = nil // DBG
		tl.prev = nil // DBG
		tl.info.setFlags(fRemoved)
		wt.unlock()
		return true, nil
	} else if wheel == wheelExp {
		var ret bool
		lst := &wt.expired
		// might be running
		if tl.info.flags()&fRunning == 0 {
			// not running => easy remove
			lst.rm(tl)
			tl.next = nil // DBG
			tl.prev = nil // DBG
			tl.info.setFlags(fRemoved)
			ret = true
		} else {
			// if wheel == wheelExp, the wheel & flags change are always done
			// under wt.lock() so flags should never be fRunning here
			// (since fRunning implies wheel == wheelNone or in the race
			// case wheel == wheelRQ)
			// TODO: change to BUG
			w, i := tl.info.wheelPos()
			PANIC("timer on wheelExp but fRunning was set: %p (n: %p, p: %p),"+
				" flags 0x%x (crt 0x%x) wheel %d/%d (crt %d/%d)\n",
				tl, tl.next, tl.prev, flags, tl.info.flags(),
				wheel, idx, w, i)
			// running
			// mark it so it's not re-added (e.g. running periodic)
			if delF&fDelTry == 0 {
				tl.info.setFlags(fDelete)
			}
			ret = false
		}
		wt.unlock()
		return ret, nil
	} else if wheel == wheelRQ {
		// on the delayed runq => protected by wt.rqLocks[idx]
		wt.unlock()            // unlock main wheels
		wt.rQlocks[idx].Lock() // lock target runq
		// check if anything changed
		wheel2, idx2 := tl.info.wheelPos()
		if wheel != wheel2 || idx != idx2 {
			// changed => retry
			wt.rQlocks[idx].Unlock()
			goto retry // main lock already unlocked here
		} else {
			var ret bool
			// not changed, ok try to remove
			if tl.info.flags()&fRunning == 0 {
				// not running => remove
				lst := &wt.rQs[idx]
				lst.rm(tl)
				tl.next = nil // DBG
				tl.prev = nil // DBG
				tl.info.setFlags(fRemoved)
				ret = true
			} else { // running
				// handle race with runq: if the timer is on wheelRQ it
				// might set wheel to wheelNone and fRunning in parallel
				// with code running under wt.lock() (so even if we checked
				// the flags and wheel at the beginning they might have
				// changed by now).

				// mark it so it's not re-added (e.g. running periodic)
				if delF&fDelTry == 0 {
					tl.info.setFlags(fDelete)
				}
				ret = false
			}
			wt.rQlocks[idx].Unlock()
			return ret, nil // main lock already unlocked here
		}
	}
	wt.unlock()
	// TODO: change to BUG
	PANIC(" unknown wheel for %p (n: %p, p: %p),"+
		" flags 0x%x wheel %d/%d\n",
		tl, tl.next, tl.prev, tl.info.flags(), wheel, idx)
	return true, ErrInvalidTimer
}

// Del will remove the corresponding timer either immediately or, if
// running, when the timer handler terminates.
// On success if the timer was removed it returns true, nil.
// If the timer is running (and cannot be removed yet) it returns false, nil.
// If there was an error encountered it will return true or false and the
// error (true meaning don't retry, the timer is removed).
//
// Running timers will be marked for removal the moment their handles
// terminate (return), ignoring any possible re-arm request
// (return true, interval from the handler).
// To only delete a timer if it's not running at the moment (allowing it to
// re-arm itself if it is running), use DelTry(),
//
// To  waiting for a  running timer to terminate (if running) and then delete
// it, use DelWait().
//
// Multiple Del*()s can be safely run on the same timer.
func (wt *WTimer) Del(tl *TimerLnk) (bool, error) {
	return wt.del(tl, 0)
}

// DelTry will try to remove the corresponding timer, but it will do nothing
// if the timer is running (it will allow it to re-arm itself).
// It returns true on success (timer removed) and false if the timer is
// running, along with a possible error.
func (wt *WTimer) DelTry(tl *TimerLnk) (bool, error) {
	return wt.del(tl, fDelTry)
}

// DelWait will remove the corresponding timer, waiting for it if already
// running (busy wait, use with care).
// NOTE: experimental.
// It returns true if the timer was removed, false if not (timers that are
// configured to execute in their own temporary goroutine, using the FgoR
// flag, cannot be safely removed if running). In both cases it
// might return an error (ErrInvalidTimer or ErrInactiveTimer).
func (wt *WTimer) DelWait(tl *TimerLnk) (bool, error) {
	var ok bool
	var err error
	for {
		ok, err = wt.del(tl, fDelRaceOk)
		if !ok && err == nil {
			// del failed, check if running
			// if it's still marked as running it might have actually
			// been remove by returning false in callback, in which
			// case the timer handler is not touched anymore => check if
			// is a normal or fast timer and in this case check if it's
			// running right now. FgoR timers cannot be checked.
			flags := tl.info.flags()
			wheel, idx := tl.rctx.wheelPos() // running
			if flags&FgoR != 0 {
				return false, nil
			}
			if flags&fRunning == fRunning {
				// get the right lock
				if wheel == wheelExp {
					wt.lock()
					flags2 := tl.info.flags()
					wheel2, idx2 := tl.rctx.wheelPos()
					if wheel == wheel2 && idx == idx2 {
						// it's ok we locked the right list
						if wt.running != tl && (flags2&fRunning != 0) {
							// marked as running, but not really running
							// => self removed by callback false return
							wt.unlock()
							tl.info.setFlags(fRemoved)
							return true, nil
						}
						// else fallthrough retry
					}
					wt.unlock()
					// running now or moved to other wheel, retry (fallthrough)
				} else if wheel == wheelRQ {
					wt.rQlocks[idx].Lock()
					flags2 := tl.info.flags()
					wheel2, idx2 := tl.rctx.wheelPos()
					if wheel == wheel2 && idx == idx2 {
						if wt.rQrunning[idx] != tl && (flags2&fRunning != 0) {
							// not running on the advertised rq,
							// but marked as running
							wt.rQlocks[idx].Unlock()
							tl.info.setFlags(fRemoved)
							return true, nil
						}
						// else fallthrough retry
					}
					wt.rQlocks[idx].Unlock()
					// fallthrough to Gosched()
				}
				// spinning...
				runtime.Gosched()
			}
		} else {
			if ok &&
				( /*err == ErrInactiveTimer ||*/ err == ErrAlreadyRemovedTimer) {
				err = nil
			}
			break
		}
	}
	return ok, err
}

// redistTimer will move tl to a new list/wheel according to
// tl.expire and the current time (specified by now).
// lst is the list that currently owns tl.
func (wt *WTimer) redistTimer(lst *timerLst, tl *TimerLnk, now Ticks) {
	expire := tl.expire
	if expire.LT(now) {
		BUG("rtimer %p on wheel/idx: %d/%d: expire less then \"now\":"+
			" expire %d now %d (ticks), lst %p \n",
			tl, lst.wheelNo, lst.wheelIdx, expire.Val(), now.Val(), lst)
		// try to fix it to expire immediately
		expire = now
	}
	w, idx := getWheelPos(expire, now)

	// allow explicitly extending the timeout by extending expire
	// (so no BUG checks for wheel >= old wheel), but check for
	// wheel & lst being the same
	if w == lst.wheelNo && idx == lst.wheelIdx {
		BUG("redistributed to the same wheel/idx: %d/%d -> %d/%d"+
			" expire %d now %d (ticks), lst %p tl %p\n",
			lst.wheelNo, lst.wheelIdx, w, idx, tl.expire.Val(), now.Val(),
			lst, tl)
		return // nothing to do
	}

	lst.rm(tl)
	if wt.appendTimer(tl, w, idx) != nil {
		if ERRon() {
			ERR("append timer failed for tl %p on %d/%d redist to %d/%d"+
				" expire %d now %d (tick), lst %p\n",
				tl, lst.wheelNo, lst.wheelIdx, w, idx,
				tl.expire.Val(), now.Val(), lst)
		}
		tl.next = nil
		tl.prev = nil
		tl.info.setFlags(fRemoved)
	}
}

// redistLst empties lst and redistributes all its entries according
// to their expire timeout and "now". now represent the current in ticks.
func (wt *WTimer) redistLst(lst *timerLst, now Ticks) {
	s := lst.head.next
	// del current element safe iteration
	for v, nxt := s, s.next; v != &lst.head; v, nxt = nxt, nxt.next {
		wt.redistTimer(lst, v, now)
	}
	if !lst.isEmpty() {
		BUG("lst on wheel %d idx %d (%p) not empty after redistTimer"+
			" @%d ticks\n", lst.wheelNo, lst.wheelIdx, lst, now)
	}
}

// redistTimers will cause all the timers to be moved to lists according
// to their expire relative to the current time (passed as the "now" parameter).
func (wt *WTimer) redistTimers(now Ticks) {
	t := now.Val()
	idx0 := wheel0Pos(t) // corresp. pos in wheel 0
	if idx0 == 0 {       // multiple of W0Entries => look first in wheel 1
		idx1 := wheel1Pos(t)
		if idx1 == 0 {
			// multiple of W1Entries*W0Entries  => look first in wheel 2
			idx2 := wheel2Pos(t)
			if idx2 == 0 {
				// multiple of W2Entries * W1Entries * W0Entries =>
				// time to change pos in wheel 3 => redist current pos
				idx3 := wheel3Pos(t)
				wt.redistLst(&wt.wheels[3].lsts[idx3], now)
			}
			wt.redistLst(&wt.wheels[2].lsts[idx2], now)
		}
		wt.redistLst(&wt.wheels[1].lsts[idx1], now)
	}
	// wheel 0 always runs on each new tick
	wt.wheels[0].lsts[idx0].mv(&wt.expired)
}

// handle callback return (re-add if rearm is true, ignore otherwise).
// WARNING: it should be called with wt.lock() (oplock) held
func (wt *WTimer) afterRunUnsafe(t *TimerLnk,
	rearm bool, delta time.Duration) bool {
	if rearm && (t.info.flags()&fDelete == 0) {
		t.info.resetFlags(fRunning)
		// re-add
		if delta != Periodic {
			t.intvl = delta
			//t.deltaExp0, _ = wt.Ticks(ret)
			/* should be handled  now (round-up) in addUnsafe()
			if delta < wt.tickDuration {
				// re-add too small -> force at least 1 tick to avoid
				// running continuously on expire
				// TODO: counter or DBG() msg?
				t.intvl = wt.tickDuration
				//t.deltaExp0 = NewTick
			}
			*/
		}
		if wt.addUnsafe(t, wt.Now()) != nil {
			// add failed (bug?)
			PANIC("addUnsafe failed\n")
			t.info.setFlags(fRemoved)
			return false
		}
		return true
	} else if rearm {
		// this means fDelete is set
		w, i := t.info.wheelPos()
		if w != wheelNone {
			PANIC("expected wheel to be none : %d/%d flags 0x%x\n", w, i, t.info.flags())
		}

		t.info.chgFlags(fRemoved, fRunning)
	} // else rearm == false => we cannot use t, it might already be destroyed
	return false
}

// processExpired will handle all the entries in the expired list.
// It must be always called under wt.opLock.
func (wt *WTimer) processExpired(now Ticks) {
	lst := &wt.expired
	rQadded := 0 // elemnts added to the rQs

	for !lst.isEmpty() {
		t := lst.head.next
		lst.rm(t)
		t.next = nil
		t.prev = nil
		flags := t.info.flags()
		if flags&Ffast != 0 {
			// fast timer -> execute it now
			wt.running = t
			t.rctx.setWheel(wheelExp, wheelNoIdx)
			t.info.setFlags(fRunning)
			wt.unlock()
			rearm, delta := t.f(wt, t, t.arg)
			// a return of rearm == false  means the timer should be removed
			// immediately: this means the timer handler might not
			// exist anymore so if rearm == false we cannot use t anymore.
			if !rearm {
				t = nil // DBG: force nil to catch bugs early
			}
			wt.lock()
			// re-add if requested
			wt.afterRunUnsafe(t, rearm, delta)
			wt.running = nil // always after resetting fRunning
			// it might have modified the expired list => restart
			// (keep the lock)
			continue
		} else if flags&FgoR != 0 {
			// run in separate go routine, experimental
			// no mark as running possibility..
			t.info.setFlags(fRunning)
			t.rctx.setWheel(wheelNone, wheelNoIdx)
			wt.unlock()
			wt.wg.Add(1)
			go func() {
				defer wt.wg.Done()
				rearm, delta := t.f(wt, t, t.arg)
				// a return of rearm == false  means the timer should be
				// removed/ immediately: this means the timer handler
				// might not exist anymore so if rearm == false we
				// cannot use t anymore.
				if !rearm {
					t = nil // DBG: force nil to catch bugs early
				}
				wt.lock()
				wt.afterRunUnsafe(t, rearm, delta)
				wt.unlock()
			}()
			wt.lock()
			// while not locked, someone might have modified the expired
			// list => restart
			continue
		} else {
			// slow timer -> add to runq
			rqPos := atomic.LoadUint32(&wt.rQhead)
			idx := rqPos % runQueuesNo
			wt.rQlocks[idx].Lock()
			wt.rQs[idx].append(t)
			wt.rQlocks[idx].Unlock()
			atomic.CompareAndSwapUint32(&wt.rQhead, rqPos, rqPos+1)
			// it should never fail since it's modified only under wt.Lock()
			// but even if the code changes it would still be ok: if the
			// swap fails it means rqHead changed under us => don't update it
			// (some parallel running future code added to the same rQ which
			// is not problematic, only slightly inefficient)
			rQadded++
		}
	}
	if rQadded != 0 {
		// something was added to the runqueues => signal the runq workers
		wt.unlock()
		sigsNo := rQadded
		if sigsNo > runQueuesWorkersNo {
			sigsNo = runQueuesWorkersNo
		}
	runq_signal:
		// signal the runq workers, but don't send more signals then workers
		for i := 0; i < sigsNo; i++ {
			select {
			case wt.rQch <- struct{}{}:
			default:
				/*
					if DBGon() {
						DBG("all runq busy after signaling %d /%d "+
							"(total added %d, pending work %d)\n",
							i, sigsNo, rQadded,
							wt.rQhead-wt.rQtail)
					}
				*/
				break runq_signal
			}
		}
		wt.lock()
	}
}

// runqListen listens on ch for a runq number and will run all the
// timer handlers queued to the respective runq.
func (wt *WTimer) runqListen(ch <-chan struct{}) {
loop:
	for {
		select {
		case <-wt.cancel:
			break loop
		case _, ok := <-ch:
			if !ok {
				// EOF
				break loop
			}
		retry:
			// try getting a runq index to run
			for {
				pos := atomic.LoadUint32(&wt.rQtail)
				if pos == atomic.LoadUint32(&wt.rQhead) {
					// someone else stole our work, nothing to do, wait for
					// another signal
					continue loop
				}
				if !atomic.CompareAndSwapUint32(&wt.rQtail, pos, pos+1) {
					// tail changed, someone was faster => try another index
					continue retry
				}
				idx := pos % runQueuesNo
				wt.rQlocks[idx].Lock()
				lst := &wt.rQs[idx]
				for !lst.isEmpty() {
					t := lst.head.next
					// flags op needs to be atomic since: we can not wt.lock()
					// here (deadlock possible since processExpired()
					// holds wt.lock() and tries to acquire a rQLock
					// fRunning must be set before setting wheel to wheelNone
					// (in lst.rm(t) to avoid a del race.

					wt.rQrunning[idx] = t
					t.rctx.setWheel(wheelRQ, uint16(idx))
					t.info.setFlags(fRunning)

					lst.rm(t)

					t.next = nil
					t.prev = nil

					wt.rQlocks[idx].Unlock()

					rearm, delta := t.f(wt, t, t.arg)
					// a return of rearm == false  means the timer should be
					// removed/ immediately: this means the timer handler
					// might not exist anymore so if rearm == false we
					// cannot use t anymore.
					if !rearm {
						t = nil // DBG: force nil to catch bugs early
					}

					wt.rQlocks[idx].Lock()
					// if a Del() happened while running it will set the
					// fDelete flags under the rQ lock => we have to take rQLock
					// before checking the fDelete flag or otherwise we could
					// race (fDelete set after we check for it...)
					if rearm && t.info.flags()&fDelete != 0 {
						rearm = false // force no re-add
					}
					wt.rQlocks[idx].Unlock()

					wt.lock()

					wt.afterRunUnsafe(t, rearm, delta)
					wt.unlock()
					wt.rQlocks[idx].Lock()
					wt.rQrunning[idx] = nil // always after fRunning reset
				} // for lst

				wt.rQlocks[idx].Unlock()
			} // for retry a new index
		} // select
	} // for main wait on signal loop
}

// run all the timers that expire at "now"
func (wt *WTimer) run(now Ticks) {
	wt.lock()
	wt.redistTimers(now)
	wt.processExpired(now)
	wt.unlock()
}

// advance the internal time to the passed value, running all the
// timers that expire.
// It must never be called in parallel.
// TODO: public version: RunTicks(diff) for running "by hand", w/o ticker.
func (wt *WTimer) advanceTimeTo(t Ticks) {
	now := wt.Now()
	if now.GT(t) {
		BUG("advancing too many ticks: %d ticks (%s)\n",
			t.Sub(now).Val(), wt.Duration(t.Sub(now)))
	}
	for wt.Now().NE(t) {
		wt.incTime() // change to cmpIncTime(old, new...) to allow parallel use
		wt.run(wt.Now())
	}
}
