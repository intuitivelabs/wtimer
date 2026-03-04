package wtimer

import (
	"testing"
	"time"

	"github.com/intuitivelabs/timestamp"
)

func TestLTimerInit(t *testing.T) {
	var lt LTimer

	if err := lt.Init(time.Millisecond); err != nil {
		t.Fatalf("LTimer init failure: %s\n", err)
	}

	// check all wheel lists are properly initialized
	for i := 0; i < len(lt.wlists); i++ {
		if lt.wlists[i].head.next != lt.wlists[i].head.prev ||
			lt.wlists[i].head.next != &lt.wlists[i].head ||
			lt.wlists[i].head.prev != &lt.wlists[i].head {
			t.Errorf("LTimer wlists[%d] not properly init\n", i)
		}
		flags := lt.wlists[i].head.info.flags()
		if flags&fHead == 0 {
			t.Errorf("LTimer wlists[%d] missing fHead flag\n", i)
		}
	}

	// check wheels
	tsz := 0
	for i := 0; i < len(lt.wheels); i++ {
		sz := len(lt.wheels[i].lsts)
		if sz != int(wheelEntries[i]) {
			t.Errorf("LTimer wheel %d: wrong slice size: %d, expected %d\n",
				i, sz, wheelEntries[i])
		}
		tsz += sz
	}
	if tsz != wTotalEntries {
		t.Errorf("LTimer: wrong total wheel entries: %d\n", tsz)
	}

	// check expired list
	if lt.expired.head.next != lt.expired.head.prev ||
		lt.expired.head.next != &lt.expired.head {
		t.Errorf("LTimer expired not properly init\n")
	}
	if lt.expired.wheelNo != wheelExp || lt.expired.wheelIdx != wheelNoIdx {
		t.Errorf("LTimer expired wrong wheel/idx: %d/%d\n",
			lt.expired.wheelNo, lt.expired.wheelIdx)
	}
}

func TestLTimerAddFire(t *testing.T) {
	var lt LTimer
	var tl LTimerLnk
	fired := 0

	if err := lt.Init(time.Millisecond); err != nil {
		t.Fatalf("LTimer init failure: %s\n", err)
	}

	f := func(lt *LTimer, h *LTimerLnk, p interface{}) (bool, time.Duration) {
		fired++
		return false, 0
	}

	lt.InitTimer(&tl)
	if err := lt.AddT(&tl, NewTicks(5), f, nil); err != nil {
		t.Fatalf("AddT failed: %s\n", err)
	}
	if tl.info.flags()&fActive == 0 {
		t.Fatalf("timer not marked active after AddT\n")
	}

	// advance 4 ticks - should not fire
	lt.AdvanceT(NewTicks(4))
	if fired != 0 {
		t.Fatalf("timer fired too early: %d\n", fired)
	}

	// advance 1 more tick - should fire
	lt.AdvanceT(NewTicks(1))
	if fired != 1 {
		t.Fatalf("timer should have fired once, fired %d\n", fired)
	}
}

func TestLTimerPeriodic(t *testing.T) {
	var lt LTimer
	var tl LTimerLnk
	fired := 0

	if err := lt.Init(time.Millisecond); err != nil {
		t.Fatalf("LTimer init failure: %s\n", err)
	}

	f := func(lt *LTimer, h *LTimerLnk, p interface{}) (bool, time.Duration) {
		fired++
		if fired < 5 {
			return true, Periodic
		}
		return false, 0
	}

	lt.InitTimer(&tl)
	if err := lt.AddT(&tl, NewTicks(10), f, nil); err != nil {
		t.Fatalf("AddT failed: %s\n", err)
	}

	// advance enough ticks for 5 firings: 10 + 10 + 10 + 10 + 10 = 50
	lt.AdvanceT(NewTicks(50))
	if fired != 5 {
		t.Fatalf("periodic timer: expected 5 firings, got %d\n", fired)
	}
}

func TestLTimerDel(t *testing.T) {
	var lt LTimer
	var tl LTimerLnk
	fired := 0

	if err := lt.Init(time.Millisecond); err != nil {
		t.Fatalf("LTimer init failure: %s\n", err)
	}

	f := func(lt *LTimer, h *LTimerLnk, p interface{}) (bool, time.Duration) {
		fired++
		return false, 0
	}

	lt.InitTimer(&tl)
	if err := lt.AddT(&tl, NewTicks(10), f, nil); err != nil {
		t.Fatalf("AddT failed: %s\n", err)
	}

	// delete before it fires
	ok, err := lt.Del(&tl)
	if err != nil || !ok {
		t.Fatalf("Del failed: ok=%v err=%v\n", ok, err)
	}

	// advance past expire - should not fire
	lt.AdvanceT(NewTicks(20))
	if fired != 0 {
		t.Fatalf("timer fired after Del: %d\n", fired)
	}
}

