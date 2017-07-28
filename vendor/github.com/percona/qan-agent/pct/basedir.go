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
	"encoding/json"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
)

const (
	DEFAULT_BASEDIR      = "/usr/local/percona/qan-agent"
	CONFIG_FILE_SUFFIX   = ".conf"
	INSTANCE_FILE_SUFFIX = ".json"
	// Relative to Basedir.path:
	CONFIG_DIR   = "config"
	INSTANCE_DIR = "instance"
	DATA_DIR     = "data"
	BIN_DIR      = "bin"
	TRASH_DIR    = "trash"
	START_LOCK   = "start.lock"
	START_SCRIPT = "start.sh"
)

type basedir struct {
	path        string
	configDir   string
	instanceDir string
	dataDir     string
	binDir      string
	trashDir    string
}

var Basedir basedir

func (b *basedir) Init(path string) error {
	var err error
	b.path, err = filepath.Abs(path)
	if err != nil {
		return err
	}

	if err := MakeDir(b.path); err != nil && !os.IsExist(err) {
		return err
	}

	b.configDir = filepath.Join(b.path, CONFIG_DIR)
	if err := MakeDir(b.configDir); err != nil && !os.IsExist(err) {
		return err
	}
	if err := os.Chmod(b.configDir, 0700); err != nil {
		return err
	}

	b.instanceDir = filepath.Join(b.path, INSTANCE_DIR)
	if err := MakeDir(b.instanceDir); err != nil && !os.IsExist(err) {
		return err
	}
	if err := os.Chmod(b.instanceDir, 0700); err != nil {
		return err
	}

	b.dataDir = filepath.Join(b.path, DATA_DIR)
	if err := MakeDir(b.dataDir); err != nil && !os.IsExist(err) {
		return err
	}

	b.binDir = filepath.Join(b.path, BIN_DIR)
	if err := MakeDir(b.binDir); err != nil && !os.IsExist(err) {
		return err
	}

	b.trashDir = filepath.Join(b.path, TRASH_DIR)
	if err := MakeDir(b.trashDir); err != nil && !os.IsExist(err) {
		return err
	}

	return nil
}

func (b *basedir) Path() string {
	return b.path
}

func (b *basedir) Dir(service string) string {
	switch service {
	case "config":
		return b.configDir
	case "instance":
		return b.instanceDir
	case "data":
		return b.dataDir
	case "bin":
		return b.binDir
	case "trash":
		return b.trashDir
	default:
		log.Panic("Invalid service: " + service)
	}
	return ""
}

func (b *basedir) File(file string) string {
	switch file {
	case "start-lock":
		file = START_LOCK
	case "start-script":
		file = START_SCRIPT
	default:
		log.Panicf("Unknown basedir file: %s", file)
	}
	return filepath.Join(b.Path(), file)
}

func (b *basedir) ConfigFile(service string) string {
	return filepath.Join(b.configDir, service+CONFIG_FILE_SUFFIX)
}

func (b *basedir) InstanceFile(uuid string) string {
	return filepath.Join(b.instanceDir, uuid+INSTANCE_FILE_SUFFIX)
}

func (b *basedir) ReadConfig(name string, v interface{}) (string, error) {
	return b.readFile(b.ConfigFile(name), v)
}

func (b *basedir) WriteConfig(name string, v interface{}) error {
	return b.writeFile(b.ConfigFile(name), v)
}

func (b *basedir) RemoveConfig(service string) error {
	configFile := filepath.Join(b.configDir, service+CONFIG_FILE_SUFFIX)
	return RemoveFile(configFile)
}

func (b *basedir) WriteConfigString(service, config string) error {
	configFile := filepath.Join(b.configDir, service+CONFIG_FILE_SUFFIX)
	return ioutil.WriteFile(configFile, []byte(config), 0600)
}

func (b *basedir) ReadInstance(uuid string, v interface{}) error {
	_, err := b.readFile(b.InstanceFile(uuid), v)
	return err
}

// Given a service string and a config this method will serialize the config
// to JSON and store the result in a file with composite name <service>-<uuid>.conf
func (b *basedir) WriteInstance(uuid string, v interface{}) error {
	return b.writeFile(b.InstanceFile(uuid), v)
}

// --------------------------------------------------------------------------

func (b *basedir) readFile(file string, v interface{}) (string, error) {
	data, err := ioutil.ReadFile(file)
	if err != nil && !os.IsNotExist(err) {
		return "", err // error isn't "file not found"
	}
	if len(data) > 0 {
		err = json.Unmarshal(data, &v)
	}
	return string(data), err
}

func (b *basedir) writeFile(filePath string, config interface{}) error {
	data, err := json.MarshalIndent(config, "", "    ")
	if err != nil {
		return err
	}
	return ioutil.WriteFile(filePath, data, 0600)
}
