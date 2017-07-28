# Changelog

## v0.3.0 (2017-07-07)

* Add standard metrics:
  * `mongodb_exporter_scrapes_total`
  * `mongodb_exporter_last_scrape_error`
  * `mongodb_exporter_last_scrape_duration_seconds`
* Fix a few data races.

## v0.2.0 (2017-06-28)

* Default listen port changed to 9216.
* All log messages now go to stderr. Logging flags changed.
* Fewer messages on default INFO logging level.
* Use https://github.com/prometheus/common log for logging instead of https://github.com/golang/glog.
* Use https://github.com/prometheus/common version to build with version information.
* Use https://github.com/prometheus/promu for building.

## v0.1.0

* First tagged version.
