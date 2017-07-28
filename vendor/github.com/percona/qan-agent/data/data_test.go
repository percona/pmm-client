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

package data_test

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"io/ioutil"
	"log"
	"os"
	"path"
	"path/filepath"
	"testing"
	"time"

	. "github.com/go-test/test"
	"github.com/percona/pmm/proto"
	pc "github.com/percona/pmm/proto/config"
	"github.com/percona/qan-agent/data"
	"github.com/percona/qan-agent/pct"
	"github.com/percona/qan-agent/test"
	"github.com/percona/qan-agent/test/mock"
	. "gopkg.in/check.v1"
)

// Hook up gocheck into the "go test" runner.
func Test(t *testing.T) { TestingT(t) }

var sample = test.RootDir + "/qan/"

func debug(logChan chan *proto.LogEntry) {
	for logEntry := range logChan {
		log.Println(logEntry)
	}
}

/////////////////////////////////////////////////////////////////////////////
// DiskvSpooler test suite
/////////////////////////////////////////////////////////////////////////////

type DiskvSpoolerTestSuite struct {
	logChan  chan *proto.LogEntry
	logger   *pct.Logger
	basedir  string
	dataDir  string
	trashDir string
	limits   pc.DataSpoolLimits
}

var _ = Suite(&DiskvSpoolerTestSuite{})

func (s *DiskvSpoolerTestSuite) SetUpSuite(t *C) {
	s.logChan = make(chan *proto.LogEntry, 10)
	s.logger = pct.NewLogger(s.logChan, "data_test")

	s.basedir, _ = ioutil.TempDir("/tmp", "percona-agent-data-spooler-test")
	s.dataDir = path.Join(s.basedir, "data")
	s.trashDir = path.Join(s.basedir, "trash")

	s.limits = pc.DataSpoolLimits{
		MaxAge:   data.DEFAULT_DATA_MAX_AGE,
		MaxSize:  data.DEFAULT_DATA_MAX_SIZE,
		MaxFiles: data.DEFAULT_DATA_MAX_FILES,
	}
}

func (s *DiskvSpoolerTestSuite) SetUpTest(t *C) {
	files, _ := filepath.Glob(s.dataDir + "/*")
	for _, file := range files {
		if err := os.Remove(file); err != nil {
			t.Error(err)
		}
	}
	files, _ = filepath.Glob(s.trashDir + "/data/*")
	for _, file := range files {
		if err := os.Remove(file); err != nil {
			t.Error(err)
		}
	}
}

func (s *DiskvSpoolerTestSuite) TearDownSuite(t *C) {
	if err := os.RemoveAll(s.basedir); err != nil {
		t.Error(err)
	}
}

// --------------------------------------------------------------------------

func (s *DiskvSpoolerTestSuite) TestSpoolData(t *C) {
	sz := proto.NewJsonSerializer()

	// Create and start the spooler.
	spool := data.NewDiskvSpooler(s.logger, s.dataDir, s.trashDir, "localhost", s.limits)
	if spool == nil {
		t.Fatal("NewDiskvSpooler")
	}

	err := spool.Start(sz)
	if err != nil {
		t.Fatal(err)
	}

	// Doesn't matter what data we spool; just send some bytes...
	now := time.Now()
	logEntry := &proto.LogEntry{
		Ts:      now,
		Level:   1,
		Service: "mm",
		Msg:     "hello world",
	}
	spool.Write("log", logEntry)

	// Spooler should wrap data in proto.Data and write to disk, in format of serializer.
	files := test.WaitFiles(s.dataDir, 1)
	if len(files) != 1 {
		t.Fatalf("Expected 1 file, got %d\n", len(files))
	}

	gotFiles := []string{}
	filesChan := spool.Files()
	for file := range filesChan {
		gotFiles = append(gotFiles, file)
	}
	if gotFiles[0] != files[0].Name() {
		t.Error("Spool writes and returns " + files[0].Name())
	}
	if len(gotFiles) != len(files) {
		t.Error("Spool writes and returns ", len(files), " file")
	}

	// data is proto.Data[ metadata, Data: proto.LogEntry[...] ]
	data, err := spool.Read(gotFiles[0])
	if err != nil {
		t.Error(err)
	}
	protoData := &proto.Data{}
	if err := json.Unmarshal(data, protoData); err != nil {
		t.Fatal(err)
	}
	t.Check(protoData.Service, Equals, "log")
	t.Check(protoData.ContentType, Equals, "application/json")
	t.Check(protoData.ContentEncoding, Equals, "")
	if protoData.Created.IsZero() || protoData.Created.Before(now) {
		// The proto.Data can't be created before the data it contains.
		t.Errorf("proto.Data.Created after data, got %s", protoData.Created)
	}

	// The LogoEntry we get back should be identical the one we spooled.
	gotLogEntry := &proto.LogEntry{}
	if err := json.Unmarshal(protoData.Data, gotLogEntry); err != nil {
		t.Fatal(err)
	}
	if same, diff := IsDeeply(gotLogEntry, logEntry); !same {
		t.Logf("%#v", gotLogEntry)
		t.Error(diff)
	}

	// Removing data from spooler should remove the file.
	spool.Remove(gotFiles[0])
	files = test.WaitFiles(s.dataDir, -1)
	if len(files) != 0 {
		t.Fatalf("Expected no files, got %d\n", len(files))
	}

	spool.Stop()
}

