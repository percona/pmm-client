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

package pct_test

import (
	"fmt"
	"io/ioutil"
	"math/rand"
	"os"
	"path/filepath"

	"github.com/percona/qan-agent/pct"
	. "gopkg.in/check.v1"
)

type TestSuite struct {
	baseDir     string
	tmpDir      string
	testPidFile *pct.PidFile
	tmpFile     *os.File
}

var _ = Suite(&TestSuite{})

func (s *TestSuite) SetUpSuite(t *C) {
	// We can't/shouldn't use /usr/local/percona/ (the default basedir), so use
	// a tmpdir instead with roughly the same structure.
	basedir, err := ioutil.TempDir("", "pidfile-test-")
	t.Assert(err, IsNil)
	s.baseDir = basedir
	if err := pct.Basedir.Init(s.baseDir); err != nil {
		t.Errorf("Could initialize tmp Basedir: %v", err)
	}
	// We need and extra tmpdir for tests (!= basedir)
	tmpdir, err := ioutil.TempDir("", "pidfile-test-")
	t.Assert(err, IsNil)
	s.tmpDir = tmpdir
}

func (s *TestSuite) TearDownSuite(t *C) {
	if err := os.RemoveAll(s.baseDir); err != nil {
		t.Error(err)
	}

	if err := os.RemoveAll(s.tmpDir); err != nil {
		t.Error(err)
	}
}

func (s *TestSuite) SetUpTest(t *C) {
	s.testPidFile = pct.NewPidFile()
}

func removeTmpFile(tmpFileName string, t *C) error {
	if err := os.Remove(tmpFileName); err != nil {
		t.Logf("Could not delete tmp file: %v", err)
		return err
	}
	return nil
}

func getTmpFileName() string {
	return fmt.Sprintf("%v.pid", rand.Int())
}

func getTmpAbsFileName(tmpDir string) string {
	return filepath.Join(tmpDir, getTmpFileName())
}

// ----------------------------------------------------------------------------

func (s *TestSuite) TestGet(t *C) {
	t.Assert(s.testPidFile.Get(), Equals, "")
}

func (s *TestSuite) TestSetEmpty(t *C) {
	t.Assert(s.testPidFile.Set(""), Equals, nil)
}

func (s *TestSuite) TestSetNotExistsAbs(t *C) {
	randPidFileName := getTmpAbsFileName(s.tmpDir)
	// Set should fail, pidfile does not accept absolute paths outside basedir
	t.Check(s.testPidFile.Set(randPidFileName), NotNil)

	randPidFileName = filepath.Join(pct.Basedir.Path(), getTmpFileName())
	// Set should succeed, pidfile absolute path directory is basedir
	t.Check(s.testPidFile.Set(randPidFileName), IsNil)

	// Try to set pidfile in one directory level higher than basedir
	randPidFileName = filepath.Join(filepath.Dir(pct.Basedir.Path()), getTmpFileName())
	// Set should fail, pidfile absolute path directory is below basedir
	t.Check(s.testPidFile.Set(randPidFileName), NotNil)
}

func (s *TestSuite) TestSetNotExistsRel(t *C) {
	// Should fail, yields a pidfile outside basedir
	t.Check(s.testPidFile.Set("../percona-agent.pid"), NotNil)
	// Should pass, path resolves to "."
	t.Check(s.testPidFile.Set("./bin/../percona-agent.pid"), IsNil)

	randPidFileName := getTmpFileName()
	// Set should pass, pidfile does not exist and has no path
	t.Assert(s.testPidFile.Set(randPidFileName), Equals, nil)
}

func (s *TestSuite) TestSetExistsRel(t *C) {
	// Create pidfile and try to Set it
	tmpFile, err := ioutil.TempFile(pct.Basedir.Path(), "")
	if err != nil {
		t.Errorf("Could not create a tmp file: %v", err)
	}
	// Set should fail, pidfile exists
	t.Assert(s.testPidFile.Set(tmpFile.Name()), NotNil)
}

func (s *TestSuite) TestRemoveEmpty(t *C) {
	t.Check(s.testPidFile.Set(""), Equals, nil)
	// Remove should succeed, empty pidfile string provided
	t.Assert(s.testPidFile.Remove(), Equals, nil)
}

func (s *TestSuite) TestRemoveRel(t *C) {
	tmpFileName := getTmpFileName()
	t.Check(s.testPidFile.Set(tmpFileName), Equals, nil)
	// Remove should succeed, pidfile exists
	t.Assert(s.testPidFile.Remove(), Equals, nil)
	absFilePath := filepath.Join(pct.Basedir.Path(), tmpFileName)
	// Check if pidfile was deleted
	t.Assert(pct.FileExists(absFilePath), Equals, false)
}

func (s *TestSuite) TestRemoveNotExistsRel(t *C) {
	randPidFileName := getTmpFileName()
	t.Check(s.testPidFile.Set(randPidFileName), Equals, nil)
	absFilePath := filepath.Join(pct.Basedir.Path(), randPidFileName)
	t.Check(removeTmpFile(absFilePath, t), Equals, nil)
	// Remove should succed even when pidfile is missing
	t.Assert(s.testPidFile.Remove(), Equals, nil)
}