func TestLTimerDelInsideCallback(t *testing.T) {
	var lt LTimer
	var tl1, tl2 LTimerLnk
	fired1, fired2 := 0, 0

	if err := lt.Init(time.Millisecond); err != nil {
		t.Fatalf("LTimer init failure: %s\n", err)
	}

	f2 := func(lt *LTimer, h *LTimerLnk, p interface{}) (bool, time.Duration) {
		fired2++
		return false, 0
	}

	// f1 fires at tick 5, deletes tl2 (scheduled for tick 10)
	f1 := func(lt *LTimer, h *LTimerLnk, p interface{}) (bool, time.Duration) {
		fired1++
		lt.Del(&tl2)
		return false, 0
	}

	lt.InitTimer(&tl1)
	lt.InitTimer(&tl2)
	if err := lt.AddT(&tl1, NewTicks(5), f1, nil); err != nil {
		t.Fatalf("AddT tl1 failed: %s\n", err)
	}
	if err := lt.AddT(&tl2, NewTicks(10), f2, nil); err != nil {
		t.Fatalf("AddT tl2 failed: %s\n", err)
	}

	lt.AdvanceT(NewTicks(15))
	if fired1 != 1 {
		t.Fatalf("tl1 should have fired once, fired %d\n", fired1)
	}
	if fired2 != 0 {
		t.Fatalf("tl2 should not have fired (deleted), fired %d\n", fired2)
	}
}

func TestLTimerMultiple(t *testing.T) {
	var lt LTimer
	const N = 100
	var timers [N]LTimerLnk
	var firedAt [N]uint64

	if err := lt.Init(time.Millisecond); err != nil {
		t.Fatalf("LTimer init failure: %s\n", err)
	}

	for i := 0; i < N; i++ {
		lt.InitTimer(&timers[i])
		idx := i
		f := func(lt *LTimer, h *LTimerLnk, p interface{}) (bool, time.Duration) {
			firedAt[idx] = lt.Now().Val()
			return false, 0
		}
		if err := lt.AddT(&timers[i], NewTicks(uint64(i+1)), f, nil); err != nil {
			t.Fatalf("AddT timer %d failed: %s\n", i, err)
		}
	}

	lt.AdvanceT(NewTicks(N + 1))

	for i := 0; i < N; i++ {
		expected := uint64(i + 1)
		if firedAt[i] != expected {
			t.Errorf("timer %d: fired at tick %d, expected %d\n",
				i, firedAt[i], expected)
		}
	}
}

func TestLTimerRun(t *testing.T) {
	var lt LTimer
	fired := 0

	if err := lt.Init(time.Millisecond); err != nil {
		t.Fatalf("LTimer init failure: %s\n", err)
	}

	refTS := timestamp.Now()
	lt.SetRefTS(refTS, lt.Now())

	var tl LTimerLnk
	lt.InitTimer(&tl)

	f := func(lt *LTimer, h *LTimerLnk, p interface{}) (bool, time.Duration) {
		fired++
		return false, 0
	}
	if err := lt.Add(&tl, 5*time.Millisecond, refTS, f, nil); err != nil {
		t.Fatalf("Add failed: %s\n", err)
	}

	// Run with timestamp 10ms after reference
	lt.Run(refTS.Add(10 * time.Millisecond))
	if fired != 1 {
		t.Fatalf("Run: expected 1 firing, got %d\n", fired)
	}
}

func TestLTimerAdvance(t *testing.T) {
	var lt LTimer
	fired := 0

	if err := lt.Init(time.Millisecond); err != nil {
		t.Fatalf("LTimer init failure: %s\n", err)
	}

	var tl LTimerLnk
	lt.InitTimer(&tl)
	f := func(lt *LTimer, h *LTimerLnk, p interface{}) (bool, time.Duration) {
		fired++
		return false, 0
	}
	if err := lt.AddT(&tl, NewTicks(5), f, nil); err != nil {
		t.Fatalf("AddT failed: %s\n", err)
	}

	// Advance by 3ms then 3ms - should fire after second advance
	refTS := lt.Timestamp(lt.Now())
	lt.Advance(3*time.Millisecond, refTS.Add(3*time.Millisecond))
	if fired != 0 {
		t.Fatalf("timer fired too early\n")
	}
	lt.Advance(3*time.Millisecond, refTS.Add(6*time.Millisecond))
	if fired != 1 {
		t.Fatalf("expected 1 firing, got %d\n", fired)
	}
}

