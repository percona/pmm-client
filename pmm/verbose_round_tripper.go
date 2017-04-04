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
	"log"
	"net/http"
	"net/http/httputil"
	"regexp"
)

// NewDebugRoundTripper is a wrapper around http.RoundTripper that logs request and response
func NewDebugRoundTripper(parent http.RoundTripper) http.RoundTripper {
	return &verboseRoundTripper{
		parent: parent,
	}
}

type verboseRoundTripper struct {
	parent http.RoundTripper
}

// RoundTrip executes a single HTTP transaction, returning
// a Response for the provided Request and logs response and request with std logger
func (v *verboseRoundTripper) RoundTrip(req *http.Request) (resp *http.Response, err error) {
	resp, err = v.parent.RoundTrip(req)
	log.Printf("request:\n%s", dumpRequest(req))
	log.Printf("response:\n%s", dumpResponse(resp))
	return resp, err
}

// dumpRequest returns string representation of request
func dumpRequest(req *http.Request) string {
	reqDump, err := httputil.DumpRequestOut(req, true)
	if err != nil {
		return fmt.Sprintf("unable to dump request: %s", err)
	}
	return formatDump(reqDump, `> `)

}

// dumpResponse returns string representation of response
func dumpResponse(resp *http.Response) string {
	respDump, err := httputil.DumpResponse(resp, true)
	if err != nil {
		return fmt.Sprintf("unable to dump response: %s", err)
	}
	return formatDump(respDump, `< `)
}

// formatDump prefixes each line of dump with given string and changes \r\n to \n
func formatDump(data []byte, prefix string) string {
	var re1 = regexp.MustCompile(`\r?\n`)
	data = re1.ReplaceAllLiteral(data, []byte("\n"))

	var re2 = regexp.MustCompile(`(?m)^`)
	data = re2.ReplaceAllLiteral(data, []byte(prefix))

	return string(data)
}