func (s *DiskvSpoolerTestSuite) TestSpoolGzipData(t *C) {
	// Same as TestSpoolData, but use the gzip serializer.

	sz := proto.NewJsonGzipSerializer()

	// See TestSpoolData() for description of these tasks.
	spool := data.NewDiskvSpooler(s.logger, s.dataDir, s.trashDir, "localhost", s.limits)
	if spool == nil {
		t.Fatal("NewDiskvSpooler")
	}

	err := spool.Start(sz)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	logEntry := &proto.LogEntry{
		Ts:      now,
		Level:   1,
		Service: "mm",
		Msg:     "hello world",
	}
	spool.Write("log", logEntry)

	files := test.WaitFiles(s.dataDir, 1)
	if len(files) != 1 {
		t.Fatalf("Expected 1 file, got %d\n", len(files))
	}

	gotFiles := []string{}
	filesChan := spool.Files()
	for file := range filesChan {
		gotFiles = append(gotFiles, file)
	}

	gotData, err := spool.Read(gotFiles[0])
	if err != nil {
		t.Error(err)
	}
	if len(gotData) <= 0 {
		t.Fatal("1st file has data")
	}

	protoData := &proto.Data{}
	if err := json.Unmarshal(gotData, protoData); err != nil {
		t.Fatal(err)
	}
	t.Check(protoData.Service, Equals, "log")
	t.Check(protoData.ContentType, Equals, "application/json")
	t.Check(protoData.ContentEncoding, Equals, "gzip")

	// Decompress and decode and we should have the same LogEntry.
	b := bytes.NewBuffer(protoData.Data)
	g, err := gzip.NewReader(b)
	if err != nil {
		t.Error(err)
	}
	d := json.NewDecoder(g)
	gotLogEntry := &proto.LogEntry{}
	err = d.Decode(gotLogEntry)
	if err := d.Decode(gotLogEntry); err != io.EOF {
		t.Error(err)
	}

	if same, diff := IsDeeply(gotLogEntry, logEntry); !same {
		t.Error(diff)
	}

	/**
	 * Do it again to test that serialize is stateless, so to speak.
	 */

	logEntry2 := &proto.LogEntry{
		Ts:      now,
		Level:   2,
		Service: "mm",
		Msg:     "number 2",
	}
	spool.Write("log", logEntry2)

	files = test.WaitFiles(s.dataDir, 2)
	if len(files) != 2 {
		t.Fatalf("Expected 2 file, got %d\n", len(files))
	}

	gotFiles = []string{}
	filesChan = spool.Files()
	for file := range filesChan {
		gotFiles = append(gotFiles, file)
	}

	gotData, err = spool.Read(gotFiles[1]) // 2nd data, 2nd file
	if err != nil {
		t.Error(err)
	}
	if len(gotData) <= 0 {
		t.Fatal("2nd file has data")
	}

	protoData = &proto.Data{}
	if err := json.Unmarshal(gotData, protoData); err != nil {
		t.Fatal(err)
	}
	t.Check(protoData.Service, Equals, "log")
	t.Check(protoData.ContentType, Equals, "application/json")
	t.Check(protoData.ContentEncoding, Equals, "gzip")

	b = bytes.NewBuffer(protoData.Data)
	g, err = gzip.NewReader(b)
	if err != nil {
		t.Error(err)
	}
	d = json.NewDecoder(g)
	gotLogEntry = &proto.LogEntry{}
	err = d.Decode(gotLogEntry)
	if err := d.Decode(gotLogEntry); err != io.EOF {
		t.Error(err)
	}

	if same, diff := IsDeeply(gotLogEntry, logEntry2); !same {
		t.Error(diff)
	}

	spool.Stop()
}

