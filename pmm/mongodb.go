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

package pmm

import (
	"fmt"
	"time"

	"gopkg.in/mgo.v2"
)

// DetectMongoDB verify MongoDB connection.
func (a *Admin) DetectMongoDB(uri string) (mgo.BuildInfo, error) {
	dialInfo, err := mgo.ParseURL(uri)
	if err != nil {
		return mgo.BuildInfo{}, fmt.Errorf("Bad MongoDB uri %s: %s", uri, err)
	}

	dialInfo.Direct = true
	dialInfo.Timeout = 10 * time.Second
	session, err := mgo.DialWithInfo(dialInfo)
	if err != nil {
		return mgo.BuildInfo{}, fmt.Errorf("Cannot connect to MongoDB using uri %s: %s", uri, err)
	}
	defer session.Close()

	buildInfo, err := session.BuildInfo()
	if err != nil {
		return mgo.BuildInfo{}, fmt.Errorf("Cannot get buildInfo() for MongoDB using uri %s: %s", uri, err)
	}

	return buildInfo, nil
}
