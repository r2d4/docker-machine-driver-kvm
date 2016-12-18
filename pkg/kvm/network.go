package kvm

import (
	"bytes"
	"fmt"
	"github.com/docker/machine/libmachine/log"
	"text/template"
	"time"

	libvirt "github.com/libvirt/libvirt-go"
	"github.com/pkg/errors"
)

// Replace with hardcoded range with CIDR
// https://play.golang.org/p/m8TNTtygK0
const networkTmpl = `
<network>
  <name>{{.NetworkName}}</name>
  <ip address='192.168.39.1' netmask='255.255.255.0'>
    <dhcp>
      <range start='192.168.39.2' end='192.168.39.254'/>
    </dhcp>
  </ip>
</network>
`

func (d *Driver) createNetwork() error {
	conn, err := d.getConnection()
	if err != nil {
		return errors.Wrap(err, "getting libvirt connection")
	}
	defer conn.CloseConnection()

	tmpl := template.Must(template.New("network").Parse(networkTmpl))
	var networkXml bytes.Buffer
	err = tmpl.Execute(&networkXml, d)
	if err != nil {
		return errors.Wrap(err, "executing network template")
	}

	//Check if network already exists
	network, err := conn.LookupNetworkByName(d.NetworkName)
	if err == nil {
		return nil
	}

	network, err = conn.NetworkDefineXML(networkXml.String())
	if err != nil {
		return errors.Wrapf(err, "defining network from xml: %s", networkXml.String())
	}
	err = network.SetAutostart(true)
	if err != nil {
		return errors.Wrap(err, "setting network to autostart")
	}

	err = network.Create()
	if err != nil {
		return errors.Wrap(err, "creating network")
	}

	return nil
}

func (d *Driver) lookupIPFromDomain() (string, error) {
	dom, conn, err := d.getDomain()
	if err != nil {
		return "", errors.Wrap(err, "getting domain")
	}
	defer closeDomain(dom, conn)

	domIfaces, err := dom.ListAllInterfaceAddresses(0)
	if err != nil {
		if isNotSupportedError(err) {
			return d.lookupIPLegacy()
		}
		return "", errors.Wrap(err, "list all domain interface addresses")
	}
	if len(domIfaces) != 2 {
		return "", fmt.Errorf("Domain has wrong number of interfaces, got %d, expected 2", len(domIfaces))
	}

	for _, domIface := range domIfaces {
		if domIface.Name == d.NetworkName {
			return domIface.Addrs[0].Addr, nil
		}
	}

	return "", errors.New("Could not find IP in Domain Interfaces")
}

// This is for older versions of libvirt that don't support listAllInterfaceAddresses
func (d *Driver) lookupIPLegacy() (string, error) {
	dom, conn, err := d.getDomain()
	if err != nil {
		return "", errors.Wrapf(err, "getting domain")
	}
	defer closeDomain(dom, conn)

	net, err := conn.LookupNetworkByName(d.NetworkName)
	if err != nil {
		return "", errors.Wrapf(err, "getting network %s", d.NetworkName)
	}
	defer net.Free()

	leases, err := net.GetDHCPLeases()
	if err != nil {
		return "", errors.Wrapf(err, "Error getting DHCP leases from Network %s", d.NetworkName)
	}

	ip := ""
	expiryTime := time.Now()
	for _, lease := range leases {
		log.Debugf("Network: %s, Hostname: %s, IP: %s, Expires: %s", d.NetworkName, lease.Hostname, lease.IPaddr, lease.ExpiryTime.Format(time.RFC3339))
		//TODO(r2d4): This won't work if if the machine name doesn't
		// match the hostname of the VM
		if lease.Hostname == d.MachineName && lease.ExpiryTime.After(expiryTime) {
			ip = lease.IPaddr
			expiryTime = lease.ExpiryTime
		}
	}

	return ip, nil
}

func isNotSupportedError(e error) bool {
	if err, ok := e.(libvirt.Error); ok {
		return err.Code == libvirt.ERR_NO_SUPPORT
	}
	return false
}
