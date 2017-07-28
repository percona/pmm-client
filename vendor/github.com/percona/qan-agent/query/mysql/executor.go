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
	"database/sql"
	"fmt"
	"strings"

	"github.com/percona/qan-agent/mysql"
	"github.com/percona/pmm/proto"
)

type QueryExecutor struct {
	conn mysql.Connector
}

func NewQueryExecutor(conn mysql.Connector) *QueryExecutor {
	e := &QueryExecutor{
		conn: conn,
	}
	return e
}

func (e *QueryExecutor) Explain(db, query string, convert bool) (*proto.ExplainResult, error) {
	explain, err := e.explain(db, query)
	if err != nil {
		// MySQL 5.5 returns syntax error because it doesn't support non-SELECT EXPLAIN.
		// MySQL 5.6 non-SELECT EXPLAIN requires privs for the SQL statement.
		errCode := mysql.MySQLErrorCode(err)
		if convert && (errCode == mysql.ER_SYNTAX_ERROR || errCode == mysql.ER_USER_DENIED) && IsDMLQuery(query) {
			query = DMLToSelect(query)
			if query == "" {
				return nil, fmt.Errorf("cannot convert query to SELECT")
			}
			explain, err = e.explain(db, query) // query converted to SELECT
		}
		if err != nil {
			return nil, err
		}
	}
	return explain, nil
}

func (e *QueryExecutor) TableInfo(tables *proto.TableInfoQuery) (proto.TableInfoResult, error) {
	res := make(proto.TableInfoResult)

	if len(tables.Create) > 0 {
		for _, t := range tables.Create {
			dbTable := t.Db + "." + t.Table
			tableInfo, ok := res[dbTable]
			if !ok {
				res[dbTable] = &proto.TableInfo{}
				tableInfo = res[dbTable]
			}

			def, err := e.showCreate(Ident(t.Db, t.Table))
			if err != nil {
				if tableInfo.Errors == nil {
					tableInfo.Errors = []string{}
				}
				tableInfo.Errors = append(tableInfo.Errors, fmt.Sprintf("SHOW CREATE TABLE %s: %s", t.Table, err))
				continue
			}
			tableInfo.Create = def
		}
	}

	if len(tables.Index) > 0 {
		for _, t := range tables.Index {
			dbTable := t.Db + "." + t.Table
			tableInfo, ok := res[dbTable]
			if !ok {
				res[dbTable] = &proto.TableInfo{}
				tableInfo = res[dbTable]
			}

			indexes, err := e.showIndex(Ident(t.Db, t.Table))
			if err != nil {
				if tableInfo.Errors == nil {
					tableInfo.Errors = []string{}
				}
				tableInfo.Errors = append(tableInfo.Errors, fmt.Sprintf("SHOW INDEX FROM %s: %s", t.Table, err))
				continue
			}
			tableInfo.Index = indexes
		}
	}

	if len(tables.Status) > 0 {
		for _, t := range tables.Status {
			dbTable := t.Db + "." + t.Table
			tableInfo, ok := res[dbTable]
			if !ok {
				res[dbTable] = &proto.TableInfo{}
				tableInfo = res[dbTable]
			}

			// SHOW TABLE STATUS does not accept db.tbl so pass them separately,
			// and tbl is used in LIKE so it's not an ident.
			status, err := e.showStatus(Ident(t.Db, ""), t.Table)
			if err != nil {
				if tableInfo.Errors == nil {
					tableInfo.Errors = []string{}
				}
				tableInfo.Errors = append(tableInfo.Errors, fmt.Sprintf("SHOW TABLE STATUS FROM %s LIKE %s: %s", t.Db, t.Table, err))
				continue
			}
			tableInfo.Status = status
		}
	}

	return res, nil
}

// --------------------------------------------------------------------------

func (e *QueryExecutor) explain(db, query string) (*proto.ExplainResult, error) {
	// Transaction because we need to ensure USE and EXPLAIN are run in one connection
	tx, err := e.conn.DB().Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	// If the query has a default db, use it; else, all tables need to be db-qualified
	// or EXPLAIN will throw an error.
	if db != "" {
		_, err := tx.Exec(fmt.Sprintf("USE %s", db))
		if err != nil {
			return nil, err
		}
	}

	classicExplain, err := e.classicExplain(tx, query)
	if err != nil {
		return nil, err
	}

	jsonExplain, err := e.jsonExplain(tx, query)
	if err != nil {
		return nil, err
	}

	explain := &proto.ExplainResult{
		Classic: classicExplain,
		JSON:    jsonExplain,
	}

	return explain, nil
}

