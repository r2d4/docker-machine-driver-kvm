//
// kvm.go
// Copyright (C) 2016 Matt Rickard <m@rickard.email>
//
// Distributed under terms of the All Rights Reserved. license.
//

package kvm

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/docker/machine/libmachine/drivers"
	"github.com/docker/machine/libmachine/log"
	"github.com/docker/machine/libmachine/mcnflag"
	"github.com/docker/machine/libmachine/mcnutils"
	"github.com/docker/machine/libmachine/state"
	libvirt "github.com/libvirt/libvirt-go"
	"github.com/pkg/errors"
)

const (
	defaultIsoURL    = "https://storage.googleapis.com/minikube/iso/minikube-v0.18.0.iso"
	defaultCPU       = 1
	defaultDiskSize  = 20000
	defaultMemory    = 2048
	qemusystem       = "qemu:///system"
	defaultCacheMode = "threads"
)

var defaultHostFolder = os.Getenv("HOME")

type Driver struct {
	*drivers.BaseDriver

	IsoURL         string
	PrivateKeyPath string

	CPU         int
	Memory      int
	DiskSize    int64
	NetworkName string
	DiskPath    string
	ISO         string
	CacheMode   string
}

func NewDriver(hostName, storePath string) *Driver {
	return &Driver{
		BaseDriver: &drivers.BaseDriver{
			MachineName: hostName,
			StorePath:   storePath,
		},
		IsoURL:      defaultIsoURL,
		CPU:         defaultCPU,
		DiskSize:    defaultDiskSize,
		Memory:      defaultMemory,
		NetworkName: defaultNetworkName,
		DiskPath:    storePath,
		CacheMode:   defaultCacheMode,
	}
}

//Not implemented yet
func (d *Driver) GetCreateFlags() []mcnflag.Flag {
	return nil
}

//Not implemented yet
func (d *Driver) SetConfigFromFlags(flags drivers.DriverOptions) error {
	return nil
}

func (d *Driver) PreCommandCheck() error {
	conn, err := getConnection()
	if err != nil {
		return errors.Wrap(err, "Error connecting to libvirt socket.  Have you added yourself to the libvirtd group?")
	}
	libVersion, err := conn.GetLibVersion()
	if err != nil {
		return errors.Wrap(err, "getting libvirt version")
	}
	log.Debugf("Using libvirt version %d", libVersion)

	return nil
}

func (d *Driver) GetURL() (string, error) {
	if err := d.PreCommandCheck(); err != nil {
		return "", errors.Wrap(err, "getting URL, precheck failed")
	}

	ip, err := d.GetIP()
	if err != nil {
		return "", errors.Wrap(err, "getting URL, could not get IP")
	}
	if ip == "" {
		return "", nil
	}

	for {
		err := drivers.WaitForSSH(d)
		if err != nil {
			d.IPAddress = ""
			time.Sleep(1 * time.Second)
		} else {
			break
		}
	}

	return fmt.Sprintf("tcp://%s:2376", ip), nil
}

func (d *Driver) GetState() (state.State, error) {
	dom, conn, err := d.getDomain()
	if err != nil {
		return state.None, errors.Wrap(err, "getting connection")
	}
	defer closeDomain(dom, conn)

	libvirtState, _, err := dom.GetState() // state, reason, error
	if err != nil {
		return state.None, errors.Wrap(err, "getting domain state")
	}

	stateMap := map[libvirt.DomainState]state.State{
		libvirt.DOMAIN_NOSTATE:     state.None,
		libvirt.DOMAIN_RUNNING:     state.Running,
		libvirt.DOMAIN_BLOCKED:     state.Error,
		libvirt.DOMAIN_PAUSED:      state.Paused,
		libvirt.DOMAIN_SHUTDOWN:    state.Stopped,
		libvirt.DOMAIN_CRASHED:     state.Error,
		libvirt.DOMAIN_PMSUSPENDED: state.Saved,
		libvirt.DOMAIN_SHUTOFF:     state.Stopped,
	}

	val, ok := stateMap[libvirtState]

	if !ok {
		return state.None, nil
	}

	return val, nil
}

func (d *Driver) GetIP() (string, error) {
	s, err := d.GetState()
	if err != nil {
		return "", errors.Wrap(err, "machine in unknown state")
	}
	if s != state.Running {
		return "", errors.New("host is not running.")
	}
	ip, err := d.lookupIP()
	if err != nil {
		return "", errors.Wrap(err, "getting IP")
	}

	return ip, nil
}

func (d *Driver) GetMachineName() string {
	return d.MachineName
}

func (d *Driver) GetSSHHostname() (string, error) {
	return d.GetIP()
}

func (d *Driver) GetSSHUsername() string {
	return "docker"
}

func (d *Driver) GetSSHKeyPath() string {
	return d.ResolveStorePath("id_rsa")
}

func (d *Driver) GetSSHPort() (int, error) {
	if d.SSHPort == 0 {
		d.SSHPort = 22
	}

	return d.SSHPort, nil
}

func (d *Driver) DriverName() string {
	return "kvm"
}