func (s *DiskvSpoolerTestSuite) TestRejectData(t *C) {
	sz := proto.NewJsonSerializer()

	// Create and start the spooler.
	spool := data.NewDiskvSpooler(s.logger, s.dataDir, s.trashDir, "localhost", s.limits)
	t.Assert(spool, NotNil)

	err := spool.Start(sz)
	t.Assert(err, IsNil)

	// Spooler should create the bad data dir.
	badDataDir := path.Join(s.trashDir, "data")
	ok := pct.FileExists(badDataDir)
	t.Assert(ok, Equals, true)

	// Spool any data...
	now := time.Now()
	logEntry := &proto.LogEntry{
		Ts:      now,
		Level:   1,
		Service: "mm",
		Msg:     "hello world",
	}
	err = spool.Write("log", logEntry)
	t.Check(err, IsNil)

	// Wait for spooler to write data to disk.
	files := test.WaitFiles(s.dataDir, 1)
	t.Assert(files, HasLen, 1)

	// Get the file name the spooler saved the data as.
	gotFiles := []string{}
	filesChan := spool.Files()
	for file := range filesChan {
		gotFiles = append(gotFiles, file)
	}
	t.Assert(gotFiles, HasLen, 1)

	// Reject the file.  The spooler should move it to the bad data dir
	// then remove it from the list.
	err = spool.Reject(gotFiles[0])
	t.Check(err, IsNil)

	ok = pct.FileExists(path.Join(s.dataDir, gotFiles[0]))
	t.Assert(ok, Equals, false)

	badFile := path.Join(badDataDir, gotFiles[0])
	ok = pct.FileExists(path.Join(badFile))
	t.Assert(ok, Equals, true)

	spool.Stop()

	/**
	 * Start another spooler now that we have data/bad/file to ensure
	 * that the spooler does not read/index/cache bad files.
	 */

	spool = data.NewDiskvSpooler(s.logger, s.dataDir, s.trashDir, "localhost", s.limits)
	t.Assert(spool, NotNil)
	err = spool.Start(sz)
	t.Assert(err, IsNil)
	spool.Write("log", logEntry)
	files = test.WaitFiles(s.dataDir, 1)
	t.Assert(files, HasLen, 1)

	// There should only be 1 new file in the spool.
	gotFiles = []string{}
	filesChan = spool.Files()
	for file := range filesChan {
		t.Check(file, Not(Equals), badFile)
		gotFiles = append(gotFiles, file)
	}
	t.Assert(gotFiles, HasLen, 1)

	spool.Stop()
}

