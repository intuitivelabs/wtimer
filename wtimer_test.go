package wtimer

import (
	"fmt"
	"math/rand"
	"os"
	"sync/atomic"
	"testing"
	"time"
	//"github.com/intuitivelabs/slog"
)

const iterations = 1000

func TestWTimerConsts(t *testing.T) {
	tbits := W0Bits + W1Bits + W2Bits + W3Bits
	if WheelsNo != 4 {
		fmt.Printf("wheels number changed (%d was 4), tests need update\n",
			WheelsNo)
		os.Exit(-1)
	}
	if TicksBits != tbits {
		fmt.Printf("wheels total bits size != ticks: %d != %d\n",
			TicksBits, tbits)

		os.Exit(-1)
	}
	if len(wheelEntries) != WheelsNo {
		t.Fatalf("wheelsEntries size is wrong: %d\n", len(wheelEntries))
	}
	wbits := [...]int{
		W0Bits,
		W1Bits,
		W2Bits,
		W3Bits,
	}
	for i, v := range wheelEntries {
		if v != (1 << wbits[i]) {
			t.Fatalf("wheelsEntries[%d] wrong: %d <> %d\n",
				i, v, 1<<wbits[i])
		}
	}
	n := 0
	for _, v := range wbits {
		n += 1 << v
	}
	if n != wTotalEntries {
		t.Fatalf("wTotalEntries wrong: %d <> %d\n",
			wTotalEntries, n)
	}
}

func TestWTimerInit(t *testing.T) {
	var wt WTimer

	if err := wt.Init(time.Millisecond); err != nil {
		t.Fatalf("WTimer init failure: %s\n", err)
	}
	for i := 0; i < len(wt.wlists); i++ {
		if wt.wlists[i].head.next != wt.wlists[i].head.prev ||
			wt.wlists[i].head.next != &wt.wlists[i].head ||
			wt.wlists[i].head.prev != &wt.wlists[i].head ||
			wt.wlists[i].head.next == nil || wt.wlists[i].head.prev == nil ||
			!wt.wlists[i].head.Detached() {
			t.Errorf("WTimer wlists[%d]  not properly init:"+
				" %p n: %p p:%p\n",
				i, &wt.wlists[i].head, wt.wlists[i].head.next,
				wt.wlists[i].head.prev)
		}
		flags := wt.wlists[i].head.info.flags()
		wheel := wt.wlists[i].wheelNo
		idx := wt.wlists[i].wheelIdx
		if flags&fHead == 0 || wheel >= WheelsNo {
			t.Errorf("WTimer wlists[%d]  not properly init:"+
				" flags 0x%x wheel %d idx %d \n",
				i, flags, wheel, idx)
		}
	}

	tsz := 0
	for i := 0; i < len(wt.wheels); i++ {
		sz := len(wt.wheels[i].lsts)
		if sz != int(wheelEntries[i]) {
			t.Errorf("WTimer wheel %d: wrong init slice size:"+
				" %d, expected %d\n",
				i, sz, wheelEntries[i])
		}
		tsz += sz
	}
	if tsz != wTotalEntries || tsz != len(wt.wlists) {
		t.Errorf("WTimer: wrong total wheel entries: %d\n", tsz)
	}

	for w := 0; w < len(wt.wheels); w++ {
		for i := 0; i < int(wheelEntries[w]); i++ {
			lst := &wt.wheels[w].lsts[i]
			if lst.head.next != lst.head.prev ||
				lst.head.next != &lst.head ||
				lst.head.prev != &lst.head ||
				lst.head.next == nil || lst.head.prev == nil ||
				!lst.head.Detached() {
				t.Errorf("WTimer wheels[%d].lsts[%d]  not properly init:"+
					" %p n: %p p:%p\n",
					w, i, &lst.head, lst.head.next, lst.head.prev)
			}
			flags := lst.head.info.flags()
			wheel := lst.wheelNo
			idx := lst.wheelIdx
			if flags&fHead == 0 || wheel != uint8(w) {
				t.Errorf("WTimer wheels[%d].lsts[%d]  not properly init:"+
					" flags 0x%x wheel %d idx %d \n",
					w, i, flags, wheel, idx)
			}
		}
	}

	if wt.expired.head.next != wt.expired.head.prev ||
		wt.expired.head.next != &wt.expired.head ||
		!wt.expired.head.Detached() {
		t.Errorf("WTimer expired  not properly init:"+
			" %p n: %p p:%p\n",
			&wt.expired.head, wt.expired.head.next, wt.expired.head.prev)
	}
	wheel := wt.expired.wheelNo
	idx := wt.expired.wheelIdx
	flags := wt.expired.head.info.flags()
	if flags&fHead == 0 || wheel != wheelExp || idx != wheelNoIdx {
		t.Errorf("WTimer expired  not properly init:"+
			" flags 0x%x wheel %d idx %d \n",
			flags, wheel, idx)
	}

	for i := 0; i < len(wt.rQs); i++ {
		if wt.rQs[i].head.next != wt.rQs[i].head.prev ||
			wt.rQs[i].head.next != &wt.rQs[i].head ||
			!wt.rQs[i].head.Detached() {
			t.Errorf("WTimer rQs[%d]  not properly init:"+
				" %p n: %p p:%p\n",
				i, &wt.rQs[i].head, wt.rQs[i].head.next,
				wt.rQs[i].head.prev)
		}
		wheel := wt.rQs[i].wheelNo
		idx := wt.rQs[i].wheelIdx
		flags := wt.rQs[i].head.info.flags()
		if flags&fHead == 0 || wheel != wheelRQ || int(idx) != i {
			t.Errorf("WTimer rQs[%d]  not properly init:"+
				" flags 0x%x wheel %d idx %d \n",
				i, flags, wheel, idx)
		}
	}
}

