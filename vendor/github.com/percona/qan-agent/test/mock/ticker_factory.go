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
	"github.com/percona/qan-agent/ticker"
)

type TickerFactory struct {
	tickers  []ticker.Ticker
	tickerNo int
	Made     []uint
}

func NewTickerFactory() *TickerFactory {
	tf := &TickerFactory{
		Made: []uint{},
	}
	return tf
}

func (tf *TickerFactory) Make(atInterval uint, sync bool) ticker.Ticker {
	tf.Made = append(tf.Made, atInterval)
	if tf.tickerNo > len(tf.tickers) {
		return tf.tickers[tf.tickerNo-1]
	}
	nextTicker := tf.tickers[tf.tickerNo]
	tf.tickerNo++
	return nextTicker
}

func (tf *TickerFactory) Set(tickers []ticker.Ticker) {
	tf.tickerNo = 0
	tf.tickers = tickers
	tf.Made = []uint{}
}