func (e *QueryExecutor) classicExplain(tx *sql.Tx, query string) (classicExplain []*proto.ExplainRow, err error) {
	// Partitions are introduced since MySQL 5.1
	// We can simply run EXPLAIN /*!50100 PARTITIONS*/ to get this column when it's available
	// without prior check for MySQL version.
	rows, err := tx.Query(fmt.Sprintf("EXPLAIN /*!50100 PARTITIONS*/ %s", query))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// Go rows.Scan() expects exact number of columns
	// so when number of columns is undefined then the easiest way to
	// overcome this problem is to count received number of columns
	// With 'partitions' it is 11 columns
	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	hasPartitions := len(columns) == 11

	for rows.Next() {
		explainRow := &proto.ExplainRow{}
		if hasPartitions {
			err = rows.Scan(
				&explainRow.Id,
				&explainRow.SelectType,
				&explainRow.Table,
				&explainRow.Partitions, // Since MySQL 5.1
				&explainRow.Type,
				&explainRow.PossibleKeys,
				&explainRow.Key,
				&explainRow.KeyLen,
				&explainRow.Ref,
				&explainRow.Rows,
				&explainRow.Extra,
			)
		} else {
			err = rows.Scan(
				&explainRow.Id,
				&explainRow.SelectType,
				&explainRow.Table,
				&explainRow.Type,
				&explainRow.PossibleKeys,
				&explainRow.Key,
				&explainRow.KeyLen,
				&explainRow.Ref,
				&explainRow.Rows,
				&explainRow.Extra,
			)
		}
		if err != nil {
			return nil, err
		}
		classicExplain = append(classicExplain, explainRow)
	}
	err = rows.Err()
	if err != nil {
		return nil, err
	}

	return classicExplain, nil
}

func (e *QueryExecutor) jsonExplain(tx *sql.Tx, query string) (string, error) {
	// EXPLAIN in JSON format is introduced since MySQL 5.6.5
	ok, err := e.conn.AtLeastVersion("5.6.5")
	if !ok || err != nil {
		return "", err
	}

	explain := ""
	err = tx.QueryRow(fmt.Sprintf("EXPLAIN FORMAT=JSON %s", query)).Scan(&explain)
	if err != nil {
		return "", err
	}

	return explain, nil
}

func (e *QueryExecutor) showCreate(dbTable string) (string, error) {
	// Result from SHOW CREATE TABLE includes two columns, "Table" and
	// "Create Table", we ignore the first one as we need only "Create Table".
	var tableName string
	var tableDef string
	err := e.conn.DB().QueryRow("SHOW CREATE TABLE "+dbTable).Scan(&tableName, &tableDef)
	return tableDef, err
}

func (e *QueryExecutor) showIndex(dbTable string) (map[string][]proto.ShowIndexRow, error) {
	rows, err := e.conn.DB().Query("SHOW INDEX FROM " + dbTable)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	hasIndexComment := len(columns) == 13 // added in MySQL 5.5

	indexes := map[string][]proto.ShowIndexRow{} // keyed on KeyName
	prevKeyName := ""
	for rows.Next() {
		indexRow := proto.ShowIndexRow{}
		if hasIndexComment {
			err = rows.Scan(
				&indexRow.Table,
				&indexRow.NonUnique,
				&indexRow.KeyName,
				&indexRow.SeqInIndex,
				&indexRow.ColumnName,
				&indexRow.Collation,
				&indexRow.Cardinality,
				&indexRow.SubPart,
				&indexRow.Packed,
				&indexRow.Null,
				&indexRow.IndexType,
				&indexRow.Comment,
				&indexRow.IndexComment,
			)
		} else {
			err = rows.Scan(
				&indexRow.Table,
				&indexRow.NonUnique,
				&indexRow.KeyName,
				&indexRow.SeqInIndex,
				&indexRow.ColumnName,
				&indexRow.Collation,
				&indexRow.Cardinality,
				&indexRow.SubPart,
				&indexRow.Packed,
				&indexRow.Null,
				&indexRow.IndexType,
				&indexRow.Comment,
			)
		}
		if err != nil {
			return nil, err
		}
		if indexRow.KeyName != prevKeyName {
			indexes[indexRow.KeyName] = []proto.ShowIndexRow{}
			prevKeyName = indexRow.KeyName
		}
		indexes[indexRow.KeyName] = append(indexes[indexRow.KeyName], indexRow)
	}
	err = rows.Err()
	if err != nil {
		return nil, err
	}

	return indexes, nil
}

func (e *QueryExecutor) showStatus(db, table string) (*proto.ShowTableStatus, error) {
	// Escape _ in the table name because it's a wildcard in LIKE.
	table = strings.Replace(table, "_", "\\_", -1)
	status := &proto.ShowTableStatus{}
	err := e.conn.DB().QueryRow(fmt.Sprintf("SHOW TABLE STATUS FROM %s LIKE '%s'", db, table)).Scan(
		&status.Name,
		&status.Engine,
		&status.Version,
		&status.RowFormat,
		&status.Rows,
		&status.AvgRowLength,
		&status.DataLength,
		&status.MaxDataLength,
		&status.IndexLength,
		&status.DataFree,
		&status.AutoIncrement,
		&status.CreateTime,
		&status.UpdateTime,
		&status.CheckTime,
		&status.Collation,
		&status.Checksum,
		&status.CreateOptions,
		&status.Comment,
	)
	return status, err
}
