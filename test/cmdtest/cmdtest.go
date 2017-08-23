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

package cmdtest

import (
	"bytes"
	"io"
	"os/exec"
	"time"
)

// CmdTest simplifies interacting with running cmd.
// You get most of it if cmd is long running, with real-time line by line output to stdin
// or if cmd is interactive so you need to make decisions depending on the provided output.
type CmdTest struct {
	reader io.Reader
	stdin  io.Writer
	output <-chan string
}

// New wraps *exec.Cmd in *CmdTest
func New(cmd *exec.Cmd) *CmdTest {
	stdin, err := cmd.StdinPipe()
	if err != nil {
		panic(err)
	}
	pipeReader, pipeWriter := io.Pipe()
	cmd.Stdout = pipeWriter
	cmd.Stderr = pipeWriter

	cmdOutput := &CmdTest{
		reader: pipeReader,
		stdin:  stdin,
	}
	cmdOutput.output = cmdOutput.run()
	return cmdOutput
}

// ReadLine reads one line from cmd stdout + stderr or empty string
func (c *CmdTest) ReadLine() (line string) {
	select {
	case line = <-c.output:
	case <-time.After(200 * time.Millisecond):
	}
	return line
}

// Write data to stdin
func (c *CmdTest) Write(data string) {
	_, err := c.stdin.Write([]byte(data))
	if err != nil {
		panic(err)
	}
}

// Output returns cmd output that was not read yet
func (c *CmdTest) Output() []string {
	lines := []string{}

	for {
		line := c.ReadLine()
		if line == "" {
			break
		}
		lines = append(lines, line)
	}

	return lines
}

// run the output parser and return channel with combined stdout + stderr
func (c *CmdTest) run() <-chan string {
	output := make(chan string, 1024)
	go func() {
		for {
			// Line doesn't end with "\n" with interactive terminals
			// e.g. "Do you want to start nuclear war? [y/n]: "
			// So we need to read a line if it either ends with "\n" or not.
			// This can't be achieved with `reader.ReadString('\n')` as it will block until
			// it encounters "\n" or stream gets closed.
			b := make([]byte, 8192)
			n, err := c.reader.Read(b)
			if n > 0 {
				lines := bytes.SplitAfter(b[:n], []byte("\n"))
				// Example: SplitAfter("a\nb\n\c\n", "\n") => ["a\n", "b\n", "c\n", ""]
				// We are getting empty element because data for split was ending with delimiter (\n)
				// We don't want it, so we remove it
				lastPos := len(lines) - 1
				if len(lines[lastPos]) == 0 {
					lines = lines[:lastPos]
				}

				for i := range lines {
					line := string(lines[i])
					output <- line
				}
			}
			if err != nil {
				break
			}
		}
	}()
	return output
}
