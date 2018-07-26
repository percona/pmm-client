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
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"os"
	"regexp"
	"strings"

	consul "github.com/hashicorp/consul/api"
	"github.com/percona/pmm/proto"
	protocfg "github.com/percona/pmm/proto/config"
	"gopkg.in/yaml.v2"
)

// Config pmm.yml config file.
type Config struct {
	ServerAddress     string `yaml:"server_address"`
	ClientAddress     string `yaml:"client_address"`
	BindAddress       string `yaml:"bind_address"`
	ClientName        string `yaml:"client_name"`
	MySQLPassword     string `yaml:"mysql_password,omitempty"`
	ServerUser        string `yaml:"server_user,omitempty"`
	ServerPassword    string `yaml:"server_password,omitempty"`
	ServerSSL         bool   `yaml:"server_ssl,omitempty"`
	ServerInsecureSSL bool   `yaml:"server_insecure_ssl,omitempty"`
}

// LoadConfig read PMM client config file.
func (a *Admin) LoadConfig() error {
	a.Config = &Config{}
	if !FileExists(ConfigFile) {
		return nil
	}
	bytes, err := ioutil.ReadFile(ConfigFile)
	if err != nil {
		return err
	}
	if err := yaml.Unmarshal(bytes, a.Config); err != nil {
		return err
	}

	// If not set previously, assume it equals to client address.
	if a.Config.BindAddress == "" {
		a.Config.BindAddress = a.Config.ClientAddress
	}
	return nil
}