func (s *DiskvSpoolerTestSuite) TestSpoolLimits(t *C) {
	limits := pc.DataSpoolLimits{
		MaxAge:   10,   // seconds
		MaxSize:  1024, // bytes
		MaxFiles: 2,
	}

	sz := proto.NewJsonSerializer()
	spool := data.NewDiskvSpooler(s.logger, s.dataDir, s.trashDir, "localhost", limits)
	t.Assert(spool, NotNil)
	err := spool.Start(sz)
	t.Assert(err, IsNil)

	// Spool 3 data files (doesn't matter what, any data works).
	now := time.Now()
	logEntry := &proto.LogEntry{
		Ts:  now,
		Msg: "1",
	}
	spool.Write("log", logEntry)
	logEntry.Msg = "2"
	spool.Write("log", logEntry)
	logEntry.Msg = "3"
	spool.Write("log", logEntry)

	// Wait for spooler to write the data files.
	files := test.WaitFiles(s.dataDir, 3)
	t.Assert(files, HasLen, 3)

	// Purge the spool and 1 data file should be droppoed because
	// we set limits.MaxFiles=2.
	n, removed := spool.Purge(time.Now().UTC(), limits)
	t.Check(n, Equals, 1)
	t.Check(removed["purged"], HasLen, 0)
	t.Check(removed["age"], HasLen, 0)
	t.Check(removed["size"], HasLen, 0)
	t.Assert(removed["files"], HasLen, 1) // here it is

	// Find out how large the files are so we can purge based on MaxSize.
	totalSize := 0
	for file := range spool.Files() {
		data, err := spool.Read(file)
		t.Assert(err, IsNil)
		totalSize += len(data)
	}

	// Set MaxSize a few bytes less than the total which should cause only one
	// data file to be purged.
	limits.MaxSize = uint64(totalSize - 10)

	n, removed = spool.Purge(time.Now().UTC(), limits)
	t.Check(n, Equals, 1)
	t.Check(removed["purged"], HasLen, 0)
	t.Check(removed["age"], HasLen, 0)
	t.Check(removed["size"], HasLen, 1) // here it is
	t.Assert(removed["files"], HasLen, 0)

	// To test MaxAge, pass in a now arg that's in the past and it should cause
	// the last file to be purged.
	n, removed = spool.Purge(time.Now().Add(-1*time.Minute).UTC(), limits)
	t.Check(n, Equals, 1)
	t.Check(removed["purged"], HasLen, 0)
	t.Check(removed["age"], HasLen, 1) // here it is
	t.Check(removed["size"], HasLen, 0)
	t.Assert(removed["files"], HasLen, 0)

	// Test a full spool purge by passing no limits.
	spool.Write("log", logEntry)
	spool.Write("log", logEntry)
	spool.Write("log", logEntry)

	files = test.WaitFiles(s.dataDir, 3)
	t.Assert(files, HasLen, 3)

	limits = pc.DataSpoolLimits{} // no limit = purge all
	n, removed = spool.Purge(time.Now().UTC(), limits)
	t.Check(n, Equals, 3)
	t.Check(removed["purged"], HasLen, 3) // here it is
	t.Check(removed["age"], HasLen, 0)
	t.Check(removed["size"], HasLen, 0)
	t.Assert(removed["files"], HasLen, 0)

	// Finally, test that the auto-purge works by sending a tick manually.
	limits = pc.DataSpoolLimits{
		MaxAge:   10,   // seconds
		MaxSize:  1024, // bytes
		MaxFiles: 2,
	}
	spool = data.NewDiskvSpooler(s.logger, s.dataDir, s.trashDir, "localhost", limits)
	t.Assert(spool, NotNil)

	purgeChan := make(chan time.Time, 1)
	spool.PurgeChan(purgeChan) // must set before calling Start

	err = spool.Start(sz)
	t.Assert(err, IsNil)

	spool.Write("log", logEntry)
	spool.Write("log", logEntry)
	spool.Write("log", logEntry) // one too many
	files = test.WaitFiles(s.dataDir, 3)
	t.Assert(files, HasLen, 3)

	purgeChan <- time.Now() // cause auto-purge in run()
	time.Sleep(200 * time.Millisecond)
	files = test.WaitFiles(s.dataDir, 2)
	t.Assert(files, HasLen, 2)
}

/////////////////////////////////////////////////////////////////////////////
// Sender test suite
/////////////////////////////////////////////////////////////////////////////

type SenderTestSuite struct {
	logChan    chan *proto.LogEntry
	logger     *pct.Logger
	tickerChan chan time.Time
	// --
	dataChan chan []byte
	respChan chan interface{}
	client   *mock.DataClient
}

var _ = Suite(&SenderTestSuite{})

func (s *SenderTestSuite) SetUpSuite(t *C) {
	s.logChan = make(chan *proto.LogEntry, 10)
	s.logger = pct.NewLogger(s.logChan, "data_test")
	s.tickerChan = make(chan time.Time, 1)

	s.dataChan = make(chan []byte, 5)
	s.respChan = make(chan interface{})
	s.client = mock.NewDataClient(s.dataChan, s.respChan)
}

func (s *SenderTestSuite) SetUpTest(t *C) {
	test.DrainTraceChan(s.client.TraceChan)
	test.DrainDataChan(s.dataChan)
	test.DrainRecvData(s.respChan)
}

// --------------------------------------------------------------------------

func (s *SenderTestSuite) TestSendData(t *C) {
	spool := mock.NewSpooler(nil)

	slow001, err := ioutil.ReadFile(sample + "slow001.json")
	if err != nil {
		t.Fatal(err)
	}

	spool.FilesOut = []string{"slow001.json"}
	spool.DataOut = map[string][]byte{"slow001.json": slow001}

	sender := data.NewSender(s.logger, s.client)

	err = sender.Start(spool, s.tickerChan, 5, false)
	if err != nil {
		t.Fatal(err)
	}

	data := test.WaitBytes(s.dataChan)
	if len(data) != 0 {
		t.Errorf("No data sent before tick; got %+v", data)
	}

	s.tickerChan <- time.Now()

	data = test.WaitBytes(s.dataChan)
	if same, diff := IsDeeply(data[0], slow001); !same {
		t.Error(diff)
	}

	t.Check(len(spool.DataOut), Equals, 1)

	select {
	case s.respChan <- &proto.Response{Code: 200}:
	case <-time.After(500 * time.Millisecond):
		t.Error("Sender receives prot.Response after sending data")
	}

	// Sender should include its websocket client status.  We're using a mock ws client
	// which reports itself as "data-client: ok".
	status := sender.Status()
	t.Check(status["data-client"], Equals, "ok")

	err = sender.Stop()
	t.Assert(err, IsNil)

	t.Check(len(spool.DataOut), Equals, 0)
	t.Check(len(spool.RejectedFiles), Equals, 0)
}

