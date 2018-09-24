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
	"archive/zip"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/percona/kardianos-service"
)

// Collector parameters and description.
type Collector struct {
	CollectorDescription string
	ExecCommand          []string
	OutputFileName       string
}

// CollectData runs a command and collects output into a file.
func (c *Collector) CollectData() error {
	fmt.Printf("%s ... ", c.CollectorDescription)
	dstfNetworks, err := os.OpenFile(c.OutputFileName, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0777)
	if err != nil {
		fmt.Printf("Skipped. Failed to create file %s with %s\n", c.OutputFileName, err)
		return err
	}
	defer dstfNetworks.Close()
	cmd, err := exec.Command(c.ExecCommand[0], c.ExecCommand[1:]...).Output()
	if err != nil {
		fmt.Printf("Skipped. Error collecting data by %s: %s\n", c.ExecCommand, err)
		return err
	}
	if _, err := dstfNetworks.Write(cmd); err != nil {
		fmt.Printf("Skipped. Failed to copy %s output into file %s\n", c.ExecCommand[0], c.OutputFileName)
		return err
	}
	fmt.Println("Done.")

	return err
}

// CheckMonitoredDBServices finds out what DB instances are monitored.
func CheckMonitoredDBServices() []string {
	var monitoredDBServices []string
	cmdPmmList, err := exec.Command("pmm-admin", "list").Output()
	if err != nil {
		fmt.Println("Error exec pmm-admin list", err)
		return nil
	}
	if strings.Contains(string(cmdPmmList), "mysql:metrics") {
		monitoredDBServices = append(monitoredDBServices, "mysql")
	}
	if strings.Contains(string(cmdPmmList), "mongodb:metrics") {
		monitoredDBServices = append(monitoredDBServices, "mongodb")
	}

	return monitoredDBServices
}

// Copy a file for collecting in the final archive
func copyFile(dirname, name string) error {
	srcf, err := os.Open(name)
	if err != nil {
		return err
	}
	defer srcf.Close()
	dstf, err := os.OpenFile(filepath.Join(dirname, filepath.Base(name)), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0777)
	if err != nil {
		return err
	}
	defer dstf.Close()
	if _, err := io.Copy(dstf, srcf); err != nil {
		return err
	}
	return err
}

// zipIt archives collected information.
func zipIt(source, target string) error {
	zipfile, err := os.Create(target)
	if err != nil {
		return err
	}
	defer zipfile.Close()
	archive := zip.NewWriter(zipfile)
	defer archive.Close()
	info, err := os.Stat(source)
	if err != nil {
		return err
	}
	var baseDir string
	if info.IsDir() {
		baseDir = filepath.Base(source)
	}
	err = filepath.Walk(source, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		if baseDir != "" {
			header.Name = filepath.Join(baseDir, strings.TrimPrefix(path, source))
		}
		if info.IsDir() {
			header.Name += "/"
		} else {
			header.Method = zip.Deflate
		}
		writer, err := archive.CreateHeader(header)
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()
		_, err = io.Copy(writer, file)
		return err
	})
	return err
}

