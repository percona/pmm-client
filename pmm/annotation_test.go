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
	"context"
	"fmt"
	"net/url"
	"testing"
	"time"

	"github.com/percona/pmm-client/pmm/managed"
	"github.com/percona/pmm-client/test/fakeapi"
	"github.com/stretchr/testify/assert"
)

// TestAdmin_AddAnnotation tests add annotation to managed
func TestAdmin_AddAnnotation(t *testing.T) {

	// create fakeapi
	api := fakeapi.New()
	defer api.Close()
	api.AddAnnotation()

	u, _ := url.Parse(api.URL())

	// create pmm-admin instance
	admin := &Admin{}
	insecureFlag := true
	timeout := 1 * time.Second
	debug := false
	admin.qanAPI = NewAPI(insecureFlag, timeout, debug)
	admin.managedAPI = managed.NewClient(u.Host, "http", &url.Userinfo{}, false, true)

	// point pmm-admin to fake http api
	admin.serverURL = u.Host
	scheme := "http"
	authStr := ""
	host := u.Host // u.Host delivers host:port
	admin.serverURL = fmt.Sprintf("%s://%s%s", scheme, authStr, host)

	err := admin.AddAnnotation(context.TODO(), "Description", "tag1, tag2")
	assert.Nil(t, err)
}
