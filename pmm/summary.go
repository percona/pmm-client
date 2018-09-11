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
	"os"
	"os/exec"
	"time"
	"regexp"
	"strings"
	"io"
	"archive/zip"
	"path/filepath"
)

// Collector parameters and description.
type Collector struct {
        CollectorDescription	string
	ExecCommand		[]string
	OutputFileName		string
}

// CollectData runs a command and collects output into a file.
func (c Collector) CollectData() {
        fmt.Printf("%s ... ",c.CollectorDescription)
	dstfNetworks, err := os.OpenFile(c.OutputFileName, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0777)
	if err != nil {
		fmt.Printf("Skipped. Failed to create file %s with %s\n", c.OutputFileName, err)
        } else {
		defer dstfNetworks.Close()
		cmd, err := exec.Command(c.ExecCommand[0], c.ExecCommand[1:]...).Output()
                if err != nil {
			fmt.Printf("Skipped. Error collecting data by %s: %s\n",c.ExecCommand,err)
                } else {
			if _, err := dstfNetworks.Write(cmd); err != nil {
				fmt.Printf("Skipped. Failed to copy %s output into file %s\n",c.ExecCommand[0],c.OutputFileName)
			} else {
				fmt.Println("Done.")
			}
		}
	}
}

// CheckInitType detects which system manager is running on Linux System.
func CheckInitType() string {
	var initType string
	cmdSystemType, err := exec.Command("stat", "/proc/1/exe").Output()
        if err != nil {
		fmt.Println("Error exec command stat /proc/1/exe:", err)
        } else {
                switch {
			case strings.Contains(string(cmdSystemType),"/sbin/init"):
				initType = "init"
			case strings.Contains(string(cmdSystemType),"/usr/lib/systemd/systemd"):
				initType = "systemd"
			case strings.Contains(string(cmdSystemType),"/sbin/upstart"):
				initType = "upstart"
		}
	}
	return initType
}

