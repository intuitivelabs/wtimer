// Copyright 2026 Frafos GmbH. All rights reserved.
//
// Use of this source code is governed by a BSD-style license
// that can be found in the LICENSE.txt file in the root of the source
// tree.

package wtimer

// LTimer implements a local (single goroutine) hierarchical timer wheel.
// It uses no locking, no atomics, and starts no goroutines.
// Time must be advanced explicitly by the caller via Run(), RunT(),
// Advance(), or AdvanceT().
//
// It is a simplified version of wtimer, optimised for no locking.

import (
	"errors"
	"time"

	"github.com/intuitivelabs/timestamp"
)

// fTicksBased is an LTimer-only internal flag marking timers added via AddT.
// When set, expire is computed relative to crtTicks (pure tick arithmetic).
// When not set, expire is computed using the real clock via refTS/refTicks
// to avoid accumulated rounding drift.
const fTicksBased = 128

// LTimer implements a local (single goroutine) hierarchical timer wheel.
type LTimer struct {
	wheels       [WheelsNo]lWheel
	wlists       [wTotalEntries]lTimerLst
	expired      lTimerLst
	crtTicks     Ticks
	tickDuration time.Duration
	refTS        timestamp.TS // reference timestamp
	refTicks     Ticks        // ticks value at refTS
}

// Init initializes the local timer wheel with td as tick duration.
func (lt *LTimer) Init(td time.Duration) error {
	if td < time.Microsecond {
		return errors.New("ltimer.Init: tick duration too small")
	} else if td > time.Hour*24 {
		return errors.New("ltimer.Init: tick duration too high")
	}
	lt.tickDuration = td

	for i, pos := 0, 0; i < len(lt.wheels); i++ {
		sz := int(wheelEntries[i])
		lt.wheels[i].init(uint8(i), lt.wlists[pos:pos+sz])
		pos += sz
	}
	lt.expired.init(wheelExp, wheelNoIdx)
	lt.refTS = timestamp.Now()
	lt.refTicks = lt.crtTicks
	return nil
}

// Now returns the current LTimer time in ticks.
func (lt *LTimer) Now() Ticks {
	return lt.crtTicks
}

// Ticks returns the duration d converted to Ticks (round-down) and
// the rest (if the duration is not an integer number of ticks).
func (lt *LTimer) Ticks(d time.Duration) (Ticks, time.Duration) {
	if lt.tickDuration != 0 {
		t := d / lt.tickDuration
		return NewTicks(uint64(t)), d % lt.tickDuration
	}
	return NewTicks(0), d
}

// Duration converts a tick number to a time.Duration.
func (lt *LTimer) Duration(t Ticks) time.Duration {
	return time.Duration(t.Val()) * lt.tickDuration
}

// TicksRoundUp converts a duration into ticks rounding up
// if the duration is less than 1 tick or if duration >= 0.5 ticks.
func (lt *LTimer) TicksRoundUp(d time.Duration) Ticks {
	dticks, rest := lt.Ticks(d)
	if dticks.Val() == 0 || rest >= 50*lt.tickDuration/100 {
		return dticks.AddUint64(1)
	}
	return dticks
}

// Timestamp returns the timestamp corresponding to a ticks value.
func (lt *LTimer) Timestamp(t Ticks) timestamp.TS {
	elapsed := t.Sub(lt.refTicks)
	return lt.refTS.Add(lt.Duration(elapsed))
}

// Expire returns the timestamp corresponding to the LTimerLnk expire value.
func (lt *LTimer) Expire(ti *LTimerLnk) timestamp.TS {
	return lt.Timestamp(ti.Exp())
}

// SetRefTS sets the reference timestamp and ticks for time conversion.
// This should be called before Run() to establish the time base.
func (lt *LTimer) SetRefTS(ts timestamp.TS, ticks Ticks) {
	lt.refTS = ts
	lt.refTicks = ticks
}

// InitTimer initializes an LTimerLnk before use.
func (lt *LTimer) InitTimer(tl *LTimerLnk) {
	*tl = LTimerLnk{}
	tl.info.setWheel(wheelNone, wheelNoIdx)
}

