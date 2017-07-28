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
	"github.com/percona/qan-agent/pct/cmd"
)

type CmdFactory struct {
	Cmds []*MockCmd
}

func (f *CmdFactory) Make(name string, args ...string) cmd.Cmd {
	cmd := NewMockCmd(name, args...)
	f.Cmds = append(f.Cmds, cmd)
	return cmd
}

type MockCmd struct {
	Name string
	Args []string
	// --
	RunOutput string
	RunErr    error
}

func NewMockCmd(name string, args ...string) *MockCmd {
	return &MockCmd{
		Name: name,
		Args: args,
	}
}

func (c *MockCmd) Run() (output string, err error) {
	return c.RunOutput, c.RunErr
}