// SetConfig configure PMM client, check connectivity and write the config.
func (a *Admin) SetConfig(cf Config, flagForce bool) error {
	// Server options.
	if cf.ServerSSL && cf.ServerInsecureSSL {
		return errors.New("Flags --server-ssl and --server-insecure-ssl are mutually exclusive.")
	}

	if cf.ServerAddress != "" {
		a.Config.ServerAddress = cf.ServerAddress
		// Resetting server address clears up SSL and HTTP auth.
		a.Config.ServerSSL = false
		a.Config.ServerInsecureSSL = false
		a.Config.ServerUser = ""
		a.Config.ServerPassword = ""
	}
	if a.Config.ServerAddress == "" {
		return errors.New("Server address is not set. Use --server flag to set it.")
	}

	if cf.ServerPassword != "" {
		a.Config.ServerUser = cf.ServerUser
		a.Config.ServerPassword = cf.ServerPassword
	}
	if cf.ServerSSL {
		a.Config.ServerSSL = true
		a.Config.ServerInsecureSSL = false
	}
	if cf.ServerInsecureSSL {
		a.Config.ServerSSL = false
		a.Config.ServerInsecureSSL = true
	}

	// Set APIs and check if server is alive.
	if err := a.SetAPI(); err != nil {
		return err
	}

	// Client options.

	// Client name. Initial setup.
	if a.Config.ClientName == "" {
		if cf.ClientName != "" {
			a.Config.ClientName = cf.ClientName
		} else {
			hostname, _ := os.Hostname()
			a.Config.ClientName = hostname
		}

		node, _, err := a.consulAPI.Catalog().Node(a.Config.ClientName, nil)
		if err != nil {
			return fmt.Errorf("Unable to communicate with Consul: %s", err)
		}
		if node != nil && len(node.Services) > 0 {
			if !flagForce {
				return fmt.Errorf(`Another client with the same name '%s' detected, its address is %s.
It has the active services so this name is not available.

Specify the other one using --client-name flag.

In case this is the correct client node that was previously uninstalled with unreachable PMM server,
you can add --force flag to proceed further. Do not use this flag otherwise.
The orphaned remote services will be removed automatically.`,
					a.Config.ClientName, node.Node.Address)
			}
			// Allow to set client name and clean missing services.
			a.RepairInstallation()
		}
	} else if cf.ClientName != "" && cf.ClientName != a.Config.ClientName {
		// Attempt to change client name.
		oldName := a.Config.ClientName
		newName := cf.ClientName

		// Checking target name.
		node, _, err := a.consulAPI.Catalog().Node(newName, nil)
		if err != nil {
			return fmt.Errorf("Unable to communicate with Consul: %s", err)
		}
		if node != nil && len(node.Services) > 0 {
			return fmt.Errorf(`Another client with the same name '%s' detected, its address is address %s.
It has the active services so you cannot change client name as requested.`,
				newName, node.Node.Address)
		}

		// Checking source name.
		node, _, err = a.consulAPI.Catalog().Node(oldName, nil)
		if err != nil {
			return fmt.Errorf("Unable to communicate with Consul: %s", err)
		}

		if node != nil && len(node.Services) > 0 {
			if !flagForce {
				return errors.New("This client has active services. Some data might be lost, you can add --force flag to proceed further.")
			}

			// At the time of writing there is no easy way to rename node.
			// To do this all services needs to be de-registered and registered again with new name
			errs := a.renameClientNameInServices(node, oldName, newName)
			if errs != nil {
				log.Printf("WARNING: there were some errors, renaming partially failed: %s\n", errs)
			}

			// deregister old node
			err := a.deregisterNode(oldName)
			if err != nil {
				log.Printf("Unable to deregister old node: %s\n", err)
			}
		}

		a.Config.ClientName = cf.ClientName
	}
	if match, _ := regexp.MatchString(NameRegex, a.Config.ClientName); !match {
		return errors.New(`Client name must be 2 to 60 characters long, contain only letters, numbers and symbols _ - . :
Use --client-name flag to set the correct one.`)
	}

	// Client address. Initial setup.
	isDetectedIP := false
	if a.Config.ClientAddress == "" {
		if cf.ClientAddress != "" {
			a.Config.ClientAddress = cf.ClientAddress
		} else {
			// Detect remote address from nginx response header.
			a.Config.ClientAddress = a.getNginxHeader("X-Remote-IP")
			isDetectedIP = true
		}

		if a.Config.ClientAddress == "" {
			return errors.New("Cannot detect client address. Use --client-address flag to set it.")
		}
	} else if cf.ClientAddress != "" && cf.ClientAddress != a.Config.ClientAddress {
		// Attempt to change client address.
		node, _, err := a.consulAPI.Catalog().Node(a.Config.ClientName, nil)
		if err != nil {
			return fmt.Errorf("Unable to communicate with Consul: %s", err)
		}
		if node != nil && len(node.Services) > 0 {
			return errors.New("Changing of client address is allowed only if there are no services under monitoring.")
		}

		a.Config.ClientAddress = cf.ClientAddress
	}

	// Bind address. Initial setup.
	if a.Config.BindAddress == "" {
		a.Config.BindAddress = a.Config.ClientAddress
		if cf.BindAddress != "" {
			a.Config.BindAddress = cf.BindAddress
			isDetectedIP = false
		}
	} else if cf.BindAddress != "" && cf.BindAddress != a.Config.BindAddress {
		// Attempt to change bind address.
		node, _, err := a.consulAPI.Catalog().Node(a.Config.ClientName, nil)
		if err != nil {
			return fmt.Errorf("Unable to communicate with Consul: %s", err)
		}
		if node != nil && len(node.Services) > 0 {
			return errors.New("Changing of bind address is allowed only if there are no services under monitoring.")
		}

		a.Config.BindAddress = cf.BindAddress
	}

	if !isAddressLocal(a.Config.BindAddress) {
		if isDetectedIP {
			return fmt.Errorf(`Detected address '%s' is not locally bound.
This usually happens when client and server are on the different networks.

Use --bind-address flag to set locally bound address, usually a private one, while client address is public.
The bind address should correspond to the detected client address via NAT and you would need to configure port forwarding.

PMM server should be able to connect to the client address '%s' which should translate to a local bind address.
What ports to map you can find from "pmm-admin check-network" output once you add instances to the monitoring.`,
				a.Config.BindAddress, a.Config.ClientAddress)
		}
		return fmt.Errorf(`Client Address: %s
Bind Address: %s

The bind address is not locally bound.

Use --bind-address flag to set locally bound address, usually a private one,
and --client-address flag to set the corresponding remote address, usually a public one.
The bind address should correspond to the client address via NAT and you would need to configure port forwarding.

PMM server should be able to connect to the client address which should translate to a local bind address.
What ports to map you can find from "pmm-admin check-network" output once you add instances to the monitoring.`,
			a.Config.ClientAddress, a.Config.BindAddress)
	}

	// If agent config exists, update the options like address, SSL, password etc.
	agentConfigFile := fmt.Sprintf("%s/config/agent.conf", AgentBaseDir)
	if FileExists(agentConfigFile) {
		if err := a.syncAgentConfig(agentConfigFile); err != nil {
			return fmt.Errorf("Unable to update agent config %s: %s", agentConfigFile, err)
		}
		// Restart QAN agent for MySQL.
		if _, err := a.StartStopMonitoring("restart", "mysql:queries"); err != nil && err != ErrNoService {
			return fmt.Errorf("Unable to restart queries service for MySQL: %s", err)
		}
		// Restart QAN agent for MongoDB.
		if _, err := a.StartStopMonitoring("restart", "mongodb:queries"); err != nil && err != ErrNoService {
			return fmt.Errorf("Unable to restart queries service for MongoDB: %s", err)
		}
	}

	// Write the config.
	if err := a.writeConfig(); err != nil {
		return fmt.Errorf("Unable to write config file %s: %s", ConfigFile, err)
	}

	// Restart all services when resetting server address (wiping password) or changing password.
	if cf.ServerAddress != "" || cf.ServerPassword != "" {
		_, _, err := a.StartStopAllMonitoring("restart")
		if err != nil {
			return fmt.Errorf("Error restarting one of the services: %s", err)
		}
	}

	return nil
}

// writeConfig write config to the file.
func (a *Admin) writeConfig() error {
	bytes, _ := yaml.Marshal(a.Config)
	return ioutil.WriteFile(ConfigFile, bytes, 0600)
}

