# Percona Monitoring and Management (PMM) Client v1.x

[![Build Status](https://travis-ci.com/percona/pmm-client.svg?branch=master)](https://travis-ci.com/percona/pmm-client)
[![Go Report Card](https://goreportcard.com/badge/github.com/percona/pmm-client)](https://goreportcard.com/report/github.com/percona/pmm-client) [![CLA assistant](https://cla-assistant.percona.com/readme/badge/percona/pmm-client)](https://cla-assistant.percona.com/percona/pmm-client)

See the [PMM docs](https://www.percona.com/doc/percona-monitoring-and-management/1.x/index.html) for more information.


## Submitting Bug Reports

If you find a bug in Percona PMM Client or one of the related projects, you should submit a report to that project's [JIRA](https://jira.percona.com) issue tracker.

Your first step should be [to search](https://jira.percona.com/issues/?jql=project%20%3D%20PMM%20AND%20component%20%3D%20%22PMM%20Client%22) the existing set of open tickets for a similar report. If you find that someone else has already reported your problem, then you can upvote that report to increase its visibility.

If there is no existing report, submit a report following these steps:

1. [Sign in to Percona JIRA.](https://jira.percona.com/login.jsp) You will need to create an account if you do not have one.
2. [Go to the Create Issue screen and select the relevant project.](https://jira.percona.com/secure/CreateIssueDetails!init.jspa?pid=11600&issuetype=1&priority=3&components=11308)
3. Fill in the fields of Summary, Description, Steps To Reproduce, and Affects Version to the best you can. If the bug corresponds to a crash, attach the stack trace from the logs.

An excellent resource is [Elika Etemad's article on filing good bug reports.](http://fantasai.inkedblade.net/style/talks/filing-good-bugs/).

As a general rule of thumb, please try to create bug reports that are:

- *Reproducible.* Include steps to reproduce the problem.
- *Specific.* Include as much detail as possible: which version, what environment, etc.
- *Unique.* Do not duplicate existing tickets.
- *Scoped to a Single Bug.* One bug per report.