func TestLTimerTimestamp(t *testing.T) {
	var lt LTimer

	if err := lt.Init(time.Millisecond); err != nil {
		t.Fatalf("LTimer init failure: %s\n", err)
	}

	refTS := timestamp.Now()
	lt.SetRefTS(refTS, lt.Now())

	// advance 100 ticks
	lt.AdvanceT(NewTicks(100))

	ts := lt.Timestamp(lt.Now())
	expected := refTS.Add(100 * time.Millisecond)
	diff := ts.Sub(expected)
	if diff < 0 {
		diff = -diff
	}
	if diff > time.Microsecond {
		t.Errorf("Timestamp mismatch: got %s, expected %s, diff %s\n",
			ts, expected, diff)
	}
}

func TestLTimerTickConversions(t *testing.T) {
	var lt LTimer

	if err := lt.Init(time.Millisecond); err != nil {
		t.Fatalf("LTimer init failure: %s\n", err)
	}

	// Ticks round-down
	ticks, rest := lt.Ticks(5500 * time.Microsecond)
	if ticks.Val() != 5 {
		t.Errorf("Ticks: expected 5, got %d\n", ticks.Val())
	}
	if rest != 500*time.Microsecond {
		t.Errorf("Ticks rest: expected 500us, got %s\n", rest)
	}

	// Duration
	d := lt.Duration(NewTicks(10))
	if d != 10*time.Millisecond {
		t.Errorf("Duration: expected 10ms, got %s\n", d)
	}

	// TicksRoundUp
	ticks = lt.TicksRoundUp(500 * time.Microsecond)
	if ticks.Val() != 1 {
		t.Errorf("TicksRoundUp(500us): expected 1, got %d\n", ticks.Val())
	}
	ticks = lt.TicksRoundUp(1500 * time.Microsecond)
	if ticks.Val() != 2 {
		t.Errorf("TicksRoundUp(1500us): expected 2, got %d\n", ticks.Val())
	}
}

func TestLTimerRearm(t *testing.T) {
	var lt LTimer
	var tl LTimerLnk
	fired := 0

	if err := lt.Init(time.Millisecond); err != nil {
		t.Fatalf("LTimer init failure: %s\n", err)
	}

	// re-arm with different interval
	f := func(lt *LTimer, h *LTimerLnk, p interface{}) (bool, time.Duration) {
		fired++
		if fired == 1 {
			return true, 20 * time.Millisecond // change interval
		}
		return false, 0
	}

	lt.InitTimer(&tl)
	if err := lt.AddT(&tl, NewTicks(10), f, nil); err != nil {
		t.Fatalf("AddT failed: %s\n", err)
	}

	// fire first at tick 10
	lt.AdvanceT(NewTicks(10))
	if fired != 1 {
		t.Fatalf("expected 1 firing, got %d\n", fired)
	}

	// fire second at tick 30 (10 + 20)
	lt.AdvanceT(NewTicks(19))
	if fired != 1 {
		t.Fatalf("fired too early after rearm: %d\n", fired)
	}
	lt.AdvanceT(NewTicks(1))
	if fired != 2 {
		t.Fatalf("expected 2 firings, got %d\n", fired)
	}
}

func TestLTimerExpire(t *testing.T) {
	var lt LTimer
	var tl LTimerLnk

	if err := lt.Init(time.Millisecond); err != nil {
		t.Fatalf("LTimer init failure: %s\n", err)
	}

	refTS := timestamp.Now()
	lt.SetRefTS(refTS, lt.Now())

	lt.InitTimer(&tl)
	f := func(lt *LTimer, h *LTimerLnk, p interface{}) (bool, time.Duration) {
		return false, 0
	}
	if err := lt.Add(&tl, 50*time.Millisecond, refTS, f, nil); err != nil {
		t.Fatalf("Add failed: %s\n", err)
	}

	expTS := lt.Expire(&tl)
	expected := refTS.Add(50 * time.Millisecond)
	diff := expTS.Sub(expected)
	if diff < 0 {
		diff = -diff
	}
	if diff > time.Microsecond {
		t.Errorf("Expire mismatch: got %s, expected %s, diff %s\n",
			expTS, expected, diff)
	}
}

