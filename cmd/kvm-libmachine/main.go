//
// main.go
// Copyright (C) 2016 Matt Rickard <m@rickard.email>
//
// Distributed under terms of the All Rights Reserved. license.
//

package main

import (
	"github.com/docker/machine/libmachine/drivers/plugin"
	kvm "github.com/r2d4/kvm-libmachine/pkg/kvm"
)

func main() {
	plugin.RegisterDriver(kvm.NewDriver("", ""))
}
