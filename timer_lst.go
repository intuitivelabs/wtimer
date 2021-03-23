// Copyright 2021 Intuitive Labs GmbH. All rights reserved.
//
// Use of this source code is governed by a BSD-style license
// that can be found in the LICENSE.txt file in the root of the source
// tree.

package wtimer

import ()

type timerLst struct {
	head     TimerLnk // used only as list head (only next & prev)
	wheelNo  uint8    // mostly for debugging
	wheelIdx uint16
}

// init initialises a list head (circular list).
func (lst *timerLst) init(wheelNo uint8, wheelIdx uint16) {
	lst.forceEmpty()
	lst.wheelNo = wheelNo
	lst.wheelIdx = wheelIdx
	lst.head.info.setFlags(fHead)
	lst.head.info.setWheel(wheelNo, wheelIdx)
}

// forceEmpty will completely empty the list (re-init the list head).
func (lst *timerLst) forceEmpty() {
	lst.head.next = &lst.head
	lst.head.prev = &lst.head
}

// isEmpty returns true if the list is empty.
func (lst *timerLst) isEmpty() bool {
	return lst.head.next == &lst.head
}

// insert adds a new TimerLnk entry to the list.
// There's no internal locking.
func (lst *timerLst) insert(e *TimerLnk) {
	// DBG checks:
	if !isDetached(e) {
		w, idx := e.info.wheelPos()
		PANIC("timerLst insert called on an entry not detached: "+
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
		PANIC("timerLst insert called on an entry already on a diff. list: "+
			" t wheel %d idx %d , lst wheel %d idx %d\n",
			w, idx, lst.wheelNo, lst.wheelIdx)
	}
	e.info.setWheel(lst.wheelNo, lst.wheelIdx)
}

// appends adds a TimerLnk entry at the end of the list
// There's no internal locking.
func (lst *timerLst) append(e *TimerLnk) {
	// DBG checks:
	if !isDetached(e) {
		w, idx := e.info.wheelPos()
		PANIC("timerLst append called on an entry not detached: "+
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
		PANIC("timerLst insert called on an entry already on a diff. list: "+
			" t wheel %d idx %d , lst wheel %d idx %d\n",
			w, idx, lst.wheelNo, lst.wheelIdx)
	}
	e.info.setWheel(lst.wheelNo, lst.wheelIdx)
}

// rm removes a TimerLnk entry from the list.
// There's no internal locking.
func (lst *timerLst) rm(e *TimerLnk) {
	if e == nil || e.next == nil || e.prev == nil {
		PANIC("called with nil-detached element %p\n", e)
	}
	if e.next == e || e.prev == e {
		if e == &lst.head {
			PANIC("trying to rm list head  %p\n", e)
		} else {
			PANIC("called with detached element %p:"+
				" expire %s intvl %s %s\n",
				e, e.expire, e.intvl, e.info)
		}
	}
	e.prev.next = e.next
	e.next.prev = e.prev
	// "mark" e as detached
	e.next = e
	e.prev = e

	// DBG checks:
	w, idx := e.info.wheelPos()
	if w != lst.wheelNo || idx != lst.wheelIdx {
		PANIC("timerLst rm called on an entry from a different list: "+
			" t wheel %d idx %d , lst wheel %d idx %d\n",
			w, idx, lst.wheelNo, lst.wheelIdx)
	}
	e.info.setWheel(wheelNone, wheelNoIdx)
}

// rmSubList removes a sub list defined by all the elements between
//  s & e (including s & e). It returns a pointer to the detached
// sub list (which will still be a circular list) or nil if the sub list
// is empty.
// Note: s & e must be different from lst (from the list head address).
// Examples:
//   - detach the entire list:  l := lst.rmSubList(lst.next, lst.prev)
//   - detach from start to e:  l := lst.rmSubList(lst.next, e)
//   - detach from e to end:    l := lst.rmSubList(e, lst.prev)
func (lst *timerLst) rmSubList(s, e *TimerLnk) *TimerLnk {
	if e == nil || e.next == nil || e.prev == nil {
		PANIC("called with nil-detached element %p\n", e)
	}
	if e.next == e || e.prev == e {
		if e != &lst.head {
			PANIC("called with detached element %p\n", e)
		}
	}
	if s == nil || s.next == nil || s.prev == nil {
		PANIC("called with nil-detached element %p\n", e)
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
	// (useful for debugging)
	for v := s; v != e; v = v.next {
		v.info.setWheel(wheelNone, wheelNoIdx)
	}
	e.info.setWheel(wheelNone, wheelNoIdx)

	return s
}

// appendSubList adds an entire sublist specified by the starting and
// ending element (s & e) to the beginning of lst (immediately after lst.head).
func (lst *timerLst) insertSubList(s, e *TimerLnk) {
	s.prev = &lst.head
	e.next = lst.head.next
	lst.head.next.prev = e
	lst.head.next = s

	//  mark all elements as detached
	// (useful for quickly finding the parent wheel, e.g. for locking the
	// right list)
	for v := s; v != e; v = v.next {
		v.info.setWheel(lst.wheelNo, lst.wheelIdx)
	}
	e.info.setWheel(lst.wheelNo, lst.wheelIdx)
}

// appendSubList adds an entire sublist specified by the starting and
// ending element (s & e) at the end of lst (immediately after lst.head.prev).
func (lst *timerLst) appendSubList(s, e *TimerLnk) {
	s.prev = lst.head.prev
	e.next = &lst.head
	lst.head.prev.next = s
	lst.head.prev = e

	// mark all elements as belonging to this list: wheel & idx
	// (useful for quickly finding the parent wheel, for proper locking a.s.o)
	for v := s; v != e; v = v.next {
		v.info.setWheel(lst.wheelNo, lst.wheelIdx)
	}
	e.info.setWheel(lst.wheelNo, lst.wheelIdx)
}

// mv moves all the elements of the current lst to the end of dst.
// Returns true if any elements where moved
func (lst *timerLst) mv(dst *timerLst) bool {
	s := lst.head.next
	e := lst.head.prev
	if lst.rmSubList(s, e) == nil {
		return false
	}
	dst.appendSubList(s, e)
	return true
}

// forEach iterates on the entire list calling f(e) for each element.
// It stops immediately if  f() returns false.
// WARNING: it does not support removing the current list element
// from f(), use forEachSafeRm() for that.
func (lst *timerLst) forEach(f func(e *TimerLnk) bool) {
	cont := true
	for v := lst.head.next; v != &lst.head && cont; v = v.next {
		cont = f(v)
	}
}

// forEachSafeRm is similar to forEach(), but supports removing the
// current list elements from the callback function (e).
// It does not support removing other lists elements (e.g. e->next).
func (lst *timerLst) forEachSafeRm(f func(l *timerLst, e *TimerLnk) bool) {
	cont := true
	s := lst.head.next
	for v, nxt := s, s.next; v != &lst.head && cont; v, nxt = nxt, nxt.next {
		cont = f(lst, v)
	}
}

// detached check if the TimerLnk entry is part of a list and returns true
// if not.
func isDetached(e *TimerLnk) bool {
	return e.Detached()
}