// CollectSummary get output of system and pmm utilites.
func (a *Admin) CollectSummary(mysqluser string, mysqlpassword string, mysqlport string, mysqlsocket string, mongodbuser string, mongodbpassword string, mongodbport string) error {
	fmt.Println("\nCollecting information for system diagnostic")
	// Create a directory for collecting files and log file for possible errors
	currentTime := time.Now().Local()
	cmdHostname, err := os.Hostname()
	if err != nil {
		fmt.Println("Error getting hostname:", err)
		cmdHostname = ("unknown")
	}

	dirname := strings.Join([]string{"/tmp/pmm", cmdHostname, currentTime.Format("2006-01-02T15_04_05")}, "-")
	err = os.MkdirAll(dirname, 0777)
	if err != nil {
		fmt.Printf("Error creating a temporary directory %s: %v\n", dirname, err)
		os.Exit(1)
	}

	dstLogInfo, _ := os.OpenFile(filepath.Join(dirname, "pmm-summary.err"), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0777)
	defer dstLogInfo.Close()
	summaryLogger := log.New(dstLogInfo, "", log.LstdFlags)

	var Collectors = []Collector{{"Collect pmm-admin check-network output",
		[]string{"pmm-admin", "check-network"},
		strings.Join([]string{dirname, strings.Join([]string{"pmm-admin_check-network_", cmdHostname, ".txt"}, "")}, "/")},
		{"Collect pmm-admin list output",
			[]string{"pmm-admin", "list"},
			strings.Join([]string{dirname, strings.Join([]string{"pmm-admin_list_", cmdHostname, ".txt"}, "")}, "/")},
		{"Collect ps output",
			[]string{"sh", "-c", "ps aux | grep exporter | grep -v grep"},
			strings.Join([]string{dirname, strings.Join([]string{"ps_exporter_", cmdHostname, ".txt"}, "")}, "/")},
		{"Collect pt-summary output",
			[]string{"pt-summary"},
			strings.Join([]string{dirname, strings.Join([]string{"pt-summary_", cmdHostname, ".txt"}, "")}, "/")},
		{"Collect list of open ports",
			[]string{"netstat", "-punta"},
			strings.Join([]string{dirname, strings.Join([]string{"netstat_", cmdHostname, ".txt"}, "")}, "/")}}

	switch service.Platform() {
	case "linux-upstart":
		Collectors = append(Collectors, Collector{"Collect service output",
			[]string{"service", "--status-all"},
			strings.Join([]string{dirname, strings.Join([]string{"sysv_service_", cmdHostname, ".txt"}, "")}, "/")})
	case "linux-systemd":
		Collectors = append(Collectors, Collector{"Collect systemctl output",
			[]string{"systemctl", "-l", "status"},
			strings.Join([]string{dirname, strings.Join([]string{"systemd_", cmdHostname, ".txt"}, "")}, "/")})
	case "unix-systemv":
		Collectors = append(Collectors, Collector{"Collect initctl output",
			[]string{"initctl", "list"},
			strings.Join([]string{dirname, strings.Join([]string{"upstart_", cmdHostname, ".txt"}, "")}, "/")})
	case "darwin-launchd":
		Collectors = append(Collectors, Collector{"Collect LaunchDaemons output",
			[]string{"launchctl", "bslist"},
			strings.Join([]string{dirname, strings.Join([]string{"launchd_", cmdHostname, ".txt"}, "")}, "/")})
	}

	for _, service := range CheckMonitoredDBServices() {
		switch {
		case (service == "mysql"):
			if len(mysqlsocket) > 0 {
				mysqlsocket = strings.Join([]string{"--socket=", mysqlsocket}, "")
			}
			if len(mysqluser) > 0 {
				mysqluser = strings.Join([]string{"--user=", mysqluser}, "")
			}
			if len(mysqlpassword) > 0 {
				mysqlpassword = strings.Join([]string{"--password=", mysqlpassword}, "")
			}
			if len(mysqlport) > 0 {
				mysqlport = strings.Join([]string{"--port=", mysqlport}, "")
			}
			Collectors = append(Collectors, Collector{"Collect pt-mysql-summary output",
				[]string{"pt-mysql-summary", mysqluser, mysqlpassword, mysqlsocket, mysqlport},
				strings.Join([]string{dirname, strings.Join([]string{"pt-mysql-summary_", cmdHostname, ".txt"}, "")}, "/")})
		case (service == "mongodb"):
			if len(mongodbuser) > 0 {
				mongodbuser = strings.Join([]string{"--username=", mongodbuser}, "")
			}
			if len(mongodbpassword) > 0 {
				mongodbpassword = strings.Join([]string{"--password=", mongodbpassword}, "")
			}
			if len(mongodbport) > 0 {
				mongodbport = strings.Join([]string{"localhost:", mongodbport}, "")
			}
			Collectors = append(Collectors, Collector{"Collect pt-mongodb-summary output",
				[]string{"pt-mongodb-summary", mongodbuser, mongodbpassword, mongodbport},
				strings.Join([]string{dirname, strings.Join([]string{"pt-mongodb-summary_", cmdHostname, ".txt"}, "")}, "/")})
		}
	}

	// Perform exec commands and collect results
	for _, info := range Collectors {
		summaryLogger.Printf("%s - %v\n", info.CollectorDescription, info.CollectData())
	}

	// Collect pmm logs from /var/log
	logsDir := "/var/log"
	srcf, err := os.Open(logsDir)
	if err != nil {
		fmt.Printf("Failed to open directory %s with %s\n", logsDir, err)
		summaryLogger.Println(err)
	} else {
		names, _ := filepath.Glob(filepath.Join(logsDir, "pmm-*"))
		if len(names) > 0 {
			for _, name := range names {
				err := copyFile(dirname, name)
				if err != nil {
					fmt.Printf("Failed %v\n", err)
					summaryLogger.Println(err)
				}
			}
		} else {
			fmt.Println("No PMM logs were detected in", logsDir)
			summaryLogger.Println("No PMM logs were detected in", logsDir)
		}
	}
	defer srcf.Close()

	// Archiving collected files
	archFilename := strings.Join([]string{"pmm", "-", cmdHostname, "-", currentTime.Format("2006-01-02T15_04_05"), ".zip"}, "")
	err = zipIt(dirname, archFilename)
	if err != nil {
		fmt.Printf("Error archiving directory %s: %v", dirname, err)
		os.Exit(1)
	}

	fmt.Printf("\nAll operations have been performed.\nResult file is %s\n", archFilename)

	return nil
}
