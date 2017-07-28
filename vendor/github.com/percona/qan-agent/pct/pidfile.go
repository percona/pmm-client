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

package pct

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

type PidFile struct {
	name string
	mux  *sync.RWMutex
}

func NewPidFile() *PidFile {
	p := &PidFile{
		mux: new(sync.RWMutex),
	}
	return p
}

func (p *PidFile) Get() string {
	p.mux.RLock()
	defer p.mux.RUnlock()
	return p.name
}

func (p *PidFile) Set(pidFile string) error {
	/**
	 * Get new PID file _then_ remove old, i.e. don't give up current PID file
	 * until new PID file is secured.  If no new PID file, then just remove old
	 * (if any) and return early.
	 */
	p.mux.Lock()
	defer p.mux.Unlock()

	// Return early if no new PID file.
	if pidFile == "" {
		// Remove existing PID file if any.  Do NOT call Remove() because it locks.
		if err := p.remove(); err != nil {
			return err
		}
		return nil
	}

	// Two kind of pidFile values are accepted.
	// User provided an pidFile name with and absolute path that is equal to basedir.
	// User provided relative path that has no path whatsoever.
	// Any other case should return an error.
	if filepath.IsAbs(pidFile) {
		if filepath.Dir(pidFile) != Basedir.Path() {
			return errors.New("absolute pidfile path should be equals to basedir")
		}
	} else {
		if filepath.Dir(pidFile) != "." {
			return errors.New("relative pidfile should not contain any paths")
		}
		pidFile = filepath.Join(Basedir.Path(), pidFile)
	}

	// Create new PID file, success only if it doesn't already exist.
	flags := os.O_CREATE | os.O_EXCL | os.O_WRONLY
	file, err := os.OpenFile(pidFile, flags, 0644)
	if err != nil {
		return err
	}

	// Write PID to new PID file and close.
	if _, err := file.WriteString(fmt.Sprintf("%d\n", os.Getpid())); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}

	// Remove old PID file if any.  Do NOT call Remove() because it locks.
	if err := p.remove(); err != nil {
		// If this happens we're in a weird state: holding both PID files.
		return err
	}

	// Success: new PID file set, old removed.
	p.name = pidFile
	return nil
}

func (p *PidFile) Remove() error {
	p.mux.Lock()
	defer p.mux.Unlock()
	return p.remove()
}

func (p *PidFile) remove() error {
	// Do NOT lock here.  Expect caller to lock.
	if p.name == "" {
		return nil
	}
	if err := os.Remove(p.name); err != nil && !os.IsNotExist(err) {
		return err
	}
	p.name = ""
	return nil
}
