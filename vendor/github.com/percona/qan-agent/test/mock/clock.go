/*
   Copyright (c) 2016, Percona LLC and/or its affiliates. All rights reserved.

   This program is free software: you can redistribute it and/or modify
   it under the terms of the GNU Affero General Public License as published by
   the Free Software Foundation, either version 3 of the License, or
   (at your option) any later version.

   This program is distributed in the hope that it will be useful,
   but WITHOUT ANY WARRANTY; without even the implied warranty of
   MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
   GNU Affero General Public License for more details.

   You should have received a copy of the GNU Affero General Public License
   along with this program.  If not, see <http://www.gnu.org/licenses/>
*/

package mock

import (
	"time"
)

type Clock struct {
	Added   []uint
	Removed []chan time.Time
	Eta     float64
}

func NewClock() *Clock {
	m := &Clock{
		Added:   []uint{},
		Removed: []chan time.Time{},
	}
	return m
}

func (m *Clock) Add(c chan time.Time, t uint, sync bool) {
	m.Added = append(m.Added, t)
}

func (m *Clock) Remove(c chan time.Time) {
	m.Removed = append(m.Removed, c)
}

func (m *Clock) ETA(c chan time.Time) float64 {
	return m.Eta
}