func TestWTimerStart(t *testing.T) {
	var wt WTimer
	var tl TimerLnk
	var r uint64
	var start time.Time
	var last time.Time
	var exec [7]time.Duration

	f := func(wt *WTimer, h *TimerLnk, p interface{}) (bool, time.Duration) {
		now := time.Now()
		expire := h.Exp()
		if last.IsZero() {
			last = now
			t.Logf("timer: exp. ticks %d (%s) now %d (%s)  diff %s p=%v\n",
				expire.Val(), wt.Duration(expire),
				wt.Now().Val(), wt.Duration(wt.Now()),
				last.Sub(start), p)
		} else {
			t.Logf("timer: exp. ticks %d (%s) now %d (%s)"+
				"  diff last run %s start %s\n",
				expire.Val(), wt.Duration(expire),
				wt.Now().Val(), wt.Duration(wt.Now()),
				now.Sub(last), now.Sub(start))
			last = now
		}
		exec[r] = now.Sub(start)
		r++
		if r < 7 {
			return true, Periodic
		}
		return false, 0
	}

	if err := wt.Init(time.Millisecond * 2); err != nil {
		t.Fatalf("WTimer init failure: %s\n", err)
	}
	wt.Start()

	wt.InitTimer(&tl, 0)
	/* -- now Add accepts smaller time and rounds it up
	err := wt.Add(&tl, time.Millisecond/100, f, nil)
	if err == nil {
		t.Fatalf("Add did not fail with invalid time\n")
	}
	*/
	time.Sleep(101 * time.Millisecond)
	start = time.Now()
	err := wt.Add(&tl, 20*time.Millisecond, f, nil)
	if err != nil {
		t.Fatalf("Add  failed with %q\n", err)
	}
	time.Sleep(150 * time.Millisecond)
	if r != 7 {
		t.Errorf("Timer executed only %d times\n", r)
	}
	if last.IsZero() {
		t.Fatalf("Timer never executed\n")
	}
	diff := last.Sub(start)
	if diff > (2*time.Millisecond+time.Millisecond/2) ||
		diff < (2*time.Millisecond-time.Millisecond/2) {
		//t.Errorf("timer delay wrong: %s\n", diff)
	}
	t.Logf("timer last run after %s, runs %v\n", diff, exec)

	wt.Shutdown()
}