// --- Time-based tests (Add + Run/Advance with timestamps) ---

func TestLTimerAddFireTime(t *testing.T) {
	var lt LTimer
	var tl LTimerLnk
	fired := 0

	if err := lt.Init(time.Millisecond); err != nil {
		t.Fatalf("LTimer init failure: %s\n", err)
	}

	refTS := lt.Timestamp(lt.Now())

	f := func(lt *LTimer, h *LTimerLnk, p interface{}) (bool, time.Duration) {
		fired++
		return false, 0
	}

	lt.InitTimer(&tl)
	if err := lt.Add(&tl, 5*time.Millisecond, refTS, f, nil); err != nil {
		t.Fatalf("Add failed: %s\n", err)
	}
	if tl.info.flags()&fActive == 0 {
		t.Fatalf("timer not marked active after Add\n")
	}

	// advance 4ms - should not fire
	lt.Run(refTS.Add(4 * time.Millisecond))
	if fired != 0 {
		t.Fatalf("timer fired too early: %d\n", fired)
	}

	// advance to 6ms - should fire
	lt.Run(refTS.Add(6 * time.Millisecond))
	if fired != 1 {
		t.Fatalf("timer should have fired once, fired %d\n", fired)
	}
}

func TestLTimerPeriodicTime(t *testing.T) {
	var lt LTimer
	var tl LTimerLnk
	fired := 0

	if err := lt.Init(time.Millisecond); err != nil {
		t.Fatalf("LTimer init failure: %s\n", err)
	}

	refTS := lt.Timestamp(lt.Now())

	f := func(lt *LTimer, h *LTimerLnk, p interface{}) (bool, time.Duration) {
		fired++
		if fired < 5 {
			return true, Periodic
		}
		return false, 0
	}

	lt.InitTimer(&tl)
	if err := lt.Add(&tl, 10*time.Millisecond, refTS, f, nil); err != nil {
		t.Fatalf("Add failed: %s\n", err)
	}

	// advance enough for 5 firings: 10 + 10 + 10 + 10 + 10 = 50ms
	lt.Run(refTS.Add(50 * time.Millisecond))
	if fired != 5 {
		t.Fatalf("periodic timer: expected 5 firings, got %d\n", fired)
	}
}

func TestLTimerDelTime(t *testing.T) {
	var lt LTimer
	var tl LTimerLnk
	fired := 0

	if err := lt.Init(time.Millisecond); err != nil {
		t.Fatalf("LTimer init failure: %s\n", err)
	}

	refTS := lt.Timestamp(lt.Now())

	f := func(lt *LTimer, h *LTimerLnk, p interface{}) (bool, time.Duration) {
		fired++
		return false, 0
	}

	lt.InitTimer(&tl)
	if err := lt.Add(&tl, 10*time.Millisecond, refTS, f, nil); err != nil {
		t.Fatalf("Add failed: %s\n", err)
	}

	ok, err := lt.Del(&tl)
	if err != nil || !ok {
		t.Fatalf("Del failed: ok=%v err=%v\n", ok, err)
	}

	lt.Run(refTS.Add(20 * time.Millisecond))
	if fired != 0 {
		t.Fatalf("timer fired after Del: %d\n", fired)
	}
}

func TestLTimerRearmTime(t *testing.T) {
	var lt LTimer
	var tl LTimerLnk
	fired := 0

	if err := lt.Init(time.Millisecond); err != nil {
		t.Fatalf("LTimer init failure: %s\n", err)
	}

	refTS := lt.Timestamp(lt.Now())

	f := func(lt *LTimer, h *LTimerLnk, p interface{}) (bool, time.Duration) {
		fired++
		if fired == 1 {
			return true, 20 * time.Millisecond
		}
		return false, 0
	}

	lt.InitTimer(&tl)
	if err := lt.Add(&tl, 10*time.Millisecond, refTS, f, nil); err != nil {
		t.Fatalf("Add failed: %s\n", err)
	}

	// fire first at 10ms
	lt.Run(refTS.Add(10 * time.Millisecond))
	if fired != 1 {
		t.Fatalf("expected 1 firing, got %d\n", fired)
	}

	// should not fire at 29ms
	lt.Run(refTS.Add(29 * time.Millisecond))
	if fired != 1 {
		t.Fatalf("fired too early after rearm: %d\n", fired)
	}

	// fire second at 30ms (10 + 20)
	lt.Run(refTS.Add(30 * time.Millisecond))
	if fired != 2 {
		t.Fatalf("expected 2 firings, got %d\n", fired)
	}
}

