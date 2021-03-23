# wtimer

[![Go Reference](https://pkg.go.dev/badge/github.com/intuitivelabs/wtimer.svg)](https://pkg.go.dev/github.com/intuitivelabs/wtimer)

The wtimer package provides a timer implementation using a hierarchical timer
 wheel (see
G. Varghese, T. Lauck,  Hashed and Hierarchical Timing Wheels: Efficient
      Data Structures for Implementing a Timer Facility, 1996 ).

It is optimised for high number of timers (100k+) with relatively lower
precision requirements  (>20ms). Internal heap allocations can be completely
 avoided if the timer handler structures are provided by the caller and
  if logging is set to a lower level.

## Implementation Details


The timer precision is configurable, but should not be set too high. The
 lower the precision the less CPU usage. Precision (timer ticks) below
10ms should be avoided.

The timers use a 4 level hierarchical timing wheel. Small timeouts will go
 in the first wheel.
Each wheel acts as circular buffer, the current position corresponding to the
 current time ("normalised" to that wheel level:
 (time >> sum(previous wheels bits) & wheel\_mask) >> wheel\_bits).

Each time the current ticks are a multiple of a wheel range, the corresponding
 wheel current position list will be re-distributed to the wheel before it
 and the wheel current position will be increased (or to put it another
  way each time a a ticks change causes a wheel current position change,
   all the entries at that wheel position will be re-distributed to the
  wheels before it).
If the wheel is 0 (e.g. ticks increased by 1) the corresponding list will be
 scheduled for execution.

Timers can be added and stopped dynamically, one shot and periodic timers
are supported.

Timers can be configured individually to be executed by a fixed group of
 separate go routines "workers" (default), in a new temporary go routine or
 in the main timer context (this options is for timers that execute really
 fast and need a bit lower latency).

There are two ways to use the wtimer package: configure it to automatically
 run the timers at periodic intervals or run the timers manually, providing
 the number of ticks elapsed between calls.


## Timer Handlers

Each timer handler is a pointer to a structure that can be integrated in
 an existing data type, avoiding an extra allocation for the timer handle.


## Timer Ticks

The wtimer package uses internally ticks (wtimer.Tick) to store the time.
The duration of a tick is configured when the wtimer.WTimer is initialised.

All the timeouts will be rounded to ticks duration multiples (see
 wtimer.TicksRoundUp() for details). 0 timeouts are not allowed and will
  be all rounded to 1 tick.

Ticks values smaller then 50ms will cause some visible cpu usage when idle.
Some orientative numbers (measured with cpu frequency scaling enabled):

```
    100ms =>       0 %cpu idle
     10ms =>       1-1.5% cpu idle
      1ms =>       6-9%  cpu
      0.5ms =>    11% cpu
      0.25ms =>    7-8%   cpu
      0.125ms =>  12-14% cpu
      0.062ms =>  20-22% cpu
      0.031ms =>  23-24% cpu
      0.015ms =>  25-26% cpu
      0.007ms =>  32-34% cpu
      0.003ms =>  65-70% cpu !!!
      0.001ms => 133-135% cpu !!!
```

## Example

```
import (
	"time"
	"github.com/intuitivelabs/wtimer"
)


var timers wtimer.WTimer

func init() {
	if err := timers.Init(100*time.Millisecond); err != nil {
		panic("timers init failed\n")
	}
	timers.Start()
}


type myData struct {
	timer TimerLnk
	// ....
}

func timeoutHandler(wt *wtimer.WTimer, h *wtimer.TimerLnk,
	a interface{}) (bool, time.Duration) {
	d := a.(*myData)

	// stop := doSomething(d)
	
	if stop {
		return false, 0 // don't re-add
	}
	return true, wtimer.Periodic
	// or something like to increase the timerou
	// return true, 2*h.Intvl()
}

func startTimer(d *myData) {
	err := timers.InitTimer(&d.timer, 0)
	if err != nil {
		// ...
	}
	err = timers.Add(&d.timer, 1*time.Second, timeoutHandler, d)
	if err != nil {
		// ...
	}
}

func stopTimer(d* myData)  {
	ok, err := timers.Del(&d.timer)
	if !ok {
		// timeoutHandler running right now  ...
		// could busy wait for it with timers.DelWait(&d.timer)
	}
}
```

# Timer Flags

Each timers has some configuration flags that can be set at Init() or
 Reset() time.
The possible values are:

 - 0 (not flags set): the timer callback will be executed in one of the
 dedicated "workers"

 - Ffast: the callback will be executed in the main goroutine that is
  responsible for doing all the timers management. In this case the timer
  should execute really fast since any delay will affect all other timers.

 - FGoR: the callback will be executed in its own separate temporary
 goroutine. Ideal for timers spending a lot of time or doing potentially
 blocking calls.


# Goroutines

After Start() is called the wtimer package will start 9 fixed goroutines and
 some temporary ones:

* 1 for managing the timer wheels and advancing the internal ticks,
based on the system time (will run each tick interval)

* 8 (runQueuesWorkersNo) for executing timers that don't have any flags
 set (default)

* variable numbers of goroutines for executing timers configured with the
 FGoR flag (one temporary goroutine for each of these timers)

## Limitations

 * the timer latency is +/- 1 tick + an additional ~20ms

 * low values (less then 10ms) for ticks would cause an increase in
   CPU usage when idle