func TestWTimerRun(t *testing.T) {
	var wt WTimer
	var tl TimerLnk
	var runs uint64

	//slog.SetLevel(&Log, slog.LWARN)

	f := func(wt *WTimer, h *TimerLnk, p interface{}) (bool, time.Duration) {
		runs++
		return false, 0
	}

	if err := wt.Init(time.Millisecond * 1); err != nil {
		t.Fatalf("WTimer init failure: %s\n", err)
	}

	for i := 0; i < iterations; i++ {

		delta := uint64(rand.Int63n(MaxTicksDiff))
		now := wt.Now()
		expire := now.AddUint64(delta)
		wt.InitTimer(&tl, Ffast)
		err := wt.AddExpire(&tl, expire, f, nil)
		if err != nil {
			t.Fatalf("Add  failed with %q\n", err)
		}
		w0, idx0 := tl.info.wheelPos()
		runs = 0
		// simulate a running clock
		// timers on higher wheels are executed only on transitions
		// => 4 transitions one for each wheel
		t3 := expire.Val() & (W3Mask << (W2Bits + W1Bits + W0Bits))
		t2 := expire.Val() & (W2Mask << (W1Bits + W0Bits))
		t1 := expire.Val() & (W1Mask << (W0Bits))
		t0 := expire.Val() & W0Mask

		wt.run(NewTicks(t3))
		if t2 != 0 {
			wt.run(NewTicks(t3 + t2))
		}
		if t1 != 0 {
			wt.run(NewTicks(t3 + t2 + t1))
		}
		if t0 != 0 {
			wt.run(NewTicks(t3 + t2 + t1 + t0))
		}

		if runs != 1 {
			w, idx := tl.info.wheelPos()
			w0pos := wheel0Pos(expire.Val())
			w1pos := wheel1Pos(expire.Val())
			w2pos := wheel2Pos(expire.Val())
			w3pos := wheel3Pos(expire.Val())
			t.Errorf("timer execution %d times for delta %x expires %x"+
				" (now before %x, crt %x) added to wheel %d idx %d, "+
				" crt. wheel %d idx %d f: 0x%x"+
				" run idx 0:%d(e:%v) 1:%d(e:%v)"+
				" 2:%d(e:%v) 3:%d(e:%v)  expire e:%v"+
				" sim runs for %x %x %x %x\n",
				runs, delta, expire.Val(), now.Val(), wt.Now().Val(),
				w0, idx0, w, idx, tl.info.flags(),
				w0pos, wt.wheels[0].lsts[w0pos].isEmpty(),
				w1pos, wt.wheels[1].lsts[w1pos].isEmpty(),
				w2pos, wt.wheels[2].lsts[w2pos].isEmpty(),
				w3pos, wt.wheels[3].lsts[w3pos].isEmpty(),
				wt.expired.isEmpty(),
				t3, t3+t2, t3+t2+t1, t3+t2+t1+t0,
			)
		}
		if runs > 0 {
			if !tl.Detached() {
				t.Fatalf("timer not detached after execution"+
					" (%d run, delta %d expires %s)\n", i, delta, expire)
			}
			w, idx := tl.info.wheelPos()
			if w != wheelNone || idx != wheelNoIdx {
				t.Errorf("wrong wheel %d or idx %d"+
					" (%d run, delta %d expires %s)\n",
					w, idx, i, delta, expire)
			}
			if ok, err := wt.DelTry(&tl); ok || err != nil {
				t.Errorf("unexpected DelTry return %v , %q\n", ok, err)
			}
			if ok, err := wt.Del(&tl); ok || err != nil {
				t.Errorf("unexpected Del success %v, %q\n", ok, err)
			}
		}
	}
}

