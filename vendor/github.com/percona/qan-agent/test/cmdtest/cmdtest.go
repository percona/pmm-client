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

type CmdTest struct {
	reader io.Reader
	stdin  io.Writer
	output <-chan string
}

func NewCmdTest(cmd *exec.Cmd) *CmdTest {
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
	cmdOutput.output = cmdOutput.Run()
	return cmdOutput
}

func (c *CmdTest) Run() <-chan string {
	output := make(chan string, 1024)
	go func() {
		for {
			b := make([]byte, 8192)
			n, err := c.reader.Read(b)
			if n > 0 {
				lines := bytes.SplitAfter(b[:n], []byte("\n"))
				// Example: Split(a\nb\n\c\n) => ["a\n", "b\n", "c\n", ""]
				// We are getting empty element because data for split was ending with delimiter (\n)
				// We don't want it, so we remove it
				lastPos := len(lines) - 1
				if len(lines[lastPos]) == 0 {
					lines = lines[:lastPos]
				}
				for i := range lines {
					line := string(lines[i])
					//log.Printf("%#v", line)
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

func (c *CmdTest) ReadLine() (line string) {
	select {
	case line = <-c.output:
	case <-time.After(400 * time.Millisecond):
	}
	return line
}

func (c *CmdTest) Write(data string) {
	_, err := c.stdin.Write([]byte(data))
	if err != nil {
		panic(err)
	}
}

func (c *CmdTest) Output() []string {
	lines := []string{}
OUTPUT_LINES:
	for {
		if line := c.ReadLine(); line != "" {
			lines = append(lines, line)
		} else {
			break OUTPUT_LINES
		}
	}
	return lines
}