func (s *SenderTestSuite) TestBlackhole(t *C) {
	spool := mock.NewSpooler(nil)

	slow001, err := ioutil.ReadFile(sample + "slow001.json")
	if err != nil {
		t.Fatal(err)
	}

	spool.FilesOut = []string{"slow001.json"}
	spool.DataOut = map[string][]byte{"slow001.json": slow001}

	sender := data.NewSender(s.logger, s.client)

	err = sender.Start(spool, s.tickerChan, 5, true) // <- true = enable blackhole
	if err != nil {
		t.Fatal(err)
	}

	s.tickerChan <- time.Now()

	data := test.WaitBytes(s.dataChan)
	if len(data) != 0 {
		t.Errorf("Data sent despite blackhole; got %+v", data)
	}

	select {
	case s.respChan <- &proto.Response{Code: 200}:
		// Should not recv response because no data was sent.
		t.Error("Sender receives prot.Response after sending data")
	case <-time.After(500 * time.Millisecond):
	}

	err = sender.Stop()
	t.Assert(err, IsNil)
}

func (s *SenderTestSuite) TestSendEmptyFile(t *C) {
	// Make mock spooler which returns a single file name and zero bytes
	// for that file.
	spool := mock.NewSpooler(nil)
	spool.FilesOut = []string{"empty.json"}
	spool.DataOut = map[string][]byte{"empty.json": []byte{}}

	// Start the sender.
	sender := data.NewSender(s.logger, s.client)
	err := sender.Start(spool, s.tickerChan, 5, false)
	t.Assert(err, IsNil)

	// Tick to make sender send.
	s.tickerChan <- time.Now()

	// Sender shouldn't zero-length data files...
	data := test.WaitBytes(s.dataChan)
	t.Check(data, HasLen, 0)

	err = sender.Stop()
	t.Assert(err, IsNil)

	// ...but it should remove them.
	t.Check(len(spool.DataOut), Equals, 0)
}

func (s *SenderTestSuite) TestConnectErrors(t *C) {
	spool := mock.NewSpooler(nil)

	spool.FilesOut = []string{"slow001.json"}
	spool.DataOut = map[string][]byte{"slow001.json": []byte("...")}

	sender := data.NewSender(s.logger, s.client)

	err := sender.Start(spool, s.tickerChan, 60, false)
	t.Assert(err, IsNil)

	// Any connect error will do.
	s.client.ConnectError = io.EOF
	defer func() { s.client.ConnectError = nil }()

	// Tick causes send to connect and send all files.
	s.tickerChan <- time.Now()
	t0 := time.Now()

	// Wait for sender to start trying to connect...
	if !test.WaitStatus(5, sender, "data-sender", "Connecting") {
		t.Fatal("Timeout waiting for data-sender status=Connecting")
	}
	// ...then wait for it to finsih and return.
	if !test.WaitStatusPrefix(data.MAX_SEND_ERRORS*data.CONNECT_ERROR_WAIT, sender, "data-sender", "Idle") {
		t.Fatal("Timeout waiting for data-sender status=Idle")
	}
	d := time.Now().Sub(t0).Seconds()

	// It should wait between reconnects, but not too long.
	if d < float64((data.MAX_SEND_ERRORS-1)*data.CONNECT_ERROR_WAIT) {
		t.Error("Waits between reconnects")
	}
	if d > float64(data.MAX_SEND_ERRORS*data.CONNECT_ERROR_WAIT) {
		t.Error("Waited too long between reconnects")
	}

	err = sender.Stop()
	t.Assert(err, IsNil)

	// Couldn't connect, so it doesn't send or reject anything.
	t.Check(len(spool.DataOut), Equals, 1)
	t.Check(len(spool.RejectedFiles), Equals, 0)

	// It should have called ConnectOnce() serveral times, else it didn't
	// really try to reconnect.
	trace := test.DrainTraceChan(s.client.TraceChan)
	t.Check(trace, DeepEquals, []string{
		"ConnectOnce",
		"ConnectOnce",
		"ConnectOnce",
		"DisconnectOnce",
	})
}