func TestWTadvanceTimeTo(t *testing.T) {
	var wt WTimer
	var tl TimerLnk
	var runs uint64
	const maxDiff = 128000

	//slog.SetLevel(&Log, slog.LWARN)

	f := func(wt *WTimer, h *TimerLnk, p interface{}) (bool, time.Duration) {
		runs++
		return false, 0
	}

	if err := wt.Init(time.Millisecond * 1); err != nil {
		t.Fatalf("WTimer init failure: %s\n", err)
	}

	for i := 0; i < iterations; i++ {
		delta := uint64(rand.Int63n(maxDiff))
		if i == 0 {
			// always test at least once with 0
			delta = uint64(0)
		}
		crtTicks := uint64(rand.Int63())
		wt.nowTicks = crtTicks // set start clock
		now := wt.Now()
		expire := now.AddUint64(delta)
		wt.InitTimer(&tl, Ffast)
		err := wt.AddExpire(&tl, expire, f, nil)
		if err != nil {
			t.Fatalf("Add  failed with %q\n", err)
		}
		w0, idx0 := tl.info.wheelPos()
		runs = 0
		// simulate a running clock
		// timers on higher wheels are executed only on transitions
		// => 4 transitions one for each wheel
		t3 := expire.Val() & (W3Mask << (W2Bits + W1Bits + W0Bits))
		t2 := expire.Val() & (W2Mask << (W1Bits + W0Bits))
		t1 := expire.Val() & (W1Mask << (W0Bits))
		t0 := expire.Val() & W0Mask

		if expire.EQ(wt.Now()) {
			// if expire == now, add 1 to it since otherwise
			// no timers will be executed...
			// advanceTimeTo executes timers between (crt, expire]
			wt.advanceTimeTo(expire.AddUint64(1))
		} else {
			wt.advanceTimeTo(expire)
		}

		if runs != 1 {
			w, idx := tl.info.wheelPos()
			w0pos := wheel0Pos(expire.Val())
			w1pos := wheel1Pos(expire.Val())
			w2pos := wheel2Pos(expire.Val())
			w3pos := wheel3Pos(expire.Val())
			t.Errorf("timer execution %d times for delta %x expires %x"+
				" (now before %x, crt %x) added to wheel %d idx %d, "+
				" crt. wheel %d idx %d f: 0x%x"+
				" run idx 0:%d(e:%v) 1:%d(e:%v)"+
				" 2:%d(e:%v) 3:%d(e:%v)  expire e:%v"+
				" sim runs for %x %x %x %x\n",
				runs, delta, expire.Val(), now.Val(), wt.Now().Val(),
				w0, idx0, w, idx, tl.info.flags(),
				w0pos, wt.wheels[0].lsts[w0pos].isEmpty(),
				w1pos, wt.wheels[1].lsts[w1pos].isEmpty(),
				w2pos, wt.wheels[2].lsts[w2pos].isEmpty(),
				w3pos, wt.wheels[3].lsts[w3pos].isEmpty(),
				wt.expired.isEmpty(),
				t3, t3+t2, t3+t2+t1, t3+t2+t1+t0,
			)
			// try to recover (the timer is active), it cannot be re-init
			// directly
			if ok, err := wt.Del(&tl); !ok {
				t.Fatalf("failed Del after exec failure: %s (aborting)\n",
					err)
			}
		}
		if runs > 0 {
			if !tl.Detached() {
				t.Fatalf("timer not detached after execution"+
					" (%d run, delta %d expires %s)\n", i, delta, expire)
			}
			w, idx := tl.info.wheelPos()
			if w != wheelNone || idx != wheelNoIdx {
				t.Errorf("wrong wheel %d or idx %d"+
					" (%d run, delta %d expires %s)\n",
					w, idx, i, delta, expire)
			}
			if ok, err := wt.DelTry(&tl); ok || err != nil {
				t.Errorf("unexpected DelTry return %v , %q\n", ok, err)
			}
			if ok, err := wt.Del(&tl); ok || err != nil {
				t.Errorf("unexpected Del success %v, %q\n", ok, err)
			}
		}
	}
}

func TestWTDel(t *testing.T) {
	var wt WTimer
	var tl TimerLnk
	var runs uint64
	var end uint64
	const maxDiff = 128000
	const flags = 0

	//slog.SetLevel(&Log, slog.LWARN)

	f := func(wt *WTimer, h *TimerLnk, p interface{}) (bool, time.Duration) {
		d := p.(time.Duration)
		atomic.AddUint64(&runs, 1)
		if d > 0 {
			time.Sleep(d)
		}
		atomic.AddUint64(&end, 1)
		if d > 0 {
			return true, d
		}
		return false, 0
	}

	if err := wt.Init(time.Millisecond * 1); err != nil {
		t.Fatalf("WTimer init failure: %s\n", err)
	}
	wt.Start()

	for i := 0; i < 10; i++ {
		//delta := uint64(rand.Int63n(maxDiff))
		expire := 100 * time.Millisecond
		runs = 0
		wt.InitTimer(&tl, flags)
		err := wt.Add(&tl, expire, f, time.Duration(0))
		if err != nil {
			t.Fatalf("Add  failed with %q\n", err)
		}
		time.Sleep(10 * time.Millisecond)
		ok, err := wt.Del(&tl)
		if !ok {
			t.Fatalf("failed to remove timer before expire: %q\n", err)
		}
		time.Sleep(150 * time.Millisecond)

		if atomic.LoadUint64(&runs) != 0 {
			t.Errorf("timer executed after successful delete\n")
		}

		if err := wt.Reset(&tl, flags); err != nil {
			t.Fatalf("Reset failed %q\n", err)
		}
		runs = 0
		expire = 10 * time.Millisecond
		err = wt.Add(&tl, expire, f, time.Duration(0))
		if err != nil {
			t.Fatalf("2nd Add  failed with %q\n", err)
		}
		time.Sleep(100 * time.Millisecond)

		if atomic.LoadUint64(&runs) != 1 {
			t.Errorf("timer not executed: %d\n", runs)
		} else {
			ok, err = wt.Del(&tl)
			if ok {
				t.Errorf("successful remove timer after forced-end: %q\n", err)
			} else {
				wt.InitTimer(&tl, flags)
			}
		}
		runs = 0
		end = 0
		expire = 20 * time.Millisecond
		err = wt.Add(&tl, expire, f, time.Duration(20*time.Millisecond))
		if err != nil {
			t.Fatalf("3rd Add  failed with %q\n", err)
		}
		time.Sleep(110 * time.Millisecond)

		if atomic.LoadUint64(&runs) != 3 {
			t.Errorf("timer  executed: %d times, %d ends\n", runs, end)
		}
		if ok, err := wt.Del(&tl); ok {
			t.Errorf("Del succeeded while deleting running timers:"+
				" runs %d ends %d: %s\n", runs, end, err)
		}
		time.Sleep(100 * time.Millisecond)
		if atomic.LoadUint64(&runs) != 3 {
			t.Errorf("timer  executed after delete %d times, %d ends\n",
				runs, end)
		}
	}
	wt.Shutdown()
}

