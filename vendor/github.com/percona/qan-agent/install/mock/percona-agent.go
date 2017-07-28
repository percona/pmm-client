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

// Mocking percona-agent only for init script testing purposes.
// init script relies on /proc fs to determine if the binary running
// under pidfile corresponds to a percona-agent.
// This partial mock service takes two cmd line args, mandatory basedir and
// optional pidfile. The program will just create the PID file and loop forever.
//
// Fail Exit status codes:
// 2 - User did not provide basedir flag
// 3 - Could not create pidfile
// 4 - Could not write to pidfile
// 5 - Could not close pidfile

package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

var (
	flagBasedir string
	flagPidFile string
)

func init() {
	flag.StringVar(&flagBasedir, "basedir", "", "Agent basedir")
	flag.StringVar(&flagPidFile, "pidfile", "percona-agent.pid", "PID file")
	flag.Parse()
}

func main() {

	if err := flag.CommandLine.Parse(os.Args[1:]); err != nil {
		os.Exit(10)
	}

	if flagBasedir == "" {
		os.Exit(2)
	}

	startDelayEnv := os.Getenv("PCT_TEST_START_DELAY")
	startDelay, err := strconv.ParseFloat(startDelayEnv, 32)
	if err != nil {
		startDelay = 0
	}

	time.Sleep(time.Second * time.Duration(startDelay))

	if !filepath.IsAbs(flagPidFile) {
		flagPidFile = filepath.Join(flagBasedir, flagPidFile)
	}

	// Create new PID file, success only if it doesn't already exist.
	flags := os.O_CREATE | os.O_EXCL | os.O_WRONLY
	file, err := os.OpenFile(flagPidFile, flags, 0644)
	if err != nil {
		os.Exit(3)
	}

	// Write PID to new PID file and close.
	if _, err := file.WriteString(fmt.Sprintf("%d\n", os.Getpid())); err != nil {
		os.Exit(4)
	}
	if err := file.Close(); err != nil {
		os.Exit(5)
	}

	// Wait for term/kill signal
	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc,
		syscall.SIGINT,
		syscall.SIGTERM,
		syscall.SIGQUIT)

	_ = <-sigc
	stopDelayEnv := os.Getenv("PCT_TEST_STOP_DELAY")
	stopDelay, err := strconv.ParseFloat(stopDelayEnv, 32)
	if err != nil {
		stopDelay = 0
	}
	time.Sleep(time.Second * time.Duration(stopDelay))
	os.Remove(flagPidFile)
	os.Exit(0)
}