func (s *SenderTestSuite) TestRecvErrors(t *C) {
	spool := mock.NewSpooler(nil)
	spool.FilesOut = []string{"slow001.json"}
	spool.DataOut = map[string][]byte{"slow001.json": []byte("...")}

	sender := data.NewSender(s.logger, s.client)

	err := sender.Start(spool, s.tickerChan, 60, false)
	t.Assert(err, IsNil)

	// Any recv error will do.
	doneChan := make(chan bool)
	go func() {
		for {
			select {
			case s.client.RecvError <- io.EOF:
			case <-doneChan:
				return
			}
		}
	}()
	defer func() { doneChan <- true }()

	// Tick causes send to connect and send all files.
	s.tickerChan <- time.Now()
	t0 := time.Now()

	// Wait for sender to finsih and return.
	if !test.WaitStatusPrefix(data.MAX_SEND_ERRORS*data.CONNECT_ERROR_WAIT, sender, "data-sender", "Idle") {
		t.Fatal("Timeout waiting for data-sender status=Idle")
	}
	d := time.Now().Sub(t0).Seconds()

	// It should wait between reconnects, but not too long.
	if d < float64((data.MAX_SEND_ERRORS-1)*data.CONNECT_ERROR_WAIT) {
		t.Error("Waits between reconnects")
	}
	if d > float64(data.MAX_SEND_ERRORS*data.CONNECT_ERROR_WAIT) {
		t.Error("Waited too long between reconnects")
	}
	err = sender.Stop()
	t.Assert(err, IsNil)

	// Didn't receive proper ack, so it doesn't remove any data.
	t.Check(len(spool.DataOut), Equals, 1)
	t.Check(len(spool.RejectedFiles), Equals, 0)

	// It should have called ConnectOnce() serveral times, else it didn't
	// really try to reconnect.
	trace := test.DrainTraceChan(s.client.TraceChan)
	t.Check(trace, DeepEquals, []string{
		"ConnectOnce",
		"SendBytes",
		"Recv",
		"DisconnectOnce",
		"ConnectOnce",
		"SendBytes",
		"Recv",
		"DisconnectOnce",
		"ConnectOnce",
		"SendBytes",
		"Recv",
		"DisconnectOnce",
		"DisconnectOnce",
	})
}

func (s *SenderTestSuite) Test500Error(t *C) {
	spool := mock.NewSpooler(nil)
	spool.FilesOut = []string{"file1", "file2", "file3"}
	spool.DataOut = map[string][]byte{
		"file1": []byte("file1"),
		"file2": []byte("file2"),
		"file3": []byte("file3"),
	}

	sender := data.NewSender(s.logger, s.client)
	err := sender.Start(spool, s.tickerChan, 5, false)
	t.Assert(err, IsNil)

	s.tickerChan <- time.Now()

	got := test.WaitBytes(s.dataChan)
	if same, diff := IsDeeply(got[0], []byte("file1")); !same {
		t.Error(diff)
	}

	// 3 files before API error.
	t.Check(len(spool.DataOut), Equals, 3)

	// Simulate API error.
	select {
	case s.respChan <- &proto.Response{Code: 503}:
	case <-time.After(500 * time.Millisecond):
		t.Error("Sender receives prot.Response after sending data")
	}

	// Wait for it to finsih and return.
	if !test.WaitStatusPrefix(data.MAX_SEND_ERRORS*data.CONNECT_ERROR_WAIT, sender, "data-sender", "Idle") {
		t.Fatal("Timeout waiting for data-sender status=Idle")
	}

	// Still 3 files after API error.
	t.Check(len(spool.DataOut), Equals, 3)
	t.Check(len(spool.RejectedFiles), Equals, 0)

	// There's only 1 call to SendBytes because after an API error
	// the send stops immediately.
	trace := test.DrainTraceChan(s.client.TraceChan)
	t.Check(trace, DeepEquals, []string{
		"ConnectOnce",
		"SendBytes",
		"Recv",
		"DisconnectOnce",
	})

	err = sender.Stop()
	t.Assert(err, IsNil)
}

