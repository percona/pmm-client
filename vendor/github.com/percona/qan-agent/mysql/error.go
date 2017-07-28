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
	"net"

	"github.com/go-sql-driver/mysql"
)

func MySQLErrorCode(err error) uint16 {
	if val, ok := err.(*mysql.MySQLError); ok {
		return val.Number
	}

	return 0 // not a mysql error
}

func FormatError(err error) string {
	switch err.(type) {
	case *net.OpError:
		e := err.(*net.OpError)
		if e.Op == "dial" {
			return fmt.Sprintf("%s: %s", e.Err, e.Addr)
		}
	}
	return fmt.Sprintf("%s", err)
}

// MySQL error codes
const (
	ER_SPECIFIC_ACCESS_DENIED_ERROR = 1227
	ER_SYNTAX_ERROR                 = 1064
	ER_USER_DENIED                  = 1142
)
