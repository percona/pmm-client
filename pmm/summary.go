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
	"archive/tar"
	"compress/gzip"
	"errors"
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
	dstf, err := os.Create(c.OutputFileName)
	if err != nil {
		fmt.Printf("Skipped. Failed to create file %s with %s\n", c.OutputFileName, err)
		return err
	}
	defer dstf.Close()
	cmd := exec.Command(c.ExecCommand[0], c.ExecCommand[1:]...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Printf("%s\n", string(output))
		return errors.New(string(output))
	}
	if _, err := dstf.Write(output); err != nil {
		fmt.Printf("Skipped. Failed to copy %s output into file %s\n", c.ExecCommand[0], c.OutputFileName)
		return err
	}
	err = errors.New("Done")
	fmt.Println(err)

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

// Copy a file for collecting into the final archive
func copyFile(dirname, name string) error {
	srcf, err := os.Open(name)
	if err != nil {
		return fmt.Errorf("copy %s to %s: %v", name, dirname, err)
	}
	defer srcf.Close()

	dstf, err := os.Create(filepath.Join(dirname, filepath.Base(name)))
	if err != nil {
		return fmt.Errorf("copy %s to %s: %v", name, dirname, err)
	}

	if _, err := io.Copy(dstf, srcf); err != nil {
		dstf.Close()
		os.Remove(dstf.Name())
		return fmt.Errorf("copy %s to %s: %v", name, dirname, err)
	}

	if err := dstf.Close(); err != nil {
		os.Remove(dstf.Name())
		return fmt.Errorf("copy %s to %s: %v", name, dirname, err)
	}

	return nil
}

// tarIt archives collected information.
func tarIt(source, target string) error {
	dir, err := os.Open(source)
	if err != nil {
		return err
	}
	defer dir.Close()

	files, err := dir.Readdir(0)
	if err != nil {
		return err
	}

	tarfile, err := os.Create(target)
	if err != nil {
		return err
	}
	defer tarfile.Close()

	var fileWriter io.WriteCloser = tarfile
	fileWriter = gzip.NewWriter(tarfile)
	defer fileWriter.Close()

	tarfileWriter := tar.NewWriter(fileWriter)
	defer tarfileWriter.Close()

	for _, fileInfo := range files {
		if fileInfo.IsDir() {
			continue
		}

		file, err := os.Open(dir.Name() + string(filepath.Separator) + fileInfo.Name())
		if err != nil {
			return err
		}
		defer file.Close()

		header := new(tar.Header)
		header.Name = filepath.Base(file.Name())
		header.Size = fileInfo.Size()
		header.Mode = int64(fileInfo.Mode())
		header.ModTime = fileInfo.ModTime()

		err = tarfileWriter.WriteHeader(header)
		if err != nil {
			return err
		}

		_, err = io.Copy(tarfileWriter, file)
		if err != nil {
			return err
		}
	}

	return err
}

// CollectSummary get output of system and pmm utilites.
func (a *Admin) CollectSummary() error {
	fmt.Println("\nCollecting information for system diagnostic")
	// Create a directory for collecting files and log file for possible errors
	currentTime := time.Now()
	cmdHostname, err := os.Hostname()
	if err != nil {
		fmt.Println("Error getting hostname:", err)
		cmdHostname = "unknown"
	}

	dirname := filepath.Join("/tmp", strings.Join([]string{"pmm", cmdHostname, currentTime.Format("2006-01-02T15_04_05")}, "-"))
	err = os.MkdirAll(dirname, 0755)
	if err != nil {
		fmt.Printf("Error creating a temporary directory %s: %v\n", dirname, err)
		os.Exit(1)
	}

	dstLogInfo, err := os.Create(filepath.Join(dirname, "pmm-summary.err"))
	if err != nil {
		fmt.Printf("Error create pmm-summary.err file: %v\n", err)
		os.Exit(1)
	}
	defer dstLogInfo.Close()
	summaryLogger := log.New(dstLogInfo, "", log.LstdFlags)

	var Collectors = []Collector{{"Collect pmm-admin check-network output",
		[]string{"pmm-admin", "check-network"},
		filepath.Join(dirname, strings.Join([]string{"pmm-admin_check-network_", cmdHostname, ".txt"}, ""))},
		{"Collect pmm-admin list output",
			[]string{"pmm-admin", "list"},
			filepath.Join(dirname, strings.Join([]string{"pmm-admin_list_", cmdHostname, ".txt"}, ""))},
		{"Collect ps output",
			[]string{"sh", "-c", "ps aux | grep exporter | grep -v grep"},
			filepath.Join(dirname, strings.Join([]string{"ps_exporter_", cmdHostname, ".txt"}, ""))},
		{"Collect pt-summary output",
			[]string{"pt-summary"},
			filepath.Join(dirname, strings.Join([]string{"pt-summary_", cmdHostname, ".txt"}, ""))},
		{"Collect list of open ports",
			[]string{"netstat", "-punta"},
			filepath.Join(dirname, strings.Join([]string{"netstat_", cmdHostname, ".txt"}, ""))}}

	switch service.Platform() {
	case "linux-upstart":
		Collectors = append(Collectors, Collector{"Collect service output",
			[]string{"service", "--status-all"},
			filepath.Join(dirname, strings.Join([]string{"sysv_service_", cmdHostname, ".txt"}, ""))})
	case "linux-systemd":
		Collectors = append(Collectors, Collector{"Collect systemctl output",
			[]string{"systemctl", "-l", "status"},
			filepath.Join(dirname, strings.Join([]string{"systemd_", cmdHostname, ".txt"}, ""))})
	case "unix-systemv":
		Collectors = append(Collectors, Collector{"Collect initctl output",
			[]string{"initctl", "list"},
			filepath.Join(dirname, strings.Join([]string{"upstart_", cmdHostname, ".txt"}, ""))})
	case "darwin-launchd":
		Collectors = append(Collectors, Collector{"Collect LaunchDaemons output",
			[]string{"launchctl", "bslist"},
			filepath.Join(dirname, strings.Join([]string{"launchd_", cmdHostname, ".txt"}, ""))})
	}

	for _, service := range CheckMonitoredDBServices() {
		switch service {
		case "mysql":
			Collectors = append(Collectors, Collector{"Collect pt-mysql-summary output",
				[]string{"pt-mysql-summary"},
				filepath.Join(dirname, strings.Join([]string{"pt-mysql-summary_", cmdHostname, ".txt"}, ""))})
		case "mongodb":
			Collectors = append(Collectors, Collector{"Collect pt-mongodb-summary output",
				[]string{"pt-mongodb-summary"},
				filepath.Join(dirname, strings.Join([]string{"pt-mongodb-summary_", cmdHostname, ".txt"}, ""))})
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
		defer srcf.Close()
		names, err := filepath.Glob(filepath.Join(logsDir, "pmm-*"))
		if err != nil {
			fmt.Printf("Getting list of  PMM logs in %s failed/n", logsDir)
			summaryLogger.Println("No PMM logs were detected in", logsDir)
		}
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

	// Archiving collected files
	archFilename := strings.Join([]string{"summary", cmdHostname, currentTime.Format("2006-01-02T15_04_05") + ".tar.gz"}, "_")
	err = tarIt(dirname, archFilename)
	if err != nil {
		fmt.Printf("Error archiving directory %s: %v", dirname, err)
		os.Exit(1)
	}

	fmt.Printf("\nData collection complete.  Please attach file %s to the issue as requested by Percona Support.\n", archFilename)

	return nil
}
