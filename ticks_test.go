package wtimer

import (
	"math/rand"
	"os"
	"testing"
	"time"
	"unsafe"
)

var seed int64

func TestMain(m *testing.M) {
	seed = time.Now().UnixNano()
	rand.Seed(seed)
	res := m.Run()
	os.Exit(res)
}

func TestTicksConst(t *testing.T) {
	var ticks Ticks
	if TicksBits > unsafe.Sizeof(ticks.v)*8 {
		t.Fatalf("bad TicksBits constant, too big\n")
	}
	if TicksBits < 16 {
		t.Fatalf("bad TicksBits constant, too small\n")
	}
	if MaxTicksDiff == 0 || (MaxTicksDiff&(MaxTicksDiff-1) != 0) {
		t.Fatalf("wrong MaxTicksDiff 0x%x, should be 2^k\n", MaxTicksDiff)
	}
	if ((TicksMask+1)&TicksMask) != 0 ||
		(MaxTicksDiff-1)&TicksMask != (MaxTicksDiff-1) ||
		MaxTicksDiff&TicksMask != MaxTicksDiff {
		t.Fatalf("wrong TicksMask 0x%x\n", TicksMask)
	}
}

func tstOp(t *testing.T, p string, v1, v2 uint64) {
	t1 := NewTicks(v1)
	t2 := NewTicks(v2)

	if !((t1.Val() == v1) == (v1 <= TicksMask)) {
		t.Errorf(p+"Val for 0x%x (mask 0x%x) => 0x%x failed\n",
			v1, TicksMask, t1.Val())
	}
	if !((t2.Val() == v2) == (v2 <= TicksMask)) {
		t.Errorf(p+"Val for 0x%x (mask 0x%x) => 0x%x failed\n",
			v2, TicksMask, t2.Val())
	}

	if t1.EQ(t2) != ((v1 & TicksMask) == (v2 & TicksMask)) {
		t.Errorf(p+"EQ for 0x%x <> 0x%x failed (0x%x, 0x%x)\n",
			t1.Val(), t2.Val(), v1, v2)
	}
	if v1 == v2 && !t1.EQ(t2) {
		t.Errorf(p+"EQ2 for 0x%x <> 0x%x failed (0x%x, 0x%x)\n",
			t1.Val(), t2.Val(), v1, v2)
	}
	if ((v1 >= v2) && ((v1 - v2) < MaxTicksDiff)) ||
		((v1 < v2) && ((v2 - v1) < MaxTicksDiff)) {
		// as long as abs(v1-v2) is not bigger then MaxTicksDiff
		if t1.NE(t2) != (v1 != v2) {
			t.Errorf(p+"NE for 0x%x <> 0x%x failed (0x%x, 0x%x)\n",
				t1.Val(), t2.Val(), v1, v2)
		}
		if t1.LT(t2) != (v1 < v2) {
			t.Errorf(p+"LT for 0x%x <> 0x%x failed (0x%x, 0x%x)\n",
				t1.Val(), t2.Val(), v1, v2)
		}
		if t1.LE(t2) != (v1 <= v2) {
			t.Errorf(p+"LE for 0x%x <> 0x%x failed (0x%x, 0x%x)\n",
				t1.Val(), t2.Val(), v1, v2)
		}
		if t1.GT(t2) != (v1 > v2) {
			t.Errorf(p+"GT for 0x%x <> 0x%x failed (0x%x, 0x%x)\n",
				t1.Val(), t2.Val(), v1, v2)
		}
		if t1.GE(t2) != (v1 >= v2) {
			t.Errorf(p+"GE for 0x%x <> 0x%x failed (0x%x, 0x%x) v1 GE v2 %v diff 0x%x (%d) t1 - t2 = 0x%x  mask = 0x%x\n",
				t1.Val(), t2.Val(), v1, v2,
				v1 >= v2, v1-v2, v1-v2, t1.Val()-t2.Val(), TicksMask)
		}
		if t1.Add(t2).NE(NewTicks(v1 + v2)) {
			t.Errorf(p+"Add for 0x%x <> 0x%x failed (0x%x, 0x%x)\n",
				t1.Val(), t2.Val(), v1, v2)
		}
		if t1.Sub(t2).NE(NewTicks(v1 - v2)) {
			t.Errorf(p+"Sub for 0x%x <> 0x%x failed (0x%x, 0x%x)\n",
				t1.Val(), t2.Val(), v1, v2)
		}
		if t1.AddUint64(v2).NE(NewTicks(v1 + v2)) {
			t.Errorf(p+"AddUint64 for 0x%x <> 0x%x failed (0x%x, 0x%x)\n",
				t1.Val(), t2.Val(), v1, v2)
		}
		if t1.SubUint64(v2).NE(NewTicks(v1 - v2)) {
			t.Errorf(p+"SubUint64 for 0x%x <> 0x%x failed (0x%x, 0x%x)\n",
				t1.Val(), t2.Val(), v1, v2)
		}
	}
}

func TestTicksOps(t *testing.T) {
	const iterations = 100000
	tstOp(t, "", 1, 2)
	tstOp(t, "", 4, 3)
	tstOp(t, "", MaxTicksDiff-1, 1)
	tstOp(t, "", 1, MaxTicksDiff-1)
	tstOp(t, "", MaxTicksDiff-1, MaxTicksDiff-2)
	tstOp(t, "", MaxTicksDiff-2, MaxTicksDiff-1)
	tstOp(t, "", MaxTicksDiff, 0)
	tstOp(t, "", MaxTicksDiff+1, MaxTicksDiff+2)
	tstOp(t, "", MaxTicksDiff+4, MaxTicksDiff+3)

	for i := 0; i < iterations; i++ {
		v1 := uint64(rand.Int63())
		diff := uint64(rand.Int63n(MaxTicksDiff))
		tstOp(t, "rand+: ", v1, v1+diff)
		tstOp(t, "rand-: ", v1, v1-diff)
	}
	for i := 0; i < iterations; i++ {
		v1 := uint64(rand.Int63())
		v2 := uint64(rand.Int63())
		tstOp(t, "rand2: ", v1, v2)
	}
}
