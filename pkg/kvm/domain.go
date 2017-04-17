package kvm

import (
	"bytes"
	"fmt"
	"text/template"

	libvirt "github.com/libvirt/libvirt-go"
	"github.com/pkg/errors"
)

const domainTmpl = `
<domain type='kvm'>
  <name>{{.MachineName}}</name> 
  <memory unit='MB'>{{.Memory}}</memory>
  <vcpu>{{.CPU}}</vcpu>
  <features>
    <acpi/>
    <apic/>
    <pae/>
  </features>
  <os>
    <type>hvm</type>
    <boot dev='cdrom'/>
    <boot dev='hd'/>
    <bootmenu enable='no'/>
  </os>
  <devices>
    <disk type='file' device='cdrom'>
      <source file='{{.ISO}}'/>
      <target dev='hdc' bus='ide'/>
      <readonly/>
    </disk>
    <disk type='file' device='disk'>
      <driver name='qemu' type='raw' cache='{{.CacheMode}}' io='threads' />
      <source file='{{.DiskPath}}'/>
      <target dev='hda' bus='ide'/>
    </disk>
    <interface type='network'>
      <source network='default'/>
    </interface>
    <interface type='network'>
      <source network='{{.NetworkName}}'/>
    </interface>
    <serial type='pty'>
      <source path='/dev/pts/2'/>
      <target port='0'/>
    </serial>
    <console type='pty' tty='/dev/pts/2'>
      <source path='/dev/pts/2'/>
      <target port='0'/>
    </console>
  </devices>
</domain>
`

func (d *Driver) getDomain() (*libvirt.Domain, *libvirt.Connect, error) {
	conn, err := getConnection()
	if err != nil {
		return nil, nil, errors.Wrap(err, "getting domain")
	}

	dom, err := conn.LookupDomainByName(d.MachineName)
	if err != nil {
		return nil, nil, errors.Wrap(err, "looking up domain")
	}

	return dom, conn, nil
}

func getConnection() (*libvirt.Connect, error) {
	conn, err := libvirt.NewConnect(qemusystem)
	if err != nil {
		return nil, errors.Wrap(err, "Error connecting to libvirt socket")
	}

	return conn, nil
}

func closeDomain(dom *libvirt.Domain, conn *libvirt.Connect) error {
	dom.Free()
	if res, _ := conn.CloseConnection(); res != 0 {
		return fmt.Errorf("Error closing connection CloseConnection() == %d, expected 0", res)
	}
	return nil
}

func (d *Driver) createDomain() (*libvirt.Domain, error) {
	tmpl := template.Must(template.New("domain").Parse(domainTmpl))
	var domainXml bytes.Buffer
	err := tmpl.Execute(&domainXml, d)
	if err != nil {
		return nil, errors.Wrap(err, "executing domain xml")
	}

	conn, err := getConnection()
	if err != nil {
		return nil, errors.Wrap(err, "Error getting libvirt connection")
	}
	defer conn.CloseConnection()

	dom, err := conn.DomainDefineXML(domainXml.String())
	if err != nil {
		return nil, errors.Wrapf(err, "Error defining domain xml: %s", domainXml.String())
	}

	return dom, nil
}
