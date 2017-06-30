package kvm

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"strings"
	"text/template"

	"github.com/docker/machine/libmachine/log"
	libvirt "github.com/libvirt/libvirt-go"
	"github.com/pkg/errors"
)

// Replace with hardcoded range with CIDR
// https://play.golang.org/p/m8TNTtygK0
const privateNetworkTmpl = `
<network>
  <name>{{.NetworkName}}</name>
  <ip address='192.168.39.1' netmask='255.255.255.0'>
    <dhcp>
      <range start='192.168.39.2' end='192.168.39.254'/>
    </dhcp>
  </ip>
</network>
`

const defaultNetworkTmpl = `
<network>
  <name>default</name>
  <uuid>73618f96-1a85-47a8-a809-7edd8ad7899a</uuid>
  <forward mode='nat'/>
  <bridge name='virbr0' stp='on' delay='0'/>
  <mac address='52:54:00:5e:a8:a3'/>
  <ip address='192.168.122.1' netmask='255.255.255.0'>
    <dhcp>
      <range start='192.168.122.2' end='192.168.122.254'/>
    </dhcp>
  </ip>
</network>
`

// const networkName = "minikube-net"

func (d *Driver) createNetworks() error {
	if err := d.createNetwork("default", defaultNetworkTmpl); err != nil {
		return errors.Wrap(err, "creating default network")
	}
	if err := d.createNetwork(d.NetworkName, privateNetworkTmpl); err != nil {
		return errors.Wrap(err, "creating private network")
	}

	return nil
}

func (d *Driver) createNetwork(networkName, networkTmpl string) error {
	log.Infof("Creating network %s...", networkName)
	conn, err := getConnection()
	if err != nil {
		return errors.Wrap(err, "getting libvirt connection")
	}
	defer conn.Close()

	tmpl := template.Must(template.New("network").Parse(networkTmpl))
	var networkXML bytes.Buffer
	err = tmpl.Execute(&networkXML, d)
	if err != nil {
		return errors.Wrap(err, "executing network template")
	}

	//Check if network already exists
	network, err := conn.LookupNetworkByName(networkName)
	if err != nil {
		network, err = conn.NetworkDefineXML(networkXML.String())
		if err != nil {
			return errors.Wrapf(err, "defining network from xml: %s", networkXML.String())
		}
	}

	err = network.SetAutostart(true)
	if err != nil {
		return errors.Wrap(err, "setting network to autostart")
	}

	active, err := network.IsActive()
	if err != nil || !active {
		err = network.Create()
		if err != nil {
			return errors.Wrap(err, "creating network")
		}
	}

	return nil
}

func (d *Driver) lookupIP() (string, error) {
	conn, err := getConnection()
	if err != nil {
		return "", errors.Wrap(err, "getting connection and domain")
	}

	defer conn.Close()

	libVersion, err := conn.GetLibVersion()
	if err != nil {
		return "", errors.Wrap(err, "getting libversion")
	}

	// Earlier versions of libvirt don't support getting DHCP address from domains by API
	if libVersion < 1002006 {
		return d.lookupIPFromStatusFile()
	}

	return d.lookupIPFromNetwork(conn)
}

func (d *Driver) lookupIPFromNetwork(conn *libvirt.Connect) (string, error) {
	network, err := conn.LookupNetworkByName(d.NetworkName)
	if err != nil {
		return "", errors.Wrap(err, "looking up network by name")
	}
	leases, err := network.GetDHCPLeases()
	if err != nil {
		return "", errors.Wrap(err, "looking up dhcp leases for network")
	}

	for _, lease := range leases {
		if lease.Type == libvirt.IP_ADDR_TYPE_IPV4 {
			return lease.IPaddr, nil
		}
	}

	// No IP has been allocated yet
	return "", nil
}

// This is for older versions of libvirt that don't support listAllInterfaceAddresses
func (d *Driver) lookupIPFromStatusFile() (string, error) {
	leasesFile := fmt.Sprintf("/var/lib/libvirt/dnsmasq/%s.leases", d.NetworkName)
	leases, err := ioutil.ReadFile(leasesFile)
	if err != nil {
		return "", errors.Wrap(err, "reading leases file")
	}
	ipAddress := ""
	for _, lease := range strings.Split(string(leases), "\n") {
		if len(lease) == 0 {
			continue
		}
		// format for lease entry
		// ExpiryTime MAC IP Hostname ExtendedMAC
		entry := strings.Split(lease, " ")
		if len(entry) != 5 {
			return "", fmt.Errorf("Malformed leases entry: %s", entry)
		}
		if entry[3] == d.MachineName {
			ipAddress = entry[2]
		}
	}
	return ipAddress, nil
}
