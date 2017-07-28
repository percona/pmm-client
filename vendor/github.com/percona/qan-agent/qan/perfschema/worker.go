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

package perfschema

import (
	"database/sql"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/percona/go-mysql/event"
	"github.com/percona/pmm/proto"
	pc "github.com/percona/pmm/proto/config"
	"github.com/percona/qan-agent/mysql"
	"github.com/percona/qan-agent/pct"
	"github.com/percona/qan-agent/qan"
)

// A DigestRow is a row from performance_schema.events_statements_summary_by_digest.
type DigestRow struct {
	Schema                  string
	Digest                  string
	CountStar               uint
	SumTimerWait            uint64
	MinTimerWait            uint64
	AvgTimerWait            uint64
	MaxTimerWait            uint64
	SumLockTime             uint64
	SumErrors               uint64
	SumWarnings             uint64
	SumRowsAffected         uint64
	SumRowsSent             uint64
	SumRowsExamined         uint64
	SumCreatedTmpDiskTables uint64 // bool in slow log
	SumCreatedTmpTables     uint64 // bool in slow log
	SumSelectFullJoin       uint64 // bool in slow log
	SumSelectFullRangeJoin  uint64
	SumSelectRange          uint64
	SumSelectRangeCheck     uint64
	SumSelectScan           uint64 // bool in slow log
	SumSortMergePasses      uint64
	SumSortRange            uint64
	SumSortRows             uint64
	SumSortScan             uint64
	SumNoIndexUsed          uint64
	SumNoGoodIndexUsed      uint64
}

// A Class represents a single query and its per-schema instances.
type Class struct {
	DigestText string
	Rows       map[string]*DigestRow // keyed on schema
}

// A Snapshot represents all rows from performance_schema.events_statements_summary_by_digest
// at a single time, grouped by digest into classes. Two consecutive Snapshots are needed to
// produce a qan.Result.
type Snapshot map[string]Class // keyed on digest (classId)

// --------------------------------------------------------------------------

type WorkerFactory interface {
	Make(name string, mysqlConn mysql.Connector) *Worker
}

type RealWorkerFactory struct {
	logChan chan proto.LogEntry
}

func NewRealWorkerFactory(logChan chan proto.LogEntry) *RealWorkerFactory {
	f := &RealWorkerFactory{
		logChan: logChan,
	}
	return f
}

func (f *RealWorkerFactory) Make(name string, mysqlConn mysql.Connector) *Worker {
	getRows := func(c chan<- *DigestRow, doneChan chan<- error) error {
		return GetDigestRows(mysqlConn, c, doneChan)
	}
	getText := func(digest string) (string, error) {
		return GetDigestText(mysqlConn, digest)
	}
	return NewWorker(pct.NewLogger(f.logChan, name), mysqlConn, getRows, getText)
}

func GetDigestRows(mysqlConn mysql.Connector, c chan<- *DigestRow, doneChan chan<- error) error {
	rows, err := mysqlConn.DB().Query(
		"SELECT " +
			" COALESCE(SCHEMA_NAME, ''), COALESCE(DIGEST, ''), COUNT_STAR," +
			" SUM_TIMER_WAIT, MIN_TIMER_WAIT, AVG_TIMER_WAIT, MAX_TIMER_WAIT," +
			" SUM_LOCK_TIME," +
			" SUM_ERRORS, SUM_WARNINGS," +
			" SUM_ROWS_AFFECTED, SUM_ROWS_SENT, SUM_ROWS_EXAMINED," +
			" SUM_CREATED_TMP_DISK_TABLES, SUM_CREATED_TMP_TABLES," +
			" SUM_SELECT_FULL_JOIN, SUM_SELECT_FULL_RANGE_JOIN, SUM_SELECT_RANGE, SUM_SELECT_RANGE_CHECK, SUM_SELECT_SCAN," +
			" SUM_SORT_MERGE_PASSES, SUM_SORT_RANGE, SUM_SORT_ROWS, SUM_SORT_SCAN," +
			" SUM_NO_INDEX_USED, SUM_NO_GOOD_INDEX_USED" +
			" FROM performance_schema.events_statements_summary_by_digest")
	if err != nil {
		// This bubbles up to the analyzer which logs it as an error:
		//   0. Analyer.Worker.Run()
		//   1. Worker.Run().getSnapShot()
		//   2. Worker.getSnapshot().getRows() (ptr to this func)
		//   3. here
		return err
	}
	go func() {
		var err error
		defer func() {
			rows.Close()
			doneChan <- err
		}()
		for rows.Next() {
			row := &DigestRow{}
			err = rows.Scan(
				&row.Schema,
				&row.Digest,
				&row.CountStar,
				&row.SumTimerWait,
				&row.MinTimerWait,
				&row.AvgTimerWait,
				&row.MaxTimerWait,
				&row.SumLockTime,
				&row.SumErrors,
				&row.SumWarnings,
				&row.SumRowsAffected,
				&row.SumRowsSent,
				&row.SumRowsExamined,
				&row.SumCreatedTmpDiskTables,
				&row.SumCreatedTmpTables,
				&row.SumSelectFullJoin,
				&row.SumSelectFullRangeJoin,
				&row.SumSelectRange,
				&row.SumSelectRangeCheck,
				&row.SumSelectScan,
				&row.SumSortMergePasses,
				&row.SumSortRange,
				&row.SumSortRows,
				&row.SumSortScan,
				&row.SumNoIndexUsed,
				&row.SumNoGoodIndexUsed,
			)
			if err != nil {
				return // This bubbles up too (see above).
			}
			c <- row
		}
		if err = rows.Err(); err != nil {
			return // This bubbles up too (see above).
		}
	}()
	return nil
}