// ResetTimer prepares a timer for re-use after it has been deleted or
// finished (returned false from the handler).
func (lt *LTimer) ResetTimer(tl *LTimerLnk) error {
	f := tl.info.flags()
	if f&fActive != 0 && f&fRemoved == 0 {
		return ErrActiveTimer
	}
	if tl.next != nil || tl.prev != nil {
		return ErrInvalidTimer
	}
	tl.info.chgFlags(0, fInternalMask)
	tl.info.setWheel(wheelNone, wheelNoIdx)
	return nil
}

// appendTimer adds an LTimerLnk to the specified wheel & idx.
func (lt *LTimer) appendTimer(tl *LTimerLnk, w uint8, idx uint16) error {
	if w < WheelsNo {
		lt.wheels[w].lsts[idx].append(tl)
	} else if w == wheelExp {
		lt.expired.append(tl)
	} else {
		BUG("ltimer: invalid wheel no: %d idx %d for %p\n", w, idx, tl)
		return ErrInvalidTimer
	}
	return nil
}

// addUnsafe adds a timer with pre-set expire and intvl fields.
// now is the current time in ticks, crtTime is the caller-provided
// current timestamp.
// If fTicksBased is set on the timer, expire is computed as now + round-up
// of intvl (pure tick arithmetic, no real clock).
// Otherwise, expire is computed using crtTime relative to refTS/refTicks
// to avoid accumulated rounding drift.
func (lt *LTimer) addUnsafe(tl *LTimerLnk, now Ticks,
	crtTime timestamp.TS) error {
	delta, _ := lt.Ticks(tl.intvl)
	if delta.Val() > MaxTicksDiff {
		BUG("ltimer: delta value is too high: %d ticks (%s) > max %d\n",
			delta.Val(), tl.intvl, uint64(MaxTicksDiff))
		return ErrTicksTooHigh
	}
	if tl.info.flags()&fTicksBased != 0 {
		// tick-based: compute expire relative to now (crtTicks)
		dticks := lt.TicksRoundUp(tl.intvl)
		if dticks.Val() > MaxTicksDiff-1 {
			BUG("ltimer: adjusted delta value is too high:"+
				" %d ticks > max %d\n",
				dticks.Val(), uint64(MaxTicksDiff))
			return ErrTicksTooHigh
		}
		tl.expire = now.Add(dticks)
	} else {
		// duration-based: use caller-provided timestamp for greater
		// precision, compute relative to start time (refTS) and then
		// convert back to ticks
		expIntvl := crtTime.Sub(lt.refTS) + tl.intvl
		dticks := lt.TicksRoundUp(expIntvl)
		if dticks.Val() > MaxTicksDiff-1 {
			BUG("ltimer: adjusted delta value is too high:"+
				" %d ticks > max %d\n",
				dticks.Val(), uint64(MaxTicksDiff))
			return ErrTicksTooHigh
		}
		tl.expire = lt.refTicks.Add(dticks)
	}
	w, idx := getWheelPos(tl.expire, lt.crtTicks)
	if w == wheelExp && DBGon() {
		DBG("ltimer: timer added with 0 expire: %p delta %d, now %d (ticks)\n",
			tl, delta, tl.expire)
	}
	return lt.appendTimer(tl, w, idx)
}

// addSanityChecks checks parameters for Add/AddT.
func (lt *LTimer) addSanityChecks(tl *LTimerLnk, d time.Duration,
	f LTimerHandlerF) error {
	if tl.info.flags()&fActive != 0 {
		if DBGon() {
			DBG("ltimer: addSanityChecks: timer already active\n")
		}
		return ErrActiveTimer
	}
	if tl.next != nil || tl.prev != nil {
		if DBGon() {
			DBG("ltimer: addSanityChecks: timer not detached\n")
		}
		return ErrInvalidTimer
	}
	if f == nil {
		return ErrInvalidParameters
	}
	return nil
}

