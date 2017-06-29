package kvm

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"strings"
	"text/template"

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

const defaultNetworkName = "minikube-net"

func (d *Driver) createNetwork() error {
	conn, err := getConnection()
	if err != nil {
		return errors.Wrap(err, "getting libvirt connection")
	}
	defer conn.Close()

	tmpl := template.Must(template.New("network").Parse(networkTmpl))
	var networkXml bytes.Buffer
	err = tmpl.Execute(&networkXml, d)
	if err != nil {
		return errors.Wrap(err, "executing network template")
	}

	//Check if network already exists
	network, err := conn.LookupNetworkByName(defaultNetworkName)
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