// syncAgentConfig sync agent config.
func (a *Admin) syncAgentConfig(agentConfigFile string) error {
	jsonData, err := ioutil.ReadFile(agentConfigFile)
	if err != nil {
		return err
	}
	agentConf := &protocfg.Agent{}
	if err := json.Unmarshal(jsonData, &agentConf); err != nil {
		return err
	}
	agentConf.ApiHostname = a.Config.ServerAddress
	agentConf.ServerSSL = a.Config.ServerSSL
	agentConf.ServerInsecureSSL = a.Config.ServerInsecureSSL
	agentConf.ServerUser = a.Config.ServerUser
	agentConf.ServerPassword = a.Config.ServerPassword

	bytes, _ := json.Marshal(agentConf)
	return ioutil.WriteFile(agentConfigFile, bytes, 0600)
}

// getNginxHeader get header value from Nginx response.
func (a *Admin) getNginxHeader(header string) string {
	url := a.qanAPI.URL(a.serverURL, "v1/status/leader")
	resp, _, err := a.qanAPI.Get(url)
	if err != nil {
		return ""
	}
	return resp.Header.Get(header)
}

// isAddressLocal check if IP address is locally bound on the system.
func isAddressLocal(myAddress string) bool {
	ips, _ := net.InterfaceAddrs()
	for _, ip := range ips {
		if strings.HasPrefix(ip.String(), myAddress+"/") {
			return true
		}
	}
	return false
}

// renameClientNameInServices changes a clientName in all services
func (a *Admin) renameClientNameInServices(node *consul.CatalogNode, oldName, newName string) (errs Errors) {
	for _, svc := range node.Services {
		for k, v := range svc.Tags {
			if v == fmt.Sprintf("alias_%s", oldName) {
				svc.Tags[k] = fmt.Sprintf("alias_%s", newName)
			}
		}

		// Add new service to Consul.
		reg := consul.CatalogRegistration{
			Node:    newName,
			Address: a.Config.ClientAddress,
			Service: svc,
		}

		if _, err := a.consulAPI.Catalog().Register(&reg, nil); err != nil {
			errs = append(errs, err)
			continue
		}

		// Remove old service from Consul
		dereg := consul.CatalogDeregistration{
			Node:      oldName,
			ServiceID: svc.ID,
		}
		if _, err := a.consulAPI.Catalog().Deregister(&dereg, nil); err != nil {
			errs = append(errs, err)
			continue
		}

		// Update consul key-value store
		prefix := fmt.Sprintf("%s/%s/", oldName, svc.ID)
		keys, _, err := a.consulAPI.KV().Keys(prefix, "", nil)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		for _, oldKey := range keys {
			data, _, err := a.consulAPI.KV().Get(oldKey, nil)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			if data == nil {
				continue
			}

			// Update qan instance
			if strings.HasSuffix(oldKey, "/qan_mysql_uuid") {
				instanceUUID := string(data.Value)
				err := a.renameInstance(instanceUUID, oldName, newName)
				if err != nil {
					errs = append(errs, err)
				}
			}

			// Update consul key
			newKey := strings.Replace(oldKey, oldName, newName, -1)
			if oldKey == newKey {
				// ... but only if key is different
				continue
			}

			data.Key = newKey
			_, err = a.consulAPI.KV().Put(data, nil)
			if err != nil {
				errs = append(errs, err)
				continue
			}

			_, err = a.consulAPI.KV().Delete(oldKey, nil)
			if err != nil {
				errs = append(errs, err)
				continue
			}
		}
	}

	return errs
}

// deregisterNode removes node from consul
func (a *Admin) deregisterNode(name string) error {
	dereg := consul.CatalogDeregistration{
		Node: name,
	}
	if _, err := a.consulAPI.Catalog().Deregister(&dereg, nil); err != nil {
		return err
	}
	return nil
}

// renameInstance renames instance with given uuid
func (a *Admin) renameInstance(instanceUUID, oldName, newName string) error {
	bytes, err := ioutil.ReadFile(fmt.Sprintf("%s/instance/%s.json", AgentBaseDir, instanceUUID))
	if err != nil {
		return err
	}

	instance := proto.Instance{}
	err = json.Unmarshal(bytes, &instance)
	if err != nil {
		return err
	}

	// change hostname if needed
	newInstanceName := strings.Replace(instance.Name, oldName, newName, 1)
	if newInstanceName == instance.Name {
		return nil
	}
	instance.Name = newInstanceName

	newBytes, err := json.MarshalIndent(instance, "", "    ")
	if err != nil {
		return err
	}
	err = a.updateInstance(instanceUUID, newBytes)
	if err != nil {
		return err
	}
	if err := ioutil.WriteFile(fmt.Sprintf("%s/instance/%s.json", AgentBaseDir, instanceUUID), newBytes, 0600); err != nil {
		return err
	}

	return nil
}

// updateInstance updates instance on QAN API.
func (a *Admin) updateInstance(inUUID string, bytes []byte) error {
	url := a.qanAPI.URL(a.serverURL, qanAPIBasePath, "instances", inUUID)
	resp, content, err := a.qanAPI.Put(url, bytes)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusNoContent {
		return a.qanAPI.Error("PUT", url, resp.StatusCode, http.StatusNoContent, content)

	}
	return nil
}