func TestWTDelWait(t *testing.T) {
	var wt WTimer
	var tl TimerLnk
	var runs uint64
	var end uint64
	const maxDiff = 128000
	const flags = 0

	//slog.SetLevel(&Log, slog.LWARN)

	f := func(wt *WTimer, h *TimerLnk, p interface{}) (bool, time.Duration) {
		d := p.(time.Duration)
		atomic.AddUint64(&runs, 1)
		if d > 0 {
			time.Sleep(d)
		}
		atomic.AddUint64(&end, 1)
		if d > 0 {
			return true, d
		}
		return false, 0
	}

	if err := wt.Init(time.Millisecond * 1); err != nil {
		t.Fatalf("WTimer init failure: %s\n", err)
	}
	wt.Start()

	const scale = 5
	for i := 0; i < 10; i++ {
		//delta := uint64(rand.Int63n(maxDiff))
		expire := scale * 100 * time.Millisecond
		runs = 0
		wt.InitTimer(&tl, flags)
		err := wt.Add(&tl, expire, f, time.Duration(0))
		if err != nil {
			t.Fatalf("Add  failed with %q\n", err)
		}
		time.Sleep(scale * 10 * time.Millisecond)
		ok, err := wt.DelWait(&tl)
		if !ok {
			t.Fatalf("failed to remove timer before expire: %q\n", err)
		}
		time.Sleep(scale * 150 * time.Millisecond)

		if atomic.LoadUint64(&runs) != 0 {
			t.Errorf("timer executed after successful delete\n")
		}

		if err := wt.Reset(&tl, flags); err != nil {
			t.Fatalf("Reset failed %q\n", err)
		}
		runs = 0
		expire = scale * 10 * time.Millisecond
		err = wt.Add(&tl, expire, f, time.Duration(0))
		if err != nil {
			t.Fatalf("2nd Add  failed with %q\n", err)
		}
		time.Sleep(scale * 100 * time.Millisecond)

		if atomic.LoadUint64(&runs) != 1 {
			t.Errorf("timer not executed: %d\n", runs)
		} else {
			ok, err = wt.DelWait(&tl)
			if !ok {
				t.Errorf("failed to remove timer after forced-end: %q\n", err)
			} else {
				wt.InitTimer(&tl, flags)
			}
		}
		runs = 0
		end = 0
		expire = scale * 20 * time.Millisecond
		err = wt.Add(&tl, expire, f, time.Duration(scale*20*time.Millisecond))
		if err != nil {
			t.Fatalf("3rd Add  failed with %q\n", err)
		}
		time.Sleep(scale * 110 * time.Millisecond)

		if atomic.LoadUint64(&runs) != 3 {
			t.Errorf("timer  executed: %d times, %d ends\n", runs, end)
		}
		t0 := time.Now()
		if ok, err := wt.DelWait(&tl); !ok {
			t.Errorf("DelWait failed while deleting running timers:"+
				" runs %d ends %d: %s\n", runs, end, err)
		}
		t1 := time.Now()
		if t1.Sub(t0) > scale*20*time.Millisecond {
			t.Errorf("DelWait waited too long: %s\n", t1.Sub(t0))
		} else if t1.Sub(t0) < scale*5*time.Millisecond {
			t.Errorf("DelWait waited too short: %s\n", t1.Sub(t0))
		}
		t.Logf("DelWait waited %s\n", t1.Sub(t0))
		if atomic.LoadUint64(&runs) != 3 {
			t.Errorf("timer  executed after delete %d times, %d ends\n",
				runs, end)
		}
		time.Sleep(scale * 100 * time.Millisecond)
		if atomic.LoadUint64(&runs) != 3 {
			t.Errorf("timer  executed after delete %d times, %d ends\n",
				runs, end)
		}
	}
	wt.Shutdown()
}