// Add starts a new timer that will fire after the specified duration.
// crtTime is the caller-provided current timestamp, used to compute the
// expire time relative to refTS/refTicks to avoid accumulated rounding drift.
func (lt *LTimer) Add(tl *LTimerLnk, d time.Duration, crtTime timestamp.TS,
	f LTimerHandlerF, p interface{}) error {
	if err := lt.addSanityChecks(tl, d, f); err != nil {
		return err
	}
	tl.f = f
	tl.arg = p
	tl.intvl = d
	// set fActive, clear internal flags (including fTicksBased)
	tl.info.chgFlags(fActive, fInternalMask|fTicksBased)
	ret := lt.addUnsafe(tl, lt.crtTicks, crtTime)
	if ret != nil {
		tl.info.setFlags(fRemoved)
	}
	return ret
}

// AddT starts a new timer that will fire after delta ticks.
// The expire time is computed using pure tick arithmetic (no real clock).
func (lt *LTimer) AddT(tl *LTimerLnk, delta Ticks,
	f LTimerHandlerF, p interface{}) error {
	intvl := lt.Duration(delta)
	if err := lt.addSanityChecks(tl, intvl, f); err != nil {
		return err
	}
	tl.f = f
	tl.arg = p
	tl.intvl = intvl
	// set fActive | fTicksBased, clear internal flags
	tl.info.chgFlags(fActive|fTicksBased, fInternalMask)
	// crtTime unused for tick-based timers, pass zero
	ret := lt.addUnsafe(tl, lt.crtTicks, 0)
	if ret != nil {
		tl.info.setFlags(fRemoved)
	}
	return ret
}

// Del removes a timer. Returns true if the timer was successfully removed.
func (lt *LTimer) Del(tl *LTimerLnk) (bool, error) {
	flags := tl.info.flags()
	if flags&fActive == 0 {
		return false, ErrInactiveTimer
	}
	w, idx := tl.info.wheelPos()
	switch {
	case w < WheelsNo:
		lt.wheels[w].lsts[idx].rm(tl)
		tl.next = nil
		tl.prev = nil
		tl.info.chgFlags(fRemoved, fActive)
		return true, nil
	case w == wheelExp:
		lt.expired.rm(tl)
		tl.next = nil
		tl.prev = nil
		tl.info.chgFlags(fRemoved, fActive)
		return true, nil
	case w == wheelNone:
		// already removed or running
		if flags&fRemoved != 0 {
			return true, ErrAlreadyRemovedTimer
		}
		// else timer is running (called from handler):
		// mark for deletion (will be checked after callback returns)
		tl.info.setFlags(fDelete)
		return true, nil
	}
	return false, ErrInvalidTimer
}

// redistTimer moves tl to the correct wheel according to tl.expire and now.
func (lt *LTimer) redistTimer(lst *lTimerLst, tl *LTimerLnk, now Ticks) {
	expire := tl.expire
	if expire.LT(now) {
		BUG("ltimer: rtimer %p on wheel/idx: %d/%d: expire less than \"now\":"+
			" expire %d now %d (ticks), lst %p\n",
			tl, lst.wheelNo, lst.wheelIdx, expire.Val(), now.Val(), lst)
		expire = now
	}
	w, idx := getWheelPos(expire, now)

	if w == lst.wheelNo && idx == lst.wheelIdx {
		BUG("ltimer: redistributed to the same wheel/idx: %d/%d -> %d/%d"+
			" expire %d now %d (ticks), lst %p tl %p\n",
			lst.wheelNo, lst.wheelIdx, w, idx, tl.expire.Val(), now.Val(),
			lst, tl)
		return
	}

	lst.rm(tl)
	if lt.appendTimer(tl, w, idx) != nil {
		if ERRon() {
			ERR("ltimer: append timer failed for tl %p on %d/%d redist to"+
				" %d/%d expire %d now %d (tick), lst %p\n",
				tl, lst.wheelNo, lst.wheelIdx, w, idx,
				tl.expire.Val(), now.Val(), lst)
		}
		tl.next = nil
		tl.prev = nil
		tl.info.setFlags(fRemoved)
	}
}

// redistLst empties lst and redistributes all entries.
func (lt *LTimer) redistLst(lst *lTimerLst, now Ticks) {
	s := lst.head.next
	for v, nxt := s, s.next; v != &lst.head; v, nxt = nxt, nxt.next {
		lt.redistTimer(lst, v, now)
	}
	if !lst.isEmpty() {
		BUG("ltimer: lst on wheel %d idx %d (%p) not empty after redistTimer"+
			" @%d ticks\n", lst.wheelNo, lst.wheelIdx, lst, now)
	}
}