func (d *Driver) Kill() error {
	dom, conn, err := d.getDomain()
	if err != nil {
		return errors.Wrap(err, "getting connection")
	}
	defer closeDomain(dom, conn)

	return dom.Destroy()
}

func (d *Driver) Restart() error {
	dom, conn, err := d.getDomain()
	if err != nil {
		return errors.Wrap(err, "getting connection")
	}
	defer closeDomain(dom, conn)

	if err := d.Stop(); err != nil {
		return errors.Wrap(err, "stopping VM:")
	}
	return d.Start()
}

func (d *Driver) Start() error {
	log.Debug("Getting domain xml...")
	dom, conn, err := d.getDomain()
	if err != nil {
		return errors.Wrap(err, "getting connection")
	}
	defer closeDomain(dom, conn)

	log.Debug("Creating domain...")
	if err := dom.Create(); err != nil {
		return errors.Wrap(err, "Error creating VM")
	}

	time.Sleep(5 * time.Second)
	for i := 0; i <= 40; i++ {
		ip, err := d.GetIP()
		if err != nil || ip == "" {
			if err != nil {
				d.IPAddress = ""
				log.Debug(err)
			}
			log.Debugf("Waiting for machine to come up %d/%d", i, 40)
			time.Sleep(3 * time.Second)
			continue
		}

		if ip != "" {
			log.Debugf("Found IP for machine: %s", ip)
			d.IPAddress = ip
			break
		}
	}

	if d.IPAddress == "" {
		return errors.New("Machine didn't return an IP after 120 seconds")
	}

	if err := drivers.WaitForSSH(d); err != nil {
		d.IPAddress = ""
		return errors.Wrap(err, "SSH not available after waiting")
	}

	return nil
}

func (d *Driver) Create() error {
	//TODO(r2d4): rewrite this, not using b2dutils
	b2dutils := mcnutils.NewB2dUtils(d.StorePath)
	if err := b2dutils.CopyIsoToMachineDir(d.IsoURL, d.MachineName); err != nil {
		return errors.Wrap(err, "Error copying ISO to machine dir")
	}

	err := d.createNetwork()
	if err != nil {
		return errors.Wrap(err, "creating network")
	}

	if err := os.MkdirAll(d.ResolveStorePath("."), 0755); err != nil {
		return errors.Wrap(err, "Error making store path directory")
	}

	for dir := d.ResolveStorePath("."); dir != "/"; dir = filepath.Dir(dir) {
		log.Debugf("Verifying executable bit set on %s", dir)
		info, err := os.Stat(dir)
		if err != nil {
			return err
		}
		mode := info.Mode()
		if mode&0001 != 1 {
			log.Debugf("Setting executable bit set on %s", dir)
			mode |= 0001
			os.Chmod(dir, mode)
		}
	}

	err = d.buildDiskImage()
	if err != nil {
		return errors.Wrap(err, "Error creating disk")
	}

	dom, err := d.createDomain()
	if err != nil {
		return errors.Wrap(err, "creating domain")
	}
	defer dom.Free()

	log.Debug("Finished create, calling Start()")
	return d.Start()
}

func (d *Driver) Stop() error {
	d.IPAddress = ""
	s, err := d.GetState()
	if err != nil {
		return errors.Wrap(err, "getting state of VM")
	}

	if s != state.Stopped {
		dom, conn, err := d.getDomain()
		defer closeDomain(dom, conn)
		if err != nil {
			return errors.Wrap(err, "getting connection")
		}

		err = dom.DestroyFlags(libvirt.DOMAIN_DESTROY_GRACEFUL)
		if err != nil {
			return errors.Wrap(err, "stopping vm")
		}

		for i := 0; i < 60; i++ {
			s, err := d.GetState()
			if err != nil {
				return errors.Wrap(err, "Error getting state of VM")
			}
			if s == state.Stopped {
				return nil
			}
			log.Info("Waiting for machine to stop %d/%d", i, 60)
			time.Sleep(1 * time.Second)
		}

	}

	return fmt.Errorf("Could not stop VM, current state %s", s.String())
}

func (d *Driver) Remove() error {
	log.Debug("Calling remove...")
	conn, err := getConnection()
	if err != nil {
		return errors.Wrap(err, "getting connection")
	}
	defer conn.CloseConnection()

	//Tear down network and disk if they exist
	network, _ := conn.LookupNetworkByName(d.NetworkName)
	log.Debug("Checking if need to delete network")
	if network != nil {
		network.Destroy()
		network.Undefine()
		log.Debug("Deleted network")
	}

	log.Debug("Checking if need to delete volume")

	pool, err := conn.LookupStoragePoolByName("default")
	/*
		if pool != nil {
			log.Debug("Pool is not empty")
			pool.Delete(0)
			pool.Undefine()
			pool.Free()
			log.Debug("Pool deleted")
		}
	*/

	vol, _ := pool.LookupStorageVolByName("minikube-pool0-vol0")
	log.Debug(vol)
	if vol != nil {
		vol.Delete(0)
		vol.Free()
		log.Debug("Deleted storage volume")
	}

	dom, err := conn.LookupDomainByName(d.MachineName)
	if dom != nil {
		dom.Destroy()
		dom.Undefine()
		log.Debug("Deleted domain")
	}

	return nil
}
