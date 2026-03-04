// Copyright 2026 Frafos GmbH. All rights reserved.
//
// Use of this source code is governed by a BSD-style license
// that can be found in the LICENSE.txt file in the root of the source
// tree.

package wtimer

// lTimerLst is a circular double-linked list for LTimerLnk entries.
// No locking, no atomic operations.
type lTimerLst struct {
	head     LTimerLnk // used only as list head (only next & prev)
	wheelNo  uint8
	wheelIdx uint16
}

func (lst *lTimerLst) init(wheelNo uint8, wheelIdx uint16) {
	lst.forceEmpty()
	lst.wheelNo = wheelNo
	lst.wheelIdx = wheelIdx
	lst.head.info.setFlags(fHead)
	lst.head.info.setWheel(wheelNo, wheelIdx)
}

func (lst *lTimerLst) forceEmpty() {
	lst.head.next = &lst.head
	lst.head.prev = &lst.head
}

func (lst *lTimerLst) isEmpty() bool {
	return lst.head.next == &lst.head
}

func (lst *lTimerLst) insert(e *LTimerLnk) {
	if !lIsDetached(e) {
		w, idx := e.info.wheelPos()
		PANIC("lTimerLst insert called on an entry not detached: "+
			" t wheel %d idx %d , lst wheel %d idx %d next %p prev %p\n",
			w, idx, lst.wheelNo, lst.wheelIdx,
			e.next, e.prev)
	}

	e.prev = &lst.head
	e.next = lst.head.next
	e.next.prev = e
	lst.head.next = e

	// DBG checks:
	w, idx := e.info.wheelPos()
	if w != wheelNone || idx != wheelNoIdx {
		PANIC("lTimerLst insert called on an entry already on a diff. list: "+
			" t wheel %d idx %d , lst wheel %d idx %d\n",
			w, idx, lst.wheelNo, lst.wheelIdx)
	}
	e.info.setWheel(lst.wheelNo, lst.wheelIdx)
}

func (lst *lTimerLst) append(e *LTimerLnk) {
	// DBG checks:
	if !lIsDetached(e) {
		w, idx := e.info.wheelPos()
		PANIC("lTimerLst append called on an entry not detached: "+
			" t wheel %d idx %d , lst wheel %d idx %d next %p prev %p\n",
			w, idx, lst.wheelNo, lst.wheelIdx,
			e.next, e.prev)
	}

	e.prev = lst.head.prev
	e.next = &lst.head
	e.prev.next = e
	lst.head.prev = e

	// DBG checks:
	w, idx := e.info.wheelPos()
	if w != wheelNone || idx != wheelNoIdx {
		PANIC("lTimerLst append called on an entry already on a diff. "+
			"list: t wheel %d idx %d , lst wheel %d idx %d\n",
			w, idx, lst.wheelNo, lst.wheelIdx)
	}
	e.info.setWheel(lst.wheelNo, lst.wheelIdx)
}

func (lst *lTimerLst) rm(e *LTimerLnk) {
	if e == nil || e.next == nil || e.prev == nil {
		PANIC("called with nil-detached element %p\n", e)
	}
	if e.next == e || e.prev == e {
		if e == &lst.head {
			PANIC("trying to rm list head %p\n", e)
		} else {
			PANIC("called with detached element %p:"+
				" expire %s intvl %s %s\n",
				e, e.expire, e.intvl, e.info)
		}
	}
	e.prev.next = e.next
	e.next.prev = e.prev
	e.next = e
	e.prev = e

	// DBG checks:
	w, idx := e.info.wheelPos()
	if w != lst.wheelNo || idx != lst.wheelIdx {
		PANIC("lTimerLst rm called on an entry from a different list: "+
			" t wheel %d idx %d , lst wheel %d idx %d\n",
			w, idx, lst.wheelNo, lst.wheelIdx)
	}
	e.info.setWheel(wheelNone, wheelNoIdx)
}

// rmSubList removes a sub list defined by all the elements between
// s & e (including s & e). It returns a pointer to the detached
// sub list (which will still be a circular list) or nil if the sub list
// is empty.
// Note: s & e must be different from lst (from the list head address).
// Examples:
//   - detach the entire list:  l := lst.rmSubList(lst.next, lst.prev)
//   - detach from start to e:  l := lst.rmSubList(lst.next, e)
//   - detach from e to end:    l := lst.rmSubList(e, lst.prev)
func (lst *lTimerLst) rmSubList(s, e *LTimerLnk) *LTimerLnk {
	if e == nil || e.next == nil || e.prev == nil {
		PANIC("called with nil-detached element %p\n", e)
	}
	if e.next == e || e.prev == e {
		if e != &lst.head {
			PANIC("called with detached element %p\n", e)
		}
	}
	if s == nil || s.next == nil || s.prev == nil {
		PANIC("called with nil-detached element %p\n", s)
	}
	if s.next == s || s.prev == s {
		if s != &lst.head {
			PANIC("called with detached element %p\n", s)
		}
	}

	if s == &lst.head || e == &lst.head {
		return nil // empty list or &head passed as parameters (wrong)
	}
	// detach
	s.prev.next = e.next
	e.next.prev = s.prev
	// make the detached part circular
	s.prev = e
	e.next = s

	// debugging: mark all elements as detached
	for v := s; v != e; v = v.next {
		v.info.setWheel(wheelNone, wheelNoIdx)
	}
	e.info.setWheel(wheelNone, wheelNoIdx)

	return s
}

// insertSubList adds an entire sublist specified by the starting and
// ending element (s & e) to the beginning of lst (immediately after lst.head).
func (lst *lTimerLst) insertSubList(s, e *LTimerLnk) {
	s.prev = &lst.head
	e.next = lst.head.next
	lst.head.next.prev = e
	lst.head.next = s

	for v := s; v != e; v = v.next {
		v.info.setWheel(lst.wheelNo, lst.wheelIdx)
	}
	e.info.setWheel(lst.wheelNo, lst.wheelIdx)
}

// appendSubList adds an entire sublist specified by the starting and
// ending element (s & e) at the end of lst (immediately before lst.head).
func (lst *lTimerLst) appendSubList(s, e *LTimerLnk) {
	s.prev = lst.head.prev
	e.next = &lst.head
	lst.head.prev.next = s
	lst.head.prev = e

	for v := s; v != e; v = v.next {
		v.info.setWheel(lst.wheelNo, lst.wheelIdx)
	}
	e.info.setWheel(lst.wheelNo, lst.wheelIdx)
}

// mv moves all the elements of the current lst to the end of dst.
// Returns true if any elements were moved.
func (lst *lTimerLst) mv(dst *lTimerLst) bool {
	s := lst.head.next
	e := lst.head.prev
	if lst.rmSubList(s, e) == nil {
		return false
	}
	dst.appendSubList(s, e)
	return true
}

// lWheel is a non-atomic version of wheel for LTimer.
type lWheel struct {
	no   uint8
	lsts []lTimerLst
}

func (w *lWheel) init(n uint8, lists []lTimerLst) {
	w.no = n
	w.lsts = lists
	for i := 0; i < len(w.lsts); i++ {
		w.lsts[i].init(w.no, uint16(i))
	}
}