func GetDigestText(mysqlConn mysql.Connector, digest string) (string, error) {
	query := fmt.Sprintf("SELECT DIGEST_TEXT"+
		" FROM performance_schema.events_statements_summary_by_digest"+
		" WHERE DIGEST='%s' LIMIT 1", digest)
	var digestText string
	err := mysqlConn.DB().QueryRow(query).Scan(&digestText)
	return digestText, err
}

// --------------------------------------------------------------------------

type GetDigestRowsFunc func(c chan<- *DigestRow, doneChan chan<- error) error
type GetDigestTextFunc func(string) (string, error)

type Worker struct {
	logger    *pct.Logger
	mysqlConn mysql.Connector
	getRows   GetDigestRowsFunc
	getText   GetDigestTextFunc
	// --
	name          string
	status        *pct.Status
	prev          Snapshot
	curr          Snapshot
	iter          *qan.Interval
	lastErr       error
	lastRowCnt    uint
	lastFetchTime float64
	lastPrepTime  float64
}

func NewWorker(logger *pct.Logger, mysqlConn mysql.Connector, getRows GetDigestRowsFunc, getText GetDigestTextFunc) *Worker {
	name := logger.Service()
	w := &Worker{
		logger:    logger,
		mysqlConn: mysqlConn,
		getRows:   getRows,
		getText:   getText,
		// --
		name:   name,
		status: pct.NewStatus([]string{name, name + "-last"}),
		prev:   make(Snapshot),
	}
	return w
}

func (w *Worker) Setup(interval *qan.Interval) error {
	if w.iter != nil {
		// Ensure intervals are in sequence, else reset.
		if interval.Number != w.iter.Number+1 {
			w.logger.Warn(fmt.Sprintf("Interval out of sequence: got %d, expected %d", interval.Number, w.iter.Number+1))
			w.reset()
		} else if interval.StartTime.Before(w.iter.StartTime) {
			w.logger.Warn(fmt.Sprintf("Interval reset: previous at %s, now %s", interval.StartTime, w.iter.StartTime))
			w.reset()
		}
	}
	w.iter = interval
	// Reset -last status vals.
	w.lastRowCnt = 0
	w.lastFetchTime = 0
	w.lastPrepTime = 0
	return nil
}

func (w *Worker) Run() (*qan.Result, error) {
	w.logger.Debug("Run:call:", w.iter.Number)
	defer w.logger.Debug("Run:return:", w.iter.Number)

	defer w.status.Update(w.name, "Idle")

	w.status.Update(w.name, "Connecting to MySQL")
	if err := w.mysqlConn.Connect(); err != nil {
		w.logger.Warn(err.Error())
		w.lastErr = err
		return nil, nil // not an error to caller
	}
	defer w.mysqlConn.Close()

	var err error
	w.curr, err = w.getSnapshot(w.prev)
	if err != nil {
		w.lastErr = err
		return nil, err
	}

	if len(w.prev) == 0 {
		return nil, nil
	}

	res, err := w.prepareResult(w.prev, w.curr)
	if err != nil {
		w.lastErr = err
		return nil, err
	}

	return res, nil
}

