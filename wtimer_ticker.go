// Copyright 2021 Intuitive Labs GmbH. All rights reserved.
//
// Use of this source code is governed by a BSD-style license
// that can be found in the LICENSE.txt file in the root of the source
// tree.

package wtimer

import (
	"github.com/intuitivelabs/timestamp"
)

// ticker should be called periodically, ideally at each tick duration
// _must_ not ever be called in parallel.
func (wt *WTimer) ticker() uint64 {
	now := timestamp.Now()
	if now.Before(wt.lastTickT) {
		// time going backwards!!
		wt.badTime++
		if wt.badTime > 10 {
			// re-init
			if ERRon() {
				ERR("trying to recover after time going backward %d times"+
					" with %s\n",
					wt.badTime, wt.lastTickT.Sub(now))
			}
			wt.lastTickT = now
			wt.refTS = wt.lastTickT
			wt.refTicks = wt.Now()
		} else if DBGon() {
			DBG("ticker: time going backward with %s (%d times)\n",
				wt.lastTickT.Sub(now), wt.badTime)
		}
		return 0
	}
	wt.badTime = 0
	if now.Sub(wt.refTS)/wt.tickDuration > (MaxTicksDiff - 2) {
		if DBGon() {
			DBG("ticker: ticks ref value overflowing after %s"+
				" (max ticks %d) -> re-adjusting\n",
				now.Sub(wt.refTS), MaxTicksDiff)
		}
		// re-init, we risk overflowing the ticks
		// new ref. ts = last tick ts
		// new ref ticks = current tick - Ticks(now - last tick ts)
		diff, _ := wt.Ticks(now.Sub(wt.lastTickT))
		wt.refTS = wt.lastTickT
		wt.refTicks = wt.Now().Sub(diff)
	}

	runTime := now.Sub(wt.refTS)
	runTicks := wt.Now().Sub(wt.refTicks)
	if runTime > wt.Duration(runTicks.AddUint64(1+20)) {
		if DBGon() {
			lost, _ := wt.Ticks(runTime - wt.Duration(runTicks))
			DBG("ticker: lost ticks since start-up: too slow:"+
				" ticks diff %d = %s, but time diff %s => lost %d ticks\n",
				runTicks.Val(), wt.Duration(runTicks), runTime, lost.Val())
		}
	} else if runTicks.Val() > 1 &&
		runTime < wt.Duration(runTicks.SubUint64(1)) {
		if DBGon() {
			faster, _ := wt.Ticks(wt.Duration(runTicks) - runTime)
			DBG("ticker: lost ticks since start-up: too fast:"+
				" ticks diff %d = %s time  diff %s => faster with %d ticks\n",
				runTicks.Val(), wt.Duration(runTicks), runTime, faster.Val())
		}
	}
	diff := now.Sub(wt.lastTickT)
	if diff < wt.tickDuration {
		// to little time has passed
		return 0
	}
	ticks, rest := wt.Ticks(diff)

	wt.lastTickT = now.Add(-rest)
	wt.advanceTimeTo(wt.Now().Add(ticks))
	return ticks.Val()
}
