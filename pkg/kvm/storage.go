package kvm

import (
	"archive/tar"
	"bytes"
	"fmt"
	"io/ioutil"
	"os"

	"github.com/docker/machine/libmachine/ssh"
	"github.com/pkg/errors"
)

// func (d *Driver) createDiskImage() error {
// 	diskSize := fmt.Sprintf("%dM", d.DiskSize)
// 	cmd := exec.Command("qemu-img", "create", "-f", "qcow2", "-o", "preallocation=metadata", d.DiskPath, diskSize)
// 	output, err := cmd.CombinedOutput()
// 	if err != nil {
// 		return errors.Wrapf(err, "creating image using qemu-img: output: %s", output)
// 	}
// 	return nil
// }

func createRawDiskImage(dest string, size int64) error {
	f, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		if os.IsExist(err) {
			return nil
		}
		return errors.Wrap(err, "opening file for raw disk image")
	}
	f.Close()

	if err := os.Truncate(dest, size<<20); err != nil {
		return errors.Wrap(err, "writing sparse file")
	}

	return nil
}

func (d *Driver) buildDiskImage() error {
	diskPath := d.ResolveStorePath(fmt.Sprintf("%s.img", d.MachineName))
	err := createRawDiskImage(diskPath, d.DiskSize)
	if err := createRawDiskImage(diskPath, d.DiskSize); err != nil {
		return errors.Wrap(err, "creating raw disk image")
	}
	tarBuf, err := d.generateCertBundle()
	if err != nil {
		return errors.Wrap(err, "generating cert bundle")
	}
	f, err := os.OpenFile(d.DiskPath, os.O_WRONLY, 0644)
	if err != nil {
		return errors.Wrap(err, "opening raw disk image to write cert bundle")
	}
	defer f.Close()

	f.Seek(0, os.SEEK_SET)
	_, err = f.Write(tarBuf.Bytes())
	if err != nil {
		return errors.Wrap(err, "wrting cert bundle to disk image")
	}

	return nil
}

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