func (w *Worker) Cleanup() error {
	w.logger.Debug("Cleanup:call:", w.iter.Number)
	defer w.logger.Debug("Cleanup:return:", w.iter.Number)
	w.prev = w.curr
	last := fmt.Sprintf("rows: %d, fetch: %s, prep: %s",
		w.lastRowCnt, pct.Duration(w.lastFetchTime), pct.Duration(w.lastPrepTime))
	if w.lastErr != nil {
		last += fmt.Sprintf(", error: %s", w.lastErr)
	}
	w.status.Update(w.name+"-last", last)
	return nil
}

func (w *Worker) Stop() error {
	return nil
}

func (w *Worker) Status() map[string]string {
	return w.status.All()
}

func (w *Worker) SetConfig(config pc.QAN) {
}

// --------------------------------------------------------------------------

func (w *Worker) reset() {
	w.iter = nil
	w.prev = make(Snapshot)
	w.lastErr = nil
	w.lastRowCnt = 0
	w.lastFetchTime = 0
	w.lastPrepTime = 0
}

func (w *Worker) getSnapshot(prev Snapshot) (Snapshot, error) {
	w.logger.Debug("getSnapshot:call:", w.iter.Number)
	defer w.logger.Debug("getSnapshot:return:", w.iter.Number)

	w.status.Update(w.name, "Processing rows")
	defer w.status.Update(w.name, "Idle")

	t0 := time.Now()
	defer func() { w.lastFetchTime = time.Now().Sub(t0).Seconds() }()

	curr := make(Snapshot)
	rowChan := make(chan *DigestRow)
	doneChan := make(chan error, 1)
	if err := w.getRows(rowChan, doneChan); err != nil {
		if err == sql.ErrNoRows {
			return curr, nil
		}
		return nil, err
	}
	var err error // from getRows() on doneChan
ROW_LOOP:
	for {
		select {
		case row := <-rowChan:
			w.lastRowCnt++
			// If events_statements_summary_by_digest is full, MySQL will start
			// setting the digest to NULL and will only compute a summary under that
			// null digest.
			// http://dev.mysql.com/doc/refman/5.6/en/statement-summary-tables.html#idm140190647360848
			// In that case, we set the digest to the string "2" (1 if for LRQ) to support
			// this summary in PCT
			classId := "2"
			if len(row.Digest) >= 32 {
				classId = strings.ToUpper(row.Digest[16:32])
			}
			if class, haveClass := curr[classId]; haveClass {
				if _, haveRow := class.Rows[row.Schema]; haveRow {
					w.logger.Error("Got class twice: ", row.Schema, row.Digest)
					continue
				}
				class.Rows[row.Schema] = row
			} else {
				// Get class digext text (fingerprint).
				var digestText string
				if prevClass, havePrevClass := prev[classId]; havePrevClass {
					// Class was in previous iter, so re-use its digest text.
					digestText = prevClass.DigestText
				} else {
					// Have never seen class before, so get digext text from perf schema.
					var err error
					digestText, err = w.getText(row.Digest)
					if classId == "2" && digestText == "" {
						// To make explains works
						digestText = `-- performance_schema.events_statements_summary_by_digest is full`
					}
					if err != nil {
						w.logger.Error(err)
						continue
					}
				}
				// Create the class and init with this schema and row.
				curr[classId] = Class{
					DigestText: digestText,
					Rows: map[string]*DigestRow{
						row.Schema: row,
					},
				}
			}
		case err = <-doneChan:
			break ROW_LOOP
		}
	}
	return curr, err
}

