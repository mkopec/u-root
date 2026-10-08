// Copyright 2026 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fit

import (
	"bytes"
	"fmt"

	"github.com/u-root/u-root/pkg/dt"
)

// SelectForBoard selects a self-contained, uncompressed FIT configuration
// for the given board. It does not silently substitute the running FDT.
func (i *Image) SelectForBoard(compatible string) error {
	configs, ok := i.Root.RootNode.LookupChildByName("configurations")
	if !ok {
		return fmt.Errorf("FIT has no configurations")
	}
	for _, config := range configs.Children {
		if !nodeCompatible(config, compatible) {
			continue
		}
		prop, ok := config.LookProperty("kernel")
		if !ok {
			return fmt.Errorf("FIT config %s has no kernel", config.Name)
		}
		kernel, err := prop.AsString()
		if err != nil {
			return err
		}
		prop, ok = config.LookProperty("fdt")
		if !ok {
			return fmt.Errorf("FIT config %s has no fdt", config.Name)
		}
		fdtName, err := prop.AsString()
		if err != nil {
			return err
		}
		var ramdisk string
		if prop, ok = config.LookProperty("ramdisk"); ok {
			ramdisk, err = prop.AsString()
			if err != nil {
				return err
			}
		}
		prop, ok = config.LookProperty("bootargs")
		if !ok {
			return fmt.Errorf("FIT config %s has no bootargs", config.Name)
		}
		cmdline, err := prop.AsString()
		if err != nil || cmdline == "" {
			return fmt.Errorf("FIT config %s has invalid bootargs", config.Name)
		}
		if err := i.checkUncompressed(kernel, "kernel"); err != nil {
			return err
		}
		if ramdisk != "" {
			if err := i.checkUncompressed(ramdisk, "ramdisk"); err != nil {
				return err
			}
		}
		if err := i.checkUncompressed(fdtName, "flat_dt"); err != nil {
			return err
		}
		fdtData, err := i.ReadImage(fdtName)
		if err != nil {
			return err
		}
		target, err := dt.ReadFDT(fdtData)
		if err != nil || !nodeCompatible(target.RootNode, compatible) {
			return fmt.Errorf("FIT config %s has no matching board DTB", config.Name)
		}
		i.ConfigOverride = config.Name
		i.Kernel, i.InitRAMFS, i.DeviceTree = kernel, ramdisk, fdtName
		i.Cmdline = cmdline
		return nil
	}
	return fmt.Errorf("FIT has no configuration for %s", compatible)
}

func nodeCompatible(n *dt.Node, compatible string) bool {
	if n == nil {
		return false
	}
	prop, ok := n.LookProperty("compatible")
	if !ok {
		return false
	}
	return string(bytes.SplitN(prop.Value, []byte{0}, 2)[0]) == compatible
}

func (i *Image) checkUncompressed(name, typ string) error {
	images, ok := i.Root.RootNode.LookupChildByName("images")
	if !ok {
		return fmt.Errorf("FIT has no images")
	}
	node, ok := images.LookupChildByName(name)
	if !ok {
		return fmt.Errorf("FIT has no image %s", name)
	}
	p, ok := node.LookProperty("type")
	if !ok {
		return fmt.Errorf("FIT image %s has no type", name)
	}
	imageType, err := p.AsString()
	if err != nil || imageType != typ {
		return fmt.Errorf("FIT image %s is not a %s", name, typ)
	}
	if p, ok = node.LookProperty("compression"); ok {
		compression, err := p.AsString()
		if err != nil || compression != "none" {
			return fmt.Errorf("FIT image %s is compressed", name)
		}
	}
	if _, ok := node.LookProperty("data"); !ok {
		return fmt.Errorf("FIT image %s has no inline data", name)
	}
	return nil
}
