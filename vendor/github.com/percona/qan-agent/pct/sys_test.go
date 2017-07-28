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
	"os"

	"github.com/percona/qan-agent/pct"
	. "gopkg.in/check.v1"
)

/////////////////////////////////////////////////////////////////////////////
// sys.go test suite
/////////////////////////////////////////////////////////////////////////////

type SysTestSuite struct {
}

var _ = Suite(&SysTestSuite{})

func (s *SysTestSuite) TestSameFile(t *C) {
	var err error
	var same bool

	same, err = pct.SameFile("/etc/passwd", "/etc/passwd")
	if !same {
		t.Error("/etc/passwd is same as itself")
	}
	if err != nil {
		t.Error(err)
	}

	same, err = pct.SameFile("/etc/passwd", "/etc/group")
	if same {
		t.Error("/etc/passwd is same as /etc/group")
	}
	if err != nil {
		t.Error(err)
	}

	/**
	 * Simulate renaming/rotating MySQL slow log. The original slow log is renamed,
	 * then a new slow log with the same original name is created.  These two files
	 * should _not_ be the same because they'll have different inodes.
	 */
	origFile := "/tmp/pct-test"
	newFile := "/tmp/pct-test-new"
	defer func() {
		os.Remove(origFile)
		os.Remove(newFile)
	}()

	var f1 *os.File
	f1, err = os.Create(origFile)
	if err != nil {
		t.Fatal(err)
	}
	f1.Close()

	os.Rename(origFile, newFile)

	var f2 *os.File
	f2, err = os.Create(origFile)
	if err != nil {
		t.Fatal(err)
	}
	f2.Close()

	same, err = pct.SameFile(origFile, newFile)
	if same {
		t.Error(origFile, "and "+newFile+" not same after rename")
	}
	if err != nil {
		t.Error(err)
	}
}

func (s *SysTestSuite) TestMbps(t *C) {
	t.Check(pct.Mbps(0, 1.0), Equals, "0.00")
	t.Check(pct.Mbps(12749201, 0), Equals, "0.00")

	// 1 Mbps = 1048576 bytes = 8 388 608 bits = 8.39 Mbps
	t.Check(pct.Mbps(1048576, 1.0), Equals, "8.39")

	// 222566303 bytes = 1 780 530 424 bits = 1780.53 Mbps
	t.Check(pct.Mbps(222566303, 1.0), Equals, "1780.53")
	t.Check(pct.Mbps(222566303, 2.0), Equals, "890.27")
	t.Check(pct.Mbps(222566303, 300.0), Equals, "5.94")  // 5m
	t.Check(pct.Mbps(222566303, 3600.0), Equals, "0.49") // 1h
}

func (s *SysTestSuite) TestBytes(t *C) {
	t.Check(pct.Bytes(0), Equals, "0")
	t.Check(pct.Bytes(1024), Equals, "1.02 kB")
	t.Check(pct.Bytes(12749201), Equals, "12.75 MB")
	t.Check(pct.Bytes(222566303), Equals, "222.57 MB")
	t.Check(pct.Bytes(1987654321), Equals, "1.99 GB")
	t.Check(pct.Bytes(5001987654321), Equals, "5.00 TB")
}

func (s *SysTestSuite) TestDuration(t *C) {
	t.Check(pct.Duration(0), Equals, "0")
	t.Check(pct.Duration(0.000001), Equals, "1µ")
	t.Check(pct.Duration(0.000010), Equals, "10µ")
	t.Check(pct.Duration(0.000100), Equals, "100µ")
	t.Check(pct.Duration(0.001), Equals, "1ms")
	t.Check(pct.Duration(0.010), Equals, "10ms")
	t.Check(pct.Duration(0.100), Equals, "100ms")
	t.Check(pct.Duration(0.100200300400), Equals, "100ms")
	t.Check(pct.Duration(0.999999), Equals, "999ms")
	t.Check(pct.Duration(1), Equals, "1s")
	t.Check(pct.Duration(1.357901), Equals, "1.358s")
	t.Check(pct.Duration(63.000001), Equals, "1m3s")
	t.Check(pct.Duration(1.300000), Equals, "1.3s")
	t.Check(pct.Duration(55), Equals, "55s")
	t.Check(pct.Duration(72), Equals, "1m12s")
	t.Check(pct.Duration(100.500600), Equals, "1m40.501s")
	t.Check(pct.Duration(4000), Equals, "1h6m40s")
	t.Check(pct.Duration(100000), Equals, "1d3h46m40s")
}

func (s *SysTestSuite) TestAtLeastVersion(t *C) {
	var got bool
	var err error

	v := "5.1"

	got, err = pct.AtLeastVersion("5.0", v)
	t.Check(err, IsNil)
	t.Check(got, Equals, false)

	got, err = pct.AtLeastVersion("ubuntu-something", v)
	t.Check(err, NotNil)
	t.Check(got, Equals, false)

	got, err = pct.AtLeastVersion("5.0.1-ubuntu-something", v)
	t.Check(err, IsNil)
	t.Check(got, Equals, false)

	got, err = pct.AtLeastVersion(v, v)
	t.Check(err, IsNil)
	t.Check(got, Equals, true)

	got, err = pct.AtLeastVersion("5.1.0-ubuntu-something", v)
	t.Check(err, IsNil)
	t.Check(got, Equals, true)

	got, err = pct.AtLeastVersion("10.1.0-MariaDB", v)
	t.Check(err, IsNil)
	t.Check(got, Equals, true)
}