func (s *SenderTestSuite) TestBadFiles(t *C) {
	spool := mock.NewSpooler(nil)
	spool.FilesOut = []string{"file1", "file2", "file3"}
	spool.DataOut = map[string][]byte{
		"file1": []byte("file1"),
		"file2": []byte("file2"),
		"file3": []byte("file3"),
	}

	sender := data.NewSender(s.logger, s.client)
	err := sender.Start(spool, s.tickerChan, 5, false)
	t.Assert(err, IsNil)

	doneChan := make(chan bool, 1)
	go func() {
		resp := []uint{400, 400, 200}
		for i := 0; i < 3; i++ {
			// Wait for sender to send data.
			select {
			case <-s.dataChan:
			case <-doneChan:
				return
			}

			// Simulate API returns 400.
			select {
			case s.respChan <- &proto.Response{Code: resp[i]}:
			case <-doneChan:
				return
			}
		}
	}()

	s.tickerChan <- time.Now()

	// Wait for sender to finish.
	if !test.WaitStatusPrefix(data.MAX_SEND_ERRORS*data.CONNECT_ERROR_WAIT, sender, "data-sender", "Idle") {
		t.Fatal("Timeout waiting for data-sender status=Idle")
	}

	doneChan <- true
	err = sender.Stop()
	t.Assert(err, IsNil)

	// Bad files are removed, so all files should have been sent.
	t.Check(len(spool.DataOut), Equals, 0)
	t.Check(len(spool.RejectedFiles), Equals, 0)
}

/////////////////////////////////////////////////////////////////////////////
// Manager test suite
/////////////////////////////////////////////////////////////////////////////

type ManagerTestSuite struct {
	logChan  chan *proto.LogEntry
	logger   *pct.Logger
	basedir  string
	trashDir string
	dataDir  string
	dataChan chan []byte
	respChan chan interface{}
	client   *mock.DataClient
}

var _ = Suite(&ManagerTestSuite{})

func (s *ManagerTestSuite) SetUpSuite(t *C) {
	var err error
	s.basedir, err = ioutil.TempDir("/tmp", "percona-agent-data-manager-test")
	t.Assert(err, IsNil)

	if err := pct.Basedir.Init(s.basedir); err != nil {
		t.Fatal(err)
	}
	s.dataDir = pct.Basedir.Dir("data")
	s.trashDir = path.Join(s.basedir, "trash")

	s.logChan = make(chan *proto.LogEntry, 10)
	s.logger = pct.NewLogger(s.logChan, "data_test")

	s.dataChan = make(chan []byte, 5)
	s.respChan = make(chan interface{})
	s.client = mock.NewDataClient(s.dataChan, s.respChan)
}

func (s *ManagerTestSuite) TearDownSuite(t *C) {
	if err := os.RemoveAll(s.basedir); err != nil {
		t.Error(err)
	}
}

// --------------------------------------------------------------------------

func (s *ManagerTestSuite) TestGetConfig(t *C) {
	m := data.NewManager(s.logger, s.dataDir, s.trashDir, "localhost", s.client)
	t.Assert(m, NotNil)

	config := &pc.Data{
		Encoding:     "none",
		SendInterval: 1,
	}
	bytes, _ := json.Marshal(config)
	// Write config to disk because manager reads it on start,
	// else it uses default config.
	pct.Basedir.WriteConfig("data", config)

	err := m.Start()
	t.Assert(err, IsNil)

	sender := m.Sender()
	t.Check(sender, NotNil)

	/**
	 * GetConfig
	 */

	cmd := &proto.Cmd{
		User:    "daniel",
		Service: "data",
		Cmd:     "GetConfig",
	}

	reply := m.Handle(cmd)
	t.Assert(reply.Error, Equals, "")
	t.Assert(reply.Data, NotNil)
	gotConfig := []proto.AgentConfig{}
	if err := json.Unmarshal(reply.Data, &gotConfig); err != nil {
		t.Fatal(err)
	}
	expectConfig := []proto.AgentConfig{
		{
			Service: "data",
			Config:  string(bytes),
		},
	}
	if same, diff := IsDeeply(gotConfig, expectConfig); !same {
		Dump(gotConfig)
		t.Error(diff)
	}

	err = m.Stop()
	t.Assert(err, IsNil)
	if !test.WaitStatus(5, m, "data", "Stopped") {
		t.Fatal("test.WaitStatus() timeout")
	}
	status := m.Status()
	t.Check(status["data-spooler"], Equals, "Stopped")
	t.Check(status["data-sender"], Equals, "Stopped")

	// Config should report Running: false.
	reply = m.Handle(cmd)
	t.Assert(reply.Error, Equals, "")
	t.Assert(reply.Data, NotNil)
	if err := json.Unmarshal(reply.Data, &gotConfig); err != nil {
		t.Fatal(err)
	}
	if same, diff := IsDeeply(gotConfig, expectConfig); !same {
		Dump(gotConfig)
		t.Error(diff)
	}
}

