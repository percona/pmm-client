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

package test

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/percona/pmm/proto"
)

var RootDir string

func init() {
	log.SetFlags(log.Ltime | log.Lmicroseconds | log.Lshortfile)

	_, filename, _, _ := runtime.Caller(1)
	dir := filepath.Dir(filename)

	for i := 0; i < 3; i++ {
		dir = dir + "/../"
		if FileExists(dir + ".git") {
			RootDir = filepath.Clean(dir + "agent/test")
			break
		}
	}
	if RootDir == "" {
		log.Panic("Cannot find repo root dir")
	}
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

func GetStatus(sendChan chan *proto.Cmd, recvChan chan *proto.Reply) map[string]string {
	statusCmd := &proto.Cmd{
		Ts:   time.Now(),
		User: "user",
		Cmd:  "Status",
	}
	sendChan <- statusCmd

	select {
	case reply := <-recvChan:
		status := make(map[string]string)
		if err := json.Unmarshal(reply.Data, &status); err != nil {
			// This shouldn't happen.
			log.Fatal(err)
		}
		return status
	case <-time.After(100 * time.Millisecond):
	}

	return map[string]string{}
}

func WriteData(data interface{}, filename string) {
	bytes, _ := json.MarshalIndent(data, "", " ")
	bytes = append(bytes, 0x0A) // newline
	ioutil.WriteFile(filename, bytes, os.ModePerm)
}

func DrainLogChan(c chan *proto.LogEntry) {
	for {
		select {
		case <-c:
		default:
			return
		}
	}
}

func DrainSendChan(c chan *proto.Cmd) {
	for {
		select {
		case <-c:
		default:
			return
		}
	}
}

func DrainRecvChan(c chan *proto.Reply) {
	for {
		select {
		case <-c:
		default:
			return
		}
	}
}

func DrainTraceChan(c chan string) []string {
	trace := []string{}
DRAIN:
	for {
		select {
		case funcCalled := <-c:
			trace = append(trace, funcCalled)
		default:
			break DRAIN
		}
	}
	return trace
}

func DrainBoolChan(c chan bool) {
	for {
		select {
		case <-c:
		default:
			return
		}
	}
}

func DrainRecvData(c chan interface{}) {
	for {
		select {
		case <-c:
		default:
			return
		}
	}
}

func DrainDataChan(c chan []byte) {
	for {
		select {
		case <-c:
		default:
			return
		}
	}
}

func FileSize(fileName string) (int64, error) {
	stat, err := os.Stat(fileName)
	if err != nil {
		return -1, err
	}
	return stat.Size(), nil
}

func Dump(v interface{}) {
	bytes, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		fmt.Printf("Error dumping data struct: %s\n", err)
	}
	fmt.Println(string(bytes))
}

func Debug(logChan chan *proto.LogEntry) {
	for l := range logChan {
		log.Println(l)
	}
}

func CopyFile(src, dst string) error {
	cmd := exec.Command("cp", src, dst)
	return cmd.Run()
}

func Sign(file string) ([]byte, []byte, error) {
	data, err := ioutil.ReadFile(file)
	if err != nil {
		return nil, nil, err
	}

	key, err := privkey(RootDir + "/pct/key.pem")
	if err != nil {
		return nil, nil, fmt.Errorf("privkey: %s", err)
	}

	h := sha256.New()
	h.Write(data)

	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, h.Sum(nil))
	if err != nil {
		return nil, nil, err
	}
	return data, sig, nil
}

func privkey(file string) (key *rsa.PrivateKey, err error) {
	buf, err := ioutil.ReadFile(file)
	if err != nil {
		return nil, err
	}

	p, _ := pem.Decode(buf)
	if p == nil {
		return nil, fmt.Errorf("Invalid private key")
	}

	der, err := x509.DecryptPEMBlock(p, []byte("percona cloud tools"))
	if err != nil {
		return nil, fmt.Errorf("x509.DecryptPEMBlock: %s", err)
	}

	return x509.ParsePKCS1PrivateKey(der)
}

func ClearDir(path ...string) error {
	dir := filepath.Join(path...)
	files, err := filepath.Glob(dir)
	if err != nil {
		return err
	}
	for _, file := range files {
		if err := os.Remove(file); err != nil {
			return err
		}
	}
	return nil
}

func LoadReport(file string, v interface{}) error {
	bytes, err := ioutil.ReadFile(file)
	if err != nil {
		return err
	}
	if err = json.Unmarshal(bytes, v); err != nil {
		return err
	}
	return nil
}
