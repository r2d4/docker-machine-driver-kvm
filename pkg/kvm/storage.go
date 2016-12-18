package kvm

import (
	"archive/tar"
	"bytes"
	"fmt"
	"io/ioutil"
	"text/template"

	"github.com/docker/machine/libmachine/ssh"
	libvirt "github.com/libvirt/libvirt-go"
	"github.com/pkg/errors"
)

const volumeTmpl = `
<volume>
  <name>{{.MachineName}}-pool0-vol0</name>
  <allocation>0</allocation>
  <capacity unit="MB">{{.DiskSize}}</capacity>
  <format type="raw"/>
  <target>
    <path>{{.DiskPath}}</path>
    <permissions>
      <owner>107</owner>
      <group>107</group>
      <mode>0744</mode>
      <label>virt_image_t</label>
    </permissions>
  </target>
</volume>
`

const poolTmpl = `
<pool type="dir">
  <name>minikube-pool0</name>
  <target>
    <path>/var/lib/libvirt/images</path>
  </target>
</pool>
`

func (d *Driver) createDisk() error {

	// Parse the template
	tmpl := template.Must(template.New("volume").Parse(volumeTmpl))
	var volumeXml bytes.Buffer
	err := tmpl.Execute(&volumeXml, d)
	if err != nil {
		return errors.Wrap(err, "executing volume template")
	}
	/*
		tmpl = template.Must(template.New("pool").Parse(poolTmpl))
		var poolXml bytes.Buffer
		err = tmpl.Execute(&poolXml, d)
		if err != nil {
			return errors.Wrap(err, "executing storage template")
		}
	*/
	// Connect to the libvirt socket
	conn, err := libvirt.NewConnect(qemusystem)
	if err != nil {
		return errors.Wrap(err, "connecting to libvirt socket")
	}
	defer func() {
		if res, _ := conn.CloseConnection(); res != 0 {
			fmt.Errorf("CloseConnection() == %d, expected 0", res)
		}
	}()
	/*
		// Create the storage pool and volume
		pool, err := conn.StoragePoolDefineXML(poolXml.String(), 0)
		if err != nil {
			return errors.Wrapf(err, "defining storage pool: %s", poolXml.String())
		}
		defer pool.Free()

		if err = pool.Create(0); err != nil {
			return errors.Wrap(err, "creating storage pool")
		}
	*/
	pool, err := conn.LookupStoragePoolByName("default")
	if err != nil {
		return errors.Wrap(err, "looking up default storage pool")
	}

	vol, err := pool.StorageVolCreateXML(volumeXml.String(), 0)
	if err != nil {
		return errors.Wrapf(err, "defining volume xml: %s", volumeXml.String())
	}
	defer vol.Free()

	p, err := d.generateCertBundle()
	if err != nil {
		return errors.Wrap(err, "generating cert bundle")
	}
	data := p.Bytes()

	// Write cert bundle and magic string to newly created volume
	stream, err := conn.NewStream(0)
	if err != nil {
		return errors.Wrap(err, "creating stream for volume")
	}
	defer func() {
		stream.Free()
	}()

	// Set the volume up to upload from stream
	if err := vol.Upload(stream, 0, uint64(len(data)), 0); err != nil {
		stream.Abort()
		return errors.Wrap(err, "uploading stream")
	}

	// Do the actual writing
	if n, err := stream.Send(data); err != nil || n != len(data) {
		return errors.Wrapf(err, "sending data, wrote %d bytes, expected %d bytes", n, len(data))
	}

	buf := make([]byte, 1e7)

	_, err = stream.Send(buf)
	if err != nil {
		return errors.Wrap(err, "sending sparse file to stream")
	}

	stream.Finish()
	/*
		if err := stream.Finish(); err != nil {
			return errors.Wrap(err, "finishing stream")
		}
	*/
	return nil
}

// func (d *Driver) createDiskImage() error {
// 	diskSize := fmt.Sprintf("%dM", d.DiskSize)
// 	cmd := exec.Command("qemu-img", "create", "-f", "qcow2", "-o", "preallocation=metadata", d.DiskPath, diskSize)
// 	output, err := cmd.CombinedOutput()
// 	if err != nil {
// 		return errors.Wrapf(err, "creating image using qemu-img: output: %s", output)
// 	}
// 	return nil
// }

func (d *Driver) generateCertBundle() (*bytes.Buffer, error) {
	magicString := "boot2docker, please format-me"

	if err := ssh.GenerateSSHKey(d.GetSSHKeyPath()); err != nil {
		return nil, errors.Wrap(err, "generating ssh key")
	}
	buf := new(bytes.Buffer)
	tw := tar.NewWriter(buf)

	file := &tar.Header{Name: magicString, Size: int64(len(magicString))}
	if err := tw.WriteHeader(file); err != nil {
		return nil, errors.Wrap(err, "writing magic string header to tar")
	}
	if _, err := tw.Write([]byte(magicString)); err != nil {
		return nil, errors.Wrap(err, "writing magic string to tar")
	}
	// .ssh/key.pub => authorized_keys
	file = &tar.Header{Name: ".ssh", Typeflag: tar.TypeDir, Mode: 0700}
	if err := tw.WriteHeader(file); err != nil {
		return nil, errors.Wrap(err, "writing .ssh header to tar")
	}
	pubKey, err := ioutil.ReadFile(d.publicSSHKeyPath())
	if err != nil {
		return nil, errors.Wrap(err, "reading ssh pub key for tar")
	}
	file = &tar.Header{Name: ".ssh/authorized_keys", Size: int64(len(pubKey)), Mode: 0644}
	if err := tw.WriteHeader(file); err != nil {
		return nil, errors.Wrap(err, "writing header for authorized_keys to tar")
	}
	if _, err := tw.Write([]byte(pubKey)); err != nil {
		return nil, errors.Wrap(err, "writing pub key to tar")
	}

	if err := tw.Close(); err != nil {
		return nil, errors.Wrap(err, "closing tar writer")
	}

	return buf, nil
}

func (d *Driver) publicSSHKeyPath() string {
	return d.GetSSHKeyPath() + ".pub"
}
