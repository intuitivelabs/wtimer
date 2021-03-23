// Copyright 2021 Intuitive Labs GmbH. All rights reserved.
//
// Use of this source code is governed by a BSD-style license
// that can be found in the LICENSE.txt file in the root of the source
// tree.

package wtimer

import (
	"time"

	"github.com/intuitivelabs/timestamp"
)

// start runq "workers"
func (wt *WTimer) startRQ() {
	// start run queue "workers"
	for i := 0; i < runQueuesWorkersNo; i++ {
		wt.wg.Add(1)
		go func() {
			defer wt.wg.Done()
			wt.runqListen(wt.rQch)
		}()
	}
}

// Start will start the timer wheel (timer + workers).
// No timers will be run if Start() was not called.
// In most cases it should be used right after Init().
func (wt *WTimer) Start() {
	wt.cancel = make(chan struct{})
	wt.lastTickT = timestamp.Now()
	wt.refTS = wt.lastTickT
	wt.refTicks = wt.Now()
	wt.startRQ()
	wt.wg.Add(1)
	go func() {
		defer wt.wg.Done()
		if DBGon() {
			DBG("starting ticker with %s at %s\n", wt.tickDuration, time.Now())
		}
		wt.lastTickT = timestamp.Now()
		wt.refTS = wt.lastTickT
		ticker := time.NewTicker(wt.tickDuration)
	loop:
		for {
			select {
			case <-wt.cancel:
				DBG("canceled\n")
				break loop
			case _, ok := <-ticker.C:
				if !ok {
					break loop
				}
				wt.ticker()
			}
		}
		ticker.Stop()
	}()
}

// Shutdown will signal all the go routines to stop and will wait for them
// to finish.
func (wt *WTimer) Shutdown() {
	if wt.cancel != nil {
		close(wt.cancel)
	}
	wt.wg.Wait()
}
