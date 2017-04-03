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

package cmdtest

import (
	"context"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"reflect"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestPmmAdmin(t *testing.T) {
	var err error

	bindir, err := ioutil.TempDir("/tmp", "pmm-client-test-bindir-")
	assert.Nil(t, err)
	defer func() {
		err := os.RemoveAll(bindir)
		assert.Nil(t, err)
	}()

	bin := bindir + "/examplecmd"
	cmd := exec.Command(
		"go",
		"build",
		"-o",
		bin,
		"./examplecmd",
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Run()
	assert.Nil(t, err, "Failed to build: %s", err)

	tests := []func(*testing.T, string){
		testInteractive,
	}
	t.Run("pmm-admin", func(t *testing.T) {
		for _, f := range tests {
			f := f // capture range variable
			fName := runtime.FuncForPC(reflect.ValueOf(f).Pointer()).Name()
			t.Run(fName, func(t *testing.T) {
				// t.Parallel()
				f(t, bin)
			})
		}
	})

}

func testInteractive(t *testing.T, bin string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(
		ctx,
		bin,
	)

	cmdTest := New(cmd)
	if err := cmd.Start(); err != nil {
		log.Fatal(err)
	}
	assert.Equal(t, "This is a test program\n", cmdTest.ReadLine())
	assert.Equal(t, "What's your name?: ", cmdTest.ReadLine())
	cmdTest.Write("Kamil\n")
	assert.Equal(t, "Hi Kamil, it's nice to meet you!\n", cmdTest.ReadLine())

	err := cmd.Wait()
	assert.Nil(t, err)

	assert.Equal(t, []string{}, cmdTest.Output()) // No more data
}
