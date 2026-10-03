// Copyright 2026 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package linux

import (
	"testing"

	"github.com/u-root/u-root/pkg/dt"
)

func TestMergeFirmwareFDTFirmwareNodes(t *testing.T) {
	running := &dt.FDT{RootNode: dt.NewNode("/", dt.WithChildren(
		dt.NewNode("firmware",
			dt.WithProperty(dt.PropertyU32("#address-cells", 2)),
			dt.WithChildren(
				dt.NewNode("coreboot", dt.WithProperty(dt.PropertyString("compatible", "coreboot"))),
				dt.NewNode("optee",
					dt.WithProperty(dt.PropertyString("compatible", "linaro,optee-tz")),
					dt.WithProperty(dt.PropertyString("method", "smc"))),
				dt.NewNode("other"),
			)),
	))}
	target := &dt.FDT{RootNode: dt.NewNode("/", dt.WithChildren(
		dt.NewNode("firmware", dt.WithChildren(
			dt.NewNode("optee", dt.WithProperty(dt.PropertyString("method", "hvc"))),
		)),
	))}

	mergeFirmwareFDT(target, running)

	fw, ok := target.RootNode.LookupChildByName("firmware")
	if !ok {
		t.Fatal("no /firmware in the merged tree")
	}
	if _, ok := fw.LookProperty("#address-cells"); !ok {
		t.Error("/firmware lost the running tree's #address-cells")
	}
	if _, ok := fw.LookupChildByName("coreboot"); !ok {
		t.Error("/firmware/coreboot was not copied")
	}
	if _, ok := fw.LookupChildByName("other"); ok {
		t.Error("/firmware/other was copied")
	}
	var optee []*dt.Node
	for _, c := range fw.Children {
		if c.Name == "optee" {
			optee = append(optee, c)
		}
	}
	if len(optee) != 1 {
		t.Fatalf("got %d /firmware/optee nodes, want 1", len(optee))
	}
	m, ok := optee[0].LookProperty("method")
	if !ok {
		t.Fatal("/firmware/optee has no method")
	}
	if s, err := m.AsString(); err != nil || s != "smc" {
		t.Errorf("/firmware/optee method = %q, %v, want the running tree's \"smc\"", s, err)
	}
}
