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

	"github.com/percona/qan-agent/mysql"
	"github.com/percona/qan-agent/qan"
	"github.com/percona/pmm/proto"
	pc "github.com/percona/pmm/proto/config"
)

type QanAnalyzer struct {
	StartChan chan bool
	StopChan  chan bool
	ErrorChan chan error
	CrashChan chan bool
	config    pc.QAN
	name      string
}

func NewQanAnalyzer(name string) *QanAnalyzer {
	a := &QanAnalyzer{
		StartChan: make(chan bool, 1),
		StopChan:  make(chan bool, 1),
		ErrorChan: make(chan error, 1),
		CrashChan: make(chan bool, 1),
		config:    pc.QAN{},
		name:      name,
	}
	return a
}

func (a *QanAnalyzer) Start() error {
	a.StartChan <- true
	return a.crashOrError()
}

func (a *QanAnalyzer) Stop() error {
	a.StopChan <- true
	return a.crashOrError()
}

func (a *QanAnalyzer) Status() map[string]string {
	return map[string]string{
		"qan-analyzer": "ok",
	}
}

func (a *QanAnalyzer) String() string {
	return a.name
}

func (a *QanAnalyzer) Config() pc.QAN {
	return a.config
}

func (a *QanAnalyzer) SetConfig(config pc.QAN) {
	a.config = config
}

// --------------------------------------------------------------------------

func (a *QanAnalyzer) crashOrError() error {
	select {
	case <-a.CrashChan:
		panic("mock.QanAnalyzer crash")
	default:
	}
	select {
	case err := <-a.ErrorChan:
		return err
	default:
	}
	return nil
}

/////////////////////////////////////////////////////////////////////////////
// Factory
/////////////////////////////////////////////////////////////////////////////

type AnalyzerArgs struct {
	Config      pc.QAN
	Name        string
	MysqlConn   mysql.Connector
	RestartChan chan proto.Instance
	TickChan    chan time.Time
}

type QanAnalyzerFactory struct {
	Args      []AnalyzerArgs
	analyzers []qan.Analyzer
	n         int
}

func NewQanAnalyzerFactory(a ...qan.Analyzer) *QanAnalyzerFactory {
	f := &QanAnalyzerFactory{
		Args:      []AnalyzerArgs{},
		analyzers: a,
	}
	return f
}

func (f *QanAnalyzerFactory) Make(
	config pc.QAN,
	name string,
	mysqlConn mysql.Connector,
	restartChan chan proto.Instance,
	tickChan chan time.Time,
) qan.Analyzer {
	if f.n < len(f.analyzers) {
		a := f.analyzers[f.n]
		// The factory is supposed to provide the config as an initialization
		// parameter for the created qan.Analizer but since we are mocking
		// and need to create the analyzer and pass it to the factory first,
		// we just set the config here.
		a.SetConfig(config)
		f.n++
		args := AnalyzerArgs{
			Config:      config,
			Name:        name,
			MysqlConn:   mysqlConn,
			RestartChan: restartChan,
			TickChan:    tickChan,
		}
		f.Args = append(f.Args, args)
		return a
	}
	panic("Need more analyzers")
}
