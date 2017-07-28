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

package pct

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/hashicorp/go-version"
)

func FileSize(fileName string) (int64, error) {
	stat, err := os.Stat(fileName)
	if err != nil {
		return -1, err
	}
	return stat.Size(), nil
}

func SameFile(file1, file2 string) (bool, error) {
	var err error

	stat1, err := os.Stat(file1)
	if err != nil {
		return false, err
	}

	stat2, err := os.Stat(file2)
	if err != nil {
		return false, err
	}

	return os.SameFile(stat1, stat2), nil
}

func MakeDir(dir string) error {
	err := os.MkdirAll(dir, 0755)
	if os.IsExist(err) {
		return nil
	}
	return err
}

func RemoveFile(file string) error {
	if file != "" {
		err := os.Remove(file)
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return nil
}

func FileExists(file string) bool {
	_, err := os.Stat(file)
	if err == nil {
		return true
	}
	if os.IsNotExist(err) {
		return false
	}
	return true
}

func Mbps(bytes uint64, seconds float64) string {
	if seconds == 0 {
		return "0.00"
	}
	bits := bytes * 8
	return fmt.Sprintf("%.2f", (float64(bits)/1000000)/seconds)
}

// http://en.wikipedia.org/wiki/Metric_prefix
var siPrefix []string = []string{"", "k", "M", "G", "T"}

func Bytes(bytes uint64) string {
	if bytes == 0 {
		return "0"
	}
	prefix := ""
	switch {
	case bytes >= 1000000000000:
		prefix = "T"
	case bytes >= 1000000000:
		prefix = "G"
	case bytes >= 1000000:
		prefix = "M"
	case bytes >= 1000:
		prefix = "k"
	}
	f := float64(bytes)
	for f > 1000 {
		f /= 1000
	}
	return fmt.Sprintf("%.2f %sB", f, prefix)
}

func Duration(s float64) string {
	if s == 0 {
		return "0"
	}
	if s < 0.001 {
		s *= 1000000
		return fmt.Sprintf("%dµ", int(s))
	}
	if s < 1 {
		s *= 1000
		return fmt.Sprintf("%dms", int(s))
	}
	d := ""
	// days
	n := 0
	for s >= 86400 {
		n++
		s -= 86400
	}
	if n > 0 {
		d = fmt.Sprintf("%dd", n)
	}
	// hours
	n = 0
	for s >= 3600 {
		n++
		s -= 3600
	}
	if n > 0 || d != "" {
		d += fmt.Sprintf("%dh", n)
	}
	// minutes
	n = 0
	for s >= 60 {
		n++
		s -= 60
	}
	if n > 0 || d != "" {
		d += fmt.Sprintf("%dm", n)
	}
	// seconds
	n = 0
	x := s
	for x >= 1 {
		n++
		x -= 1
	}
	if x > 0 {
		d += fmt.Sprintf("%.3f", s)
		d = strings.TrimRight(d, "0")
		d = strings.TrimSuffix(d, ".")
	} else {
		d += fmt.Sprintf("%d", n)
	}
	d += "s"
	return d
}

func TimeString(t time.Time) string {
	return t.UTC().Format("2006-01-02 15:04:05 MST")
}

func AtLeastVersion(v1, v2 string) (bool, error) {
	re := regexp.MustCompile("-.*$")
	v1 = re.ReplaceAllString(v1, "") // Strip everything after the first dash
	v2 = re.ReplaceAllString(v2, "") // Strip everything after the first dash
	v, err := version.NewVersion(v1)
	if err != nil {
		return false, err
	}
	constraints, err := version.NewConstraint(">= " + v2)
	if err != nil {
		return false, err
	}
	return constraints.Check(v), nil
}

func ToBool(s string) bool {
	s = strings.ToLower(s)
	switch s {
	case "yes", "true", "on":
		return true
	default:
		return false
	}
}