func (w *Worker) prepareResult(prev, curr Snapshot) (*qan.Result, error) {
	w.logger.Debug("prepareResult:call:", w.iter.Number)
	defer w.logger.Debug("prepareResult:return:", w.iter.Number)

	w.status.Update(w.name, "Preparing result")
	defer w.status.Update(w.name, "Idle")

	t0 := time.Now()
	defer func() { w.lastPrepTime = time.Now().Sub(t0).Seconds() }()

	global := event.NewClass("", "", false)
	classes := []*event.Class{}

	// Compare current classes to previous.
CLASS_LOOP:
	for classId, class := range curr {

		// If this class does not exist in prev, skip the entire class.
		prevClass, _ := prev[classId]
		/*
			if !ok {
				continue CLASS_LOOP
			}
		*/

		// This class exists in prev, so create a class aggregate of the per-schema
		// query value diffs, for rows that exist in both prev and curr.
		d := DigestRow{MinTimerWait: 0xFFFFFFFF} // class aggregate, becomes class metrics
		n := uint64(0)                           // number of query instances in prev and curr

		// Each row is an instance of the query executed in the schema.
	ROW_LOOP:
		for schema, row := range class.Rows {
			if prevRow, ok := prevClass.Rows[schema]; ok {
				// We saw this row last time, so first check if it executed during
				// the interval:
				if row.CountStar == prevRow.CountStar {
					continue ROW_LOOP // not executed during interval
				}

				// This row executed during the interval, and we've seen it before,
				// so athe diff of the totals to the class metric totals. For example,
				// if query 1 in db1 has prev.CountStar=50 and curr.CountStar=100,
				// and query 1 in db2 has prev.CountStar=100 and curr.CountStar=200,
				// that's +50 and +100 executions respectively, so +150 executions for
				// the class metrics.
				d.CountStar += row.CountStar - prevRow.CountStar
				d.SumTimerWait += row.SumTimerWait - prevRow.SumTimerWait
				d.SumLockTime += row.SumLockTime - prevRow.SumLockTime
				d.SumErrors += row.SumErrors - prevRow.SumErrors
				d.SumWarnings += row.SumWarnings - prevRow.SumWarnings
				d.SumRowsAffected += row.SumRowsAffected - prevRow.SumRowsAffected
				d.SumRowsSent += row.SumRowsSent - prevRow.SumRowsSent
				d.SumRowsExamined += row.SumRowsExamined - prevRow.SumRowsExamined
				d.SumCreatedTmpDiskTables += row.SumCreatedTmpDiskTables - prevRow.SumCreatedTmpDiskTables
				d.SumCreatedTmpTables += row.SumCreatedTmpTables - prevRow.SumCreatedTmpTables
				d.SumSelectFullJoin += row.SumSelectFullJoin - prevRow.SumSelectFullJoin
				d.SumSelectFullRangeJoin += row.SumSelectFullRangeJoin - prevRow.SumSelectFullRangeJoin
				d.SumSelectRange += row.SumSelectRange - prevRow.SumSelectRange
				d.SumSelectRangeCheck += row.SumSelectRangeCheck - prevRow.SumSelectRangeCheck
				d.SumSelectScan += row.SumSelectScan - prevRow.SumSelectScan
				d.SumSortMergePasses += row.SumSortMergePasses - prevRow.SumSortMergePasses
				d.SumSortRange += row.SumSortRange - prevRow.SumSortRange
				d.SumSortRows += row.SumSortRows - prevRow.SumSortRows
				d.SumSortScan += row.SumSortScan - prevRow.SumSortScan
				d.SumNoIndexUsed += row.SumNoIndexUsed - prevRow.SumNoIndexUsed
				d.SumNoGoodIndexUsed += row.SumNoGoodIndexUsed - prevRow.SumNoGoodIndexUsed

				// Take the current min and max.
				if row.MinTimerWait < d.MinTimerWait {
					d.MinTimerWait = row.MinTimerWait
				}
				if row.MaxTimerWait > d.MaxTimerWait {
					d.MaxTimerWait = row.MaxTimerWait
				}
				// Add the averages, divide later.
				d.AvgTimerWait += row.AvgTimerWait
			} else {
				// We didn't see this row last time, so the query executed some
				// time during the interval. Since this is our first time seeing
				// it, we don't diff the values, we use the current values.
				d.CountStar = row.CountStar
				d.SumTimerWait = row.SumTimerWait
				d.MinTimerWait = row.MinTimerWait
				d.AvgTimerWait = row.AvgTimerWait
				d.MaxTimerWait = row.MaxTimerWait
				d.SumLockTime = row.SumLockTime
				d.SumErrors = row.SumErrors
				d.SumWarnings = row.SumWarnings
				d.SumRowsAffected = row.SumRowsAffected
				d.SumRowsSent = row.SumRowsSent
				d.SumRowsExamined = row.SumRowsExamined
				d.SumCreatedTmpDiskTables = row.SumCreatedTmpDiskTables
				d.SumCreatedTmpTables = row.SumCreatedTmpTables
				d.SumSelectFullJoin = row.SumSelectFullJoin
				d.SumSelectFullRangeJoin = row.SumSelectFullRangeJoin
				d.SumSelectRange = row.SumSelectRange
				d.SumSelectRangeCheck = row.SumSelectRangeCheck
				d.SumSelectScan = row.SumSelectScan
				d.SumSortMergePasses = row.SumSortMergePasses
				d.SumSortRange = row.SumSortRange
				d.SumSortRows = row.SumSortRows
				d.SumSortScan = row.SumSortScan
				d.SumNoIndexUsed = row.SumNoIndexUsed
				d.SumNoGoodIndexUsed = row.SumNoGoodIndexUsed
			}
			n++
		}

		// Class was in prev, but no rows in prev were in curr, so skip the class.
		if n == 0 {
			continue CLASS_LOOP
		}

		// Divide the total averages to yield the average of the averages.
		// Dividing by n not d.CountStar here is correct because n is the
		// number of query instances in prev and current, so it's also the
		// number of averages we added together. d.CountStar is the total
		// number of times all queries in this classes executed, which can
		// be very high.
		d.AvgTimerWait /= n

		// Create standard metric stats from the class metrics just calculated.
		stats := event.NewMetrics()

		// Time metircs are in picoseconds, so multiply by 10^-12 to convert to seconds.
		stats.TimeMetrics["Query_time"] = &event.TimeStats{
			Sum: float64(d.SumTimerWait) * math.Pow10(-12),
			Min: float64(d.MinTimerWait) * math.Pow10(-12),
			Avg: float64(d.AvgTimerWait) * math.Pow10(-12),
			Max: float64(d.MaxTimerWait) * math.Pow10(-12),
		}

		stats.TimeMetrics["Lock_time"] = &event.TimeStats{
			Sum: float64(d.SumLockTime) * math.Pow10(-12),
		}

		stats.NumberMetrics["Errors"] = &event.NumberStats{Sum: d.SumErrors}
		stats.NumberMetrics["Warnings"] = &event.NumberStats{Sum: d.SumWarnings}
		stats.NumberMetrics["Rows_affected"] = &event.NumberStats{Sum: d.SumRowsAffected}
		stats.NumberMetrics["Rows_sent"] = &event.NumberStats{Sum: d.SumRowsSent}
		stats.NumberMetrics["Rows_examined"] = &event.NumberStats{Sum: d.SumRowsExamined}
		stats.BoolMetrics["Tmp_table_on_disk"] = &event.BoolStats{Sum: d.SumCreatedTmpDiskTables}
		stats.BoolMetrics["Tmp_table"] = &event.BoolStats{Sum: d.SumCreatedTmpTables}
		stats.BoolMetrics["Full_join"] = &event.BoolStats{Sum: d.SumSelectFullJoin}
		stats.NumberMetrics["Select_full_range_join"] = &event.NumberStats{Sum: d.SumSelectFullRangeJoin}
		stats.NumberMetrics["Select_range"] = &event.NumberStats{Sum: d.SumSelectRange}
		stats.NumberMetrics["Select_range_check"] = &event.NumberStats{Sum: d.SumSelectRangeCheck}
		stats.BoolMetrics["Full_scan"] = &event.BoolStats{Sum: d.SumSelectScan}
		stats.NumberMetrics["Merge_passes"] = &event.NumberStats{Sum: d.SumSortMergePasses}
		stats.NumberMetrics["Sort_range"] = &event.NumberStats{Sum: d.SumSortRange}
		stats.NumberMetrics["Sort_rows"] = &event.NumberStats{Sum: d.SumSortRows}
		stats.NumberMetrics["Sort_scan"] = &event.NumberStats{Sum: d.SumSortScan}
		stats.NumberMetrics["No_index_used"] = &event.NumberStats{Sum: d.SumNoIndexUsed}
		stats.NumberMetrics["No_good_index_used"] = &event.NumberStats{Sum: d.SumNoGoodIndexUsed}

		// Create and save the pre-aggregated class.  Using only last 16 digits
		// of checksum is historical: pt-query-digest does the same:
		// my $checksum = uc substr(md5_hex($val), -16);
		// 0 as tzDiff (last param) because we are not saving examples
		class := event.NewClass(classId, class.DigestText, false)
		class.TotalQueries = d.CountStar
		class.Metrics = stats
		classes = append(classes, class)

		// Add the class to the global metrics.
		global.AddClass(class)
	}

	// Each row/class was unique, so update the global counts.
	nClasses := uint64(len(classes))
	if nClasses == 0 {
		return nil, nil
	}

	result := &qan.Result{
		Global: global,
		Class:  classes,
	}

	return result, nil
}