func (s *ManagerTestSuite) TestSetConfig(t *C) {
	m := data.NewManager(s.logger, s.dataDir, s.trashDir, "localhost", s.client)
	t.Assert(m, NotNil)

	config := &pc.Data{
		Encoding:     "none",
		SendInterval: 1,
	}
	pct.Basedir.WriteConfig("data", config)

	err := m.Start()
	t.Assert(err, IsNil)

	sender := m.Sender()
	t.Check(sender, NotNil)

	/**
	 * Change SendInterval
	 */
	config.SendInterval = 5
	configData, err := json.Marshal(config)
	t.Assert(err, IsNil)
	cmd := &proto.Cmd{
		User:    "daniel",
		Service: "data",
		Cmd:     "SetConfig",
		Data:    configData,
	}

	gotReply := m.Handle(cmd)
	t.Assert(gotReply.Error, Equals, "")

	cmd = &proto.Cmd{
		User:    "daniel",
		Service: "data",
		Cmd:     "GetConfig",
	}
	reply := m.Handle(cmd)
	t.Assert(reply.Error, Equals, "")
	t.Assert(reply.Data, NotNil)
	gotConfigRes := []proto.AgentConfig{}
	if err := json.Unmarshal(reply.Data, &gotConfigRes); err != nil {
		t.Fatal(err)
	}
	expectConfigRes := []proto.AgentConfig{
		{
			Service: "data",
			Config:  string(configData),
		},
	}
	if same, diff := IsDeeply(gotConfigRes, expectConfigRes); !same {
		Dump(gotConfigRes)
		t.Error(diff)
	}

	// Verify new config on disk.
	content, err := ioutil.ReadFile(pct.Basedir.ConfigFile("data"))
	t.Assert(err, IsNil)
	gotConfig := &pc.Data{}
	if err := json.Unmarshal(content, gotConfig); err != nil {
		t.Fatal(err)
	}
	if same, diff := IsDeeply(gotConfig, config); !same {
		Dump(gotConfig)
		t.Error(diff)
	}

	/**
	 * Change Encoding
	 */
	config.Encoding = "gzip"
	configData, err = json.Marshal(config)
	t.Assert(err, IsNil)
	cmd = &proto.Cmd{
		User:    "daniel",
		Service: "data",
		Cmd:     "SetConfig",
		Data:    configData,
	}

	gotReply = m.Handle(cmd)
	t.Assert(gotReply.Error, Equals, "")

	cmd = &proto.Cmd{
		User:    "daniel",
		Service: "data",
		Cmd:     "GetConfig",
	}
	reply = m.Handle(cmd)
	t.Assert(reply.Error, Equals, "")
	t.Assert(reply.Data, NotNil)
	if err := json.Unmarshal(reply.Data, &gotConfigRes); err != nil {
		t.Fatal(err)
	}
	expectConfigRes = []proto.AgentConfig{
		{
			Service: "data",
			Config:  string(configData),
		},
	}
	if same, diff := IsDeeply(gotConfigRes, expectConfigRes); !same {
		Dump(gotConfigRes)
		t.Error(diff)
	}

	// Verify new config on disk.
	content, err = ioutil.ReadFile(pct.Basedir.ConfigFile("data"))
	t.Assert(err, IsNil)
	gotConfig = &pc.Data{}
	if err := json.Unmarshal(content, gotConfig); err != nil {
		t.Fatal(err)
	}
	if same, diff := IsDeeply(gotConfig, config); !same {
		Dump(gotConfig)
		t.Error(diff)
	}
}

func (s *ManagerTestSuite) TestStatus(t *C) {
	// Start a data manager.
	m := data.NewManager(s.logger, s.dataDir, s.trashDir, "localhost", s.client)
	t.Assert(m, NotNil)
	config := &pc.Data{
		Encoding:     "gzip",
		SendInterval: 1,
	}
	pct.Basedir.WriteConfig("data", config)

	err := m.Start()
	t.Assert(err, IsNil)

	// Get its status directly.
	if !test.WaitStatus(5, m, "data", "Running") {
		t.Fatal("test.WaitStatus() timeout")
	}
	status := m.Status()
	t.Check(status["data"], Equals, "Running")
	t.Check(status["data-spooler"], Equals, "Idle")
	t.Check(status["data-sender"], Equals, "Idle")
}
