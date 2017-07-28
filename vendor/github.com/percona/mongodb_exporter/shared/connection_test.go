// Copyright 2017 Percona LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//   http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package shared

import (
	"testing"
)

func TestRedactMongoUri(t *testing.T) {
	uri := "mongodb://mongodb_exporter:s3cr3tpassw0rd@localhost:27017"
	expected := "mongodb://****:****@localhost:27017"
	actual := RedactMongoUri(uri)
	if expected != actual {
		t.Errorf("%q != %q", expected, actual)
	}
}