// redistTimers redistributes timers across wheels for the current tick.
func (lt *LTimer) redistTimers(now Ticks) {
	t := now.Val()
	idx0 := wheel0Pos(t)
	if idx0 == 0 {
		idx1 := wheel1Pos(t)
		if idx1 == 0 {
			idx2 := wheel2Pos(t)
			if idx2 == 0 {
				idx3 := wheel3Pos(t)
				lt.redistLst(&lt.wheels[3].lsts[idx3], now)
			}
			lt.redistLst(&lt.wheels[2].lsts[idx2], now)
		}
		lt.redistLst(&lt.wheels[1].lsts[idx1], now)
	}
	lt.wheels[0].lsts[idx0].mv(&lt.expired)
}

// afterRun handles callback return (re-add if rearm is true).
// crtTime is the caller-provided timestamp for duration-based re-arms.
func (lt *LTimer) afterRun(tl *LTimerLnk, rearm bool, delta time.Duration,
	crtTime timestamp.TS) bool {
	if rearm && (tl.info.flags()&fDelete == 0) {
		tl.info.resetFlags(fRunning)
		if delta != Periodic {
			tl.intvl = delta
		}
		if lt.addUnsafe(tl, lt.crtTicks, crtTime) != nil {
			PANIC("ltimer: addUnsafe failed\n")
			tl.info.setFlags(fRemoved)
			return false
		}
		return true
	} else if rearm {
		// fDelete is set
		tl.info.chgFlags(fRemoved, fRunning)
	}
	return false
}

// processExpired handles all entries in the expired list.
// crtTime is the caller-provided timestamp for duration-based re-arms.
func (lt *LTimer) processExpired(now Ticks, crtTime timestamp.TS) {
	lst := &lt.expired
	for !lst.isEmpty() {
		t := lst.head.next
		lst.rm(t)
		t.next = nil
		t.prev = nil
		t.info.setFlags(fRunning)
		rearm, delta := t.f(lt, t, t.arg)
		if !rearm {
			t = nil
		}
		lt.afterRun(t, rearm, delta, crtTime)
	}
}

// run processes timers at the current tick.
// crtTime is the caller-provided timestamp for duration-based re-arms.
func (lt *LTimer) run(now Ticks, crtTime timestamp.TS) {
	lt.redistTimers(now)
	lt.processExpired(now, crtTime)
}

// advanceTimeTo advances the internal time to t, running all expired timers.
// For each tick, the corresponding timestamp is computed from the tick value.
func (lt *LTimer) advanceTimeTo(t Ticks) {
	if lt.crtTicks.GT(t) {
		BUG("ltimer: advancing backwards: crt %d target %d (ticks)\n",
			lt.crtTicks.Val(), t.Val())
		return
	}
	for lt.crtTicks.NE(t) {
		lt.crtTicks = lt.crtTicks.AddUint64(1)
		lt.run(lt.crtTicks, lt.Timestamp(lt.crtTicks))
	}
}

// Run advances the local timer to the given timestamp, firing all
// expired timers along the way.
func (lt *LTimer) Run(crtTime timestamp.TS) {
	elapsed := crtTime.Sub(lt.refTS)
	crtTicks := lt.refTicks.Add(lt.TicksRoundUp(elapsed))
	lt.advanceTimeTo(crtTicks)
}

// RunT advances the local timer to crtTicks, firing all expired timers.
func (lt *LTimer) RunT(crtTicks Ticks) {
	lt.advanceTimeTo(crtTicks)
}

// Advance advances the local timer by the given duration, firing all
// expired timers.
func (lt *LTimer) Advance(d time.Duration, crtTime timestamp.TS) {
	delta := lt.TicksRoundUp(d)
	lt.advanceTimeTo(lt.crtTicks.Add(delta))
}

// AdvanceT advances the local timer by delta ticks, firing all
// expired timers.
func (lt *LTimer) AdvanceT(delta Ticks) {
	lt.advanceTimeTo(lt.crtTicks.Add(delta))
}
