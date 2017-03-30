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
	service "github.com/percona/kardianos-service"
)

var (
	NewService func(i service.Interface, c *service.Config) (service.Service, error) = service.New
)

// @todo don't use singleton init, use dependency injection
func init() {
	// if we build app in tests then let's mock service installer
	if Version == "gotest" {
		NewService = func(i service.Interface, c *service.Config) (service.Service, error) {
			return &dummyService{}, nil
		}
	}
}

type dummyService struct {
}

func (*dummyService) Run() error       { return nil }
func (*dummyService) Start() error     { return nil }
func (*dummyService) Stop() error      { return nil }
func (*dummyService) Restart() error   { return nil }
func (*dummyService) Install() error   { return nil }
func (*dummyService) Uninstall() error { return nil }
func (*dummyService) Status() error    { return nil }
func (*dummyService) Logger(errs chan<- error) (service.Logger, error) {
	return service.ConsoleLogger, nil
}
func (*dummyService) SystemLogger(errs chan<- error) (service.Logger, error) {
	return service.ConsoleLogger, nil
}
func (*dummyService) String() string { return "" }

// Platform service manager handlers.
type program struct{}

func (p *program) Start(s service.Service) error {
	return nil
}

func (p *program) Stop(s service.Service) error {
	return nil
}

func (p *program) run() error {
	return nil
}

func installService(svcConfig *service.Config) error {
	prg := &program{}
	svc, err := NewService(prg, svcConfig)
	if err != nil {
		return err
	}
	if err := svc.Install(); err != nil {
		return err
	}
	if err := svc.Start(); err != nil {
		return err
	}
	return nil
}

func uninstallService(name string) error {
	prg := &program{}
	svcConfig := &service.Config{Name: name}
	svc, err := NewService(prg, svcConfig)
	if err != nil {
		return err
	}
	if err := svc.Status(); err == nil {
		if err := svc.Stop(); err != nil {
			return err
		}
	}
	if err := svc.Uninstall(); err != nil {
		return err
	}
	return nil
}

func startService(name string) error {
	prg := &program{}
	svcConfig := &service.Config{Name: name}
	svc, err := NewService(prg, svcConfig)
	if err != nil {
		return err
	}
	if err := svc.Status(); err != nil {
		if err := svc.Start(); err != nil {
			return err
		}
	}
	return nil
}

func stopService(name string) error {
	prg := &program{}
	svcConfig := &service.Config{Name: name}
	svc, err := NewService(prg, svcConfig)
	if err != nil {
		return err
	}
	if err := svc.Status(); err == nil {
		if err := svc.Stop(); err != nil {
			return err
		}
	}
	return nil
}

func getServiceStatus(name string) bool {
	prg := &program{}
	svcConfig := &service.Config{Name: name}
	svc, err := NewService(prg, svcConfig)
	if err != nil {
		return false
	}
	if err := svc.Status(); err != nil {
		return false
	}
	return true
}