func TestLTimerMultipleTime(t *testing.T) {
	var lt LTimer
	const N = 100
	var timers [N]LTimerLnk
	var firedAt [N]uint64

	if err := lt.Init(time.Millisecond); err != nil {
		t.Fatalf("LTimer init failure: %s\n", err)
	}

	refTS := lt.Timestamp(lt.Now())

	for i := 0; i < N; i++ {
		lt.InitTimer(&timers[i])
		idx := i
		f := func(lt *LTimer, h *LTimerLnk, p interface{}) (bool, time.Duration) {
			firedAt[idx] = lt.Now().Val()
			return false, 0
		}
		d := time.Duration(i+1) * time.Millisecond
		if err := lt.Add(&timers[i], d, refTS, f, nil); err != nil {
			t.Fatalf("Add timer %d failed: %s\n", i, err)
		}
	}

	lt.Run(refTS.Add(time.Duration(N+1) * time.Millisecond))

	for i := 0; i < N; i++ {
		expected := uint64(i + 1)
		if firedAt[i] != expected {
			t.Errorf("timer %d: fired at tick %d, expected %d\n",
				i, firedAt[i], expected)
		}
	}
}

func TestLTimerDelInsideCallbackTime(t *testing.T) {
	var lt LTimer
	var tl1, tl2 LTimerLnk
	fired1, fired2 := 0, 0

	if err := lt.Init(time.Millisecond); err != nil {
		t.Fatalf("LTimer init failure: %s\n", err)
	}

	refTS := lt.Timestamp(lt.Now())

	f2 := func(lt *LTimer, h *LTimerLnk, p interface{}) (bool, time.Duration) {
		fired2++
		return false, 0
	}

	f1 := func(lt *LTimer, h *LTimerLnk, p interface{}) (bool, time.Duration) {
		fired1++
		lt.Del(&tl2)
		return false, 0
	}

	lt.InitTimer(&tl1)
	lt.InitTimer(&tl2)
	if err := lt.Add(&tl1, 5*time.Millisecond, refTS, f1, nil); err != nil {
		t.Fatalf("Add tl1 failed: %s\n", err)
	}
	if err := lt.Add(&tl2, 10*time.Millisecond, refTS, f2, nil); err != nil {
		t.Fatalf("Add tl2 failed: %s\n", err)
	}

	lt.Run(refTS.Add(15 * time.Millisecond))
	if fired1 != 1 {
		t.Fatalf("tl1 should have fired once, fired %d\n", fired1)
	}
	if fired2 != 0 {
		t.Fatalf("tl2 should not have fired (deleted), fired %d\n", fired2)
	}
}

// --- Benchmarks ---

func BenchmarkLTimerAddDel(b *testing.B) {
	var lt LTimer
	if err := lt.Init(time.Millisecond); err != nil {
		b.Fatalf("LTimer init failure: %s\n", err)
	}

	f := func(lt *LTimer, h *LTimerLnk, p interface{}) (bool, time.Duration) {
		return false, 0
	}

	timers := make([]LTimerLnk, b.N)
	for i := range timers {
		lt.InitTimer(&timers[i])
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		lt.AddT(&timers[i], NewTicks(100), f, nil)
		lt.Del(&timers[i])
		timers[i].next = nil
		timers[i].prev = nil
		lt.ResetTimer(&timers[i])
	}
}

func BenchmarkLTimerFire(b *testing.B) {
	var lt LTimer
	if err := lt.Init(time.Millisecond); err != nil {
		b.Fatalf("LTimer init failure: %s\n", err)
	}

	f := func(lt *LTimer, h *LTimerLnk, p interface{}) (bool, time.Duration) {
		return false, 0
	}

	// pre-allocate
	timers := make([]LTimerLnk, b.N)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		lt.InitTimer(&timers[i])
		lt.AddT(&timers[i], NewTicks(1), f, nil)
	}
	lt.AdvanceT(NewTicks(1))
}
