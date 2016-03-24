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

package main

import (
	"flag"
	"log"
	"os"
	"os/exec"
	"io/ioutil"
	"os/signal"
	"path/filepath"
	"syscall"
	"strings"
	"time"

	"gopkg.in/yaml.v2"
)

type Exporter struct {
	Name     string     `yaml:"name"`
	Args     []string   `yaml:"args"`
	ErrChan  chan error `yaml:"-"`
	ErrCount uint       `yaml:"-"`
}

var exporters []*Exporter

const (
	BIN             = "percona-metrics"
	DEFAULT_BASEDIR = "/usr/local/percona/pmm-client"
	MAX_ERRORS      = 3
)

var (
	flagBasedir string
)

func init() {
	flag.StringVar(&flagBasedir, "basedir", DEFAULT_BASEDIR, "pmm-client basedir")
	flag.Parse()
}

var stopChan chan struct{}

func main() {
	log.Println(BIN + " running")

	if err := os.Chdir(flagBasedir); err != nil {
		log.Fatal(err)
	}

	bytes, err := ioutil.ReadFile(filepath.Join(flagBasedir, "exporters.yml"))
	if err != nil {
		log.Fatal(err)
	}
	if err := yaml.Unmarshal(bytes, &exporters); err != nil {
		log.Fatal(err)
	}

	stopChan := make(chan struct{})

	for _, e := range exporters {
		e.ErrChan = make(chan error, 1)
		go run(e)
	}

	sigTermChan := make(chan os.Signal, 1)
	signal.Notify(sigTermChan, os.Interrupt, syscall.SIGTERM)

RESPAWN_LOOP:
	for {
		select {
		case err := <-exporters[0].ErrChan:
			respawn(exporters[0], err)
		case <-sigTermChan:
			log.Println("Caught signal, terminating...")
			break RESPAWN_LOOP
		}
	}

	close(stopChan)

	timeout := time.After(1 * time.Second)
TIMEOUT_LOOP:
	for {
		select {
		case err := <-exporters[0].ErrChan:
			log.Printf(exporters[0].Name+" stopped: %s", err)
		case <-timeout:
			break TIMEOUT_LOOP
		case <-sigTermChan:
			log.Println("Caught signal, terminating...")
			break TIMEOUT_LOOP
		}
	}

	log.Println(BIN + " stopped")
}

func run(e *Exporter) {
	logFileName, _ := filepath.Abs(filepath.Join(flagBasedir, e.Name+".log"))
	logFile, err := os.OpenFile(logFileName, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0666)
	if err != nil {
		log.Fatal(err)
	}
	defer logFile.Close()

	bin, _ := filepath.Abs(filepath.Join(flagBasedir, e.Name))
	cmd := exec.Command(bin, e.Args...)
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	runErrChan := make(chan error, 1)
	go func() {
		runErrChan <- cmd.Run()
	}()

	log.Printf("Started %s %s", e.Name, strings.Join(e.Args, " "))

	select {
	case err := <-runErrChan:
		e.ErrChan <- err
	case <-stopChan:
		e.ErrChan <- cmd.Process.Kill()
	}
}

func respawn(e *Exporter, err error) {
	log.Printf(e.Name+" stopped: %s", err)
	e.ErrCount++
	if e.ErrCount < MAX_ERRORS {
		time.Sleep(1 * time.Second)
		go run(e)
	} else {
		log.Printf("Not restarting %s because it has failed too mamy times", e.Name)
	}
}
