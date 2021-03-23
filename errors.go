// Copyright 2021 Intuitive Labs GmbH. All rights reserved.
//
// Use of this source code is governed by a BSD-style license
// that can be found in the LICENSE.txt file in the root of the source
// tree.

package wtimer

import (
	"errors"
)

var ErrInactiveTimer = errors.New("called on inactive timer")
var ErrNotResetTimer = errors.New("called on not reset/init timer")
var ErrActiveTimer = errors.New("called on active timer")
var ErrRunningTimer = errors.New("called on running timer")
var ErrDeletedTimer = errors.New("called on already delete-marked timer")
var ErrAlreadyRemovedTimer = errors.New("called on already removed timer")
var ErrInvalidTimer = errors.New("called on invalid timer handler")
var ErrTicksTooHigh = errors.New("ticks delta too high")
var ErrDurationTooSmall = errors.New("duration smaller then tick")
var ErrInvalidParameters = errors.New("invalid parameters")
