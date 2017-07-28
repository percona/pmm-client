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

package mysql

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	dmlVerbs    = []string{"insert", "update", "delete", "replace"}
	updateRe    = regexp.MustCompile(`(?i)^update\s+(?:low_priority|ignore)?\s*(.*?)\s+set\s+(.*?)(?:\s+where\s+(.*?))?(?:\s+limit\s*[0-9]+(?:\s*,\s*[0-9]+)?)?$`)
	deleteRe    = regexp.MustCompile(`(?i)^delete\s+(.*?)\bfrom\s+(.*?)$`)
	insertRe    = regexp.MustCompile(`(?i)^(?:insert(?:\s+ignore)?|replace)\s+.*?\binto\s+(.*?)\(([^\)]+)\)\s*values?\s*\((.*?)\)\s*(?:\slimit\s|on\s+duplicate\s+key.*)?\s*$`)
	insertSetRe = regexp.MustCompile(`(?i)(?:insert(?:\s+ignore)?|replace)\s+(?:.*?\binto)\s+(.*?)\s*set\s+(.*?)\s*(?:\blimit\b|on\s+duplicate\s+key.*)?\s*$`)
)

func IsDMLQuery(query string) bool {
	query = strings.ToLower(strings.TrimSpace(query))
	for _, verb := range dmlVerbs {
		if strings.HasPrefix(query, verb) {
			return true
		}
	}
	return false
}

/*
  MySQL version prior 5.6.3 cannot run explain on DML commands.
  From the doc: http://dev.mysql.com/doc/refman/5.6/en/explain.html
  "As of MySQL 5.6.3, permitted explainable statements for EXPLAIN are
  SELECT, DELETE, INSERT, REPLACE, and UPDATE.
  Before MySQL 5.6.3, SELECT is the only explainable statement."

  This function converts DML queries to the equivalent SELECT to make
  it able to explain DML queries on older MySQL versions
*/
func DMLToSelect(query string) string {
	m := updateRe.FindStringSubmatch(query)
	// > 2 because we need at least a table name and a list of fields
	if len(m) > 2 {
		return updateToSelect(m)
	}

	m = deleteRe.FindStringSubmatch(query)
	if len(m) > 1 {
		return deleteToSelect(m)
	}

	m = insertRe.FindStringSubmatch(query)
	if len(m) > 2 {
		return insertToSelect(m)
	}

	m = insertSetRe.FindStringSubmatch(query)
	if len(m) > 2 {
		return insertWithSetToSelect(m)
	}

	return ""
}

func updateToSelect(matches []string) string {
	matches = matches[1:]
	matches[0], matches[1] = matches[1], matches[0]
	format := []string{"SELECT %s", " FROM %s", " WHERE %s"}
	result := ""
	for i, match := range matches {
		if match != "" {
			result = result + fmt.Sprintf(format[i], match)
		}
	}
	return result
}

func deleteToSelect(matches []string) string {
	if strings.Index(matches[2], "join") > -1 {
		return fmt.Sprintf("SELECT 1 FROM %s", matches[2])
	}
	return fmt.Sprintf("SELECT * FROM %s", matches[2])
}

func insertToSelect(matches []string) string {
	fields := strings.Split(matches[2], ",")
	values := strings.Split(matches[3], ",")
	if len(fields) == len(values) {
		query := fmt.Sprintf("SELECT * FROM %s WHERE ", matches[1])
		sep := ""
		for i := 0; i < len(fields); i++ {
			query = query + fmt.Sprintf(`%s%s=%s`, sep, strings.TrimSpace(fields[i]), values[i])
			sep = " and "
		}
		return query
	}
	return fmt.Sprintf("SELECT * FROM %s LIMIT 1", matches[1])
}

func insertWithSetToSelect(matches []string) string {
	return fmt.Sprintf("SELECT * FROM %s WHERE %s", matches[1], strings.Replace(matches[2], ",", " AND ", -1))
}

func Ident(db, table string) string {
	// Wrap the idents in ` to handle space and weird chars.
	if db != "" {
		db = "`" + db + "`"
	}
	if table != "" {
		table = "`" + table + "`"
	}
	// Join the idents if there's two, else return whichever was given.
	if db != "" && table != "" {
		return db + "." + table
	} else if table != "" {
		return table
	} else {
		return db
	}
}