// CheckMonitoredDBServices finds out what DB instances are monitored.
func CheckMonitoredDBServices() []string {
        var monitoredDBServices []string
        cmdPmmList, err := exec.Command("pmm-admin","list").Output()
        if err != nil {
                fmt.Println("Error exec pmm-admin list", err)
        } else {
                switch {
                        case strings.Contains(string(cmdPmmList),"mysql:metrics"):
                                monitoredDBServices = append(monitoredDBServices, "mysql")
                        case strings.Contains(string(cmdPmmList),"mongodb:metrics"):
                                monitoredDBServices = append(monitoredDBServices, "mongodb")
                }
        }
        return monitoredDBServices
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
		return nil
	}
	var baseDir string
	if info.IsDir() {
		baseDir = filepath.Base(source)
	}
	filepath.Walk(source, func(path string, info os.FileInfo, err error) error {
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
func (a *Admin) CollectSummary(mysqluser string, mysqlpassword string, mysqlport string, mysqlsocket string) error {
	fmt.Println("\nCollecting information for system diagnostic")
        // Create a directory for collecting files
        currentTime := time.Now().Local()
        cmdHostname, err := exec.Command("hostname").Output()
        if err != nil {
                fmt.Println("Error exec command hostname:", err)
                cmdHostname = []byte("unknown ")
        }

        dirname := strings.Join([]string{"/tmp/pmm", string(cmdHostname)[:len(string(cmdHostname))-1], currentTime.Format("2006-01-02T15_04_05")}, "-")
        err = os.MkdirAll(dirname, 0777)
	if err != nil {
		fmt.Println("Error creating a temporary directory %s: %e", dirname, err)
		os.Exit(1)
	}

        var Collectors = []Collector{{"Collect pmm-admin check-network output",
		[]string{"pmm-admin", "check-network"},
		strings.Join([]string{dirname,strings.Join([]string{"pmm-admin_check-network_",string(cmdHostname)[:len(string(cmdHostname))-1],".txt"},"")},"/")},
                {"Collect pmm-admin list output",
		[]string{"pmm-admin", "list"},
		strings.Join([]string{dirname,strings.Join([]string{"pmm-admin_list_",string(cmdHostname)[:len(string(cmdHostname))-1],".txt"},"")},"/")},
		{"Collect ps output",
                []string{"sh", "-c", "ps aux | grep exporter | grep -v grep"},
                strings.Join([]string{dirname,strings.Join([]string{"ps_exporter_",string(cmdHostname)[:len(string(cmdHostname))-1],".txt"},"")},"/")},
                {"Collect pt-summary output",
                []string{"pt-summary"},
                strings.Join([]string{dirname,strings.Join([]string{"pt-summary_",string(cmdHostname)[:len(string(cmdHostname))-1],".txt"},"")},"/")},
                {"Collect list of open ports",
                []string{"netstat", "-punta"},
                strings.Join([]string{dirname,strings.Join([]string{"netstat_",string(cmdHostname)[:len(string(cmdHostname))-1],".txt"},"")},"/")}}

	initType := CheckInitType()
	switch {
		case (initType == "init"):
			Collectors = append(Collectors, Collector{"Collect service output",
			[]string{"service", "--status-all"},
			strings.Join([]string{dirname,strings.Join([]string{"sysv_service_",string(cmdHostname)[:len(string(cmdHostname))-1],".txt"},"")},"/")})
                case (initType == "systemd"):
                        Collectors = append(Collectors, Collector{"Collect systemctl output",
                        []string{"systemctl", "-l", "status"},
                        strings.Join([]string{dirname,strings.Join([]string{"systemd_",string(cmdHostname)[:len(string(cmdHostname))-1],".txt"},"")},"/")})
                case (initType == "upstart"):
                        Collectors = append(Collectors, Collector{"Collect initctl output",
                        []string{"initctl","list"},
                        strings.Join([]string{dirname,strings.Join([]string{"upstart_",string(cmdHostname)[:len(string(cmdHostname))-1],".txt"},"")},"/")})
	}

	for _, service := range CheckMonitoredDBServices() {
		switch {
			case (service == "mysql"):
				if (len(mysqlsocket)>0) { mysqlsocket = strings.Join([]string{"--socket=",mysqlsocket},"")}
				if (len(mysqluser)>0) { mysqluser = strings.Join([]string{"--user=",mysqluser},"")}
				if (len(mysqlpassword)>0) { mysqlpassword = strings.Join([]string{"--password=",mysqlpassword},"")}
				if (len(mysqlport)>0) { mysqlport = strings.Join([]string{"--port=",mysqlport},"")}
				Collectors = append(Collectors, Collector{"Collect pt-mysql-summary output",
				[]string{"pt-mysql-summary",mysqluser,mysqlpassword,mysqlsocket,mysqlport},
				strings.Join([]string{dirname,strings.Join([]string{"pt-mysql-summary_",string(cmdHostname)[:len(string(cmdHostname))-1],".txt"},"")},"/")})
			case (service == "mongodb"):
				Collectors = append(Collectors, Collector{"Collect pt-mongodb-summary output",
				[]string{"pt-mongodb-summary"},
				strings.Join([]string{dirname,strings.Join([]string{"pt-mongodb-summary_",string(cmdHostname)[:len(string(cmdHostname))-1],".txt"},"")},"/")})
		}
	}

	// Perform exec commands and collect results
        for _, info := range Collectors {
                info.CollectData()
        }

        // Collect pmm logs from /var/log
        pmmLogs, _ := regexp.Compile("^pmm-.*")
        srcf, err := os.Open("/var/log");
        if err != nil {
		fmt.Printf("Failed to open directory %s with %s\n", dirname, err)
        } else {
	defer srcf.Close()
        }
        names, err := srcf.Readdirnames(100)
        for _, name := range names {
		if pmmLogs.MatchString(name) {
                        srcf, err := os.Open(strings.Join([]string{"/var/log/",name},"/"))
                        if err != nil {
                                fmt.Printf("Open file %s failed with %v\n", strings.Join([]string{"/var/log/",name},"/"), err)
                        } else {
                                defer srcf.Close()
                        }
                        dstf, err := os.OpenFile(strings.Join([]string{dirname,name},"/"), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0777)
                        if err != nil {
                                fmt.Printf("Create file %s failed with %v\n", strings.Join([]string{dirname,name},"/"), err)
                        } else {
                                defer dstf.Close()
                        }
                        if _, err := io.Copy(dstf, srcf); err != nil {
                                fmt.Printf("Cannot copy %s to %v: %v\n", name, dstf, err)
                        }
               }
        }

	archFilename := strings.Join([]string{"pmm","-",string(cmdHostname)[:len(string(cmdHostname))-1],"-",currentTime.Format("2006-01-02T15_04_05"),".zip"}, "")
	err = zipIt(dirname, archFilename)
        if err != nil {
                fmt.Printf("Error archiving directory %s: %v", dirname, err)
                os.Exit(1)
        }

	fmt.Printf("\nAll operations have been performed.\nResult file is %s\n", archFilename)

	return nil
}
