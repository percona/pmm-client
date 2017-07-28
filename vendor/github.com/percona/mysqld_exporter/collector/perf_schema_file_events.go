// Scrape `performance_schema.file_summary_by_event_name`.

package collector

import (
	"database/sql"

	"github.com/prometheus/client_golang/prometheus"
)

const perfFileEventsQuery = `
	SELECT
	    EVENT_NAME,
	    COUNT_READ, SUM_TIMER_READ, SUM_NUMBER_OF_BYTES_READ,
	    COUNT_WRITE, SUM_TIMER_WRITE, SUM_NUMBER_OF_BYTES_WRITE,
	    COUNT_MISC, SUM_TIMER_MISC
	  FROM performance_schema.file_summary_by_event_name
	`

// Metric descriptors.
var (
	performanceSchemaFileEventsDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, performanceSchema, "file_events_total"),
		"The total file events by event name/mode.",
		[]string{"event_name", "mode"}, nil,
	)
	performanceSchemaFileEventsTimeDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, performanceSchema, "file_events_seconds_total"),
		"The total seconds of file events by event name/mode.",
		[]string{"event_name", "mode"}, nil,
	)
	performanceSchemaFileEventsBytesDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, performanceSchema, "file_events_bytes_total"),
		"The total bytes of file events by event name/mode.",
		[]string{"event_name", "mode"}, nil,
	)
)

// ScrapePerfFileEvents collects from `performance_schema.file_summary_by_event_name`.
func ScrapePerfFileEvents(db *sql.DB, ch chan<- prometheus.Metric) error {
	// Timers here are returned in picoseconds.
	perfSchemaFileEventsRows, err := db.Query(perfFileEventsQuery)
	if err != nil {
		return err
	}
	defer perfSchemaFileEventsRows.Close()

	var (
		eventName                         string
		countRead, timeRead, bytesRead    uint64
		countWrite, timeWrite, bytesWrite uint64
		countMisc, timeMisc               uint64
	)
	for perfSchemaFileEventsRows.Next() {
		if err := perfSchemaFileEventsRows.Scan(
			&eventName,
			&countRead, &timeRead, &bytesRead,
			&countWrite, &timeWrite, &bytesWrite,
			&countMisc, &timeMisc,
		); err != nil {
			return err
		}
		ch <- prometheus.MustNewConstMetric(
			performanceSchemaFileEventsDesc, prometheus.CounterValue, float64(countRead),
			eventName, "read",
		)
		ch <- prometheus.MustNewConstMetric(
			performanceSchemaFileEventsTimeDesc, prometheus.CounterValue, float64(timeRead)/picoSeconds,
			eventName, "read",
		)
		ch <- prometheus.MustNewConstMetric(
			performanceSchemaFileEventsBytesDesc, prometheus.CounterValue, float64(bytesRead),
			eventName, "read",
		)
		ch <- prometheus.MustNewConstMetric(
			performanceSchemaFileEventsDesc, prometheus.CounterValue, float64(countWrite),
			eventName, "write",
		)
		ch <- prometheus.MustNewConstMetric(
			performanceSchemaFileEventsTimeDesc, prometheus.CounterValue, float64(timeWrite)/picoSeconds,
			eventName, "write",
		)
		ch <- prometheus.MustNewConstMetric(
			performanceSchemaFileEventsBytesDesc, prometheus.CounterValue, float64(bytesWrite),
			eventName, "write",
		)
		ch <- prometheus.MustNewConstMetric(
			performanceSchemaFileEventsDesc, prometheus.CounterValue, float64(countMisc),
			eventName, "misc",
		)
		ch <- prometheus.MustNewConstMetric(
			performanceSchemaFileEventsTimeDesc, prometheus.CounterValue, float64(timeMisc)/picoSeconds,
			eventName, "misc",
		)
	}
	return nil
}