func TestWTtimersSameInt(t *testing.T) {
	var wt WTimer
	var runs uint64
	var end uint64
	const maxDiff = 128000
	const flags = 0

	//slog.SetLevel(&Log, slog.LWARN)

	f := func(wt *WTimer, h *TimerLnk, p interface{}) (bool, time.Duration) {
		d := p.(time.Duration)
		atomic.AddUint64(&runs, 1)
		//if d > 0 {
		//	time.Sleep(d)
		//}
		atomic.AddUint64(&end, 1)
		if d > 0 {
			return true, d
		}
		return false, 0
	}

	if err := wt.Init(time.Millisecond * 1); err != nil {
		t.Fatalf("WTimer init failure: %s\n", err)
	}
	wt.Start()

	for i := 0; i < 10; i++ {
		delta := int64(rand.Int63n(int64(500 * time.Millisecond)))
		expire := time.Duration(delta) + wt.Duration(NewTicks(1))
		wait := expire + 100*time.Millisecond
		n := rand.Int63n(10) + 1
		timers := make([]TimerLnk, n)
		readd0 := make([]time.Duration, n)
		readd := make([]time.Duration, n)
		expk := make([]int64, n)
		runs = 0
		end = 0
		runsErr := uint64(0) // acceptable run errors due to latency
		expected := int64(0) // expected runs
		for k := 0; k < int(n); k++ {
			wt.InitTimer(&timers[k], flags)
			readd0[k] = time.Duration(rand.Int63n(int64(100*time.Millisecond))) + 30*time.Millisecond
			readd[k] = wt.Duration(wt.TicksRoundUp(readd0[k]))
			//readd[k] = 0
			err := wt.Add(&timers[k], expire, f, readd[k])
			if err != nil {
				t.Fatalf("Add  failed for timer %d  expire %s with %q\n",
					k, expire, err)
			}
			expected++
			if readd[k] != 0 {
				if wt.Duration(wt.TicksRoundUp(readd[k])) == 0 {
					ticks, rest := wt.Ticks(readd[k])
					t.Fatalf("ticks round up failed for %s => %s"+
						" val %d (nr %d rest %s)\n",
						readd[k], wt.Duration(wt.TicksRoundUp(readd[k])),
						wt.TicksRoundUp(readd[k]).Val(), ticks.Val(), rest)
				}
				// after expire timer executes,
				// then executes again after re-add[k]
				expected += int64(wait-expire) /
					int64(wt.Duration(wt.TicksRoundUp(readd[k])))
				expk[k] = int64(wait-expire) /
					int64(wt.Duration(wt.TicksRoundUp(readd[k])))
				rest := int64(wait-expire) %
					int64(wt.Duration(wt.TicksRoundUp(readd[k])))
				if rest < int64(10*time.Millisecond) {
					// too close, might be missed
					runsErr++
				}
			}
		}
		time.Sleep(wait)
		if atomic.LoadUint64(&runs) != uint64(expected) &&
			((atomic.LoadUint64(&runs) > (uint64(expected) + runsErr)) ||
				(atomic.LoadUint64(&runs) < (uint64(expected) - runsErr))) {
			t.Errorf("%d timers did not execute ( %d/%d) [n=%d], ends %d"+
				" expire %s wait %s, re-add: %v ; re-add0: %v, expk %v"+
				" acceptable err %d\n",
				uint64(expected)-runs, runs, expected, n, end, expire, wait,
				readd, readd0, expk, runsErr)
		}
		for k := 0; k < int(n); k++ {
			ok, err := wt.DelWait(&timers[k])
			if !ok && flags&FgoR == 0 {
				t.Fatalf("failed to remove timer %d after expire: %q\n",
					k, err)
			}
			if readd[k] != 0 {
				// a 0 timer will terminate and cannot be reset, must be
				// re-init
				if err := wt.Reset(&timers[k], flags); err != nil {
					t.Fatalf("Reset timer %d  sleep %s failed %q\n",
						k, readd[k], err)
				}
			}
		}
	}
	wt.Shutdown()
}
