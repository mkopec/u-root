// Copyright 2026 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package linux

import (
	"github.com/u-root/u-root/pkg/dt"
)

func isMemoryNode(n *dt.Node) bool {
	p, ok := n.LookProperty("device_type")
	if !ok {
		return false
	}
	s, err := p.AsString()
	return err == nil && s == "memory"
}

// mergeFirmwareFDT copies the state that firmware fills in at boot from the
// running kernel's FDT into a DTB shipped with the next kernel. Such a DTB is
// only a template: its memory node has no size, and it lacks the
// firmware-provided reservations and nodes.
//
// Copied from running:
//   - all device_type = "memory" nodes, replacing the target's,
//   - memory reservation block entries (e.g. coreboot's CBMEM),
//   - /firmware/coreboot, the pointer to the coreboot tables,
//   - /reserved-memory no-map nodes the target lacks. The running kernel
//     has no linear mapping there, and arm64 kexec relocates segments with
//     the MMU on, so no segment may be placed in them. They also keep
//     memory that firmware left in use (e.g. a scanout buffer) intact.
//   - /chosen simple-framebuffer nodes, for an early console until the
//     target's display driver takes over.
//
// Everything else, including the rest of /chosen, comes from target.
// Copied nodes lose phandle and memory-region properties, which could
// collide with or point into the target's phandle numbering.
func mergeFirmwareFDT(target, running *dt.FDT) {
	root := target.RootNode

	var kept []*dt.Node
	for _, n := range root.Children {
		if !isMemoryNode(n) {
			kept = append(kept, n)
		}
	}
	for _, n := range running.RootNode.Children {
		if isMemoryNode(n) {
			kept = append(kept, n)
		}
	}
	root.Children = kept

	for _, r := range running.ReserveEntries {
		found := false
		for _, t := range target.ReserveEntries {
			if t == r {
				found = true
				break
			}
		}
		if !found {
			target.ReserveEntries = append(target.ReserveEntries, r)
		}
	}

	if rf, ok := running.RootNode.LookupChildByName("firmware"); ok {
		if cb, ok := rf.LookupChildByName("coreboot"); ok {
			tf, ok := root.LookupChildByName("firmware")
			if !ok {
				tf = dt.NewNode("firmware")
				root.Children = append(root.Children, tf)
			}
			// coreboot adds an empty "ranges" (and possibly cell sizes)
			// so that the child's "reg" translates.
			for _, p := range rf.Properties {
				if _, ok := tf.LookProperty(p.Name); !ok {
					tf.Properties = append(tf.Properties, p)
				}
			}
			if idx, ok := tf.FindFirstMatchingChildIndex(func(c *dt.Node) bool {
				return c.Name == "coreboot"
			}); ok {
				tf.Children[idx] = cb
			} else {
				tf.Children = append(tf.Children, cb)
			}
		}
	}

	if rr, ok := running.RootNode.LookupChildByName("reserved-memory"); ok {
		tr, ok := root.LookupChildByName("reserved-memory")
		if !ok {
			tr = dt.NewNode("reserved-memory")
			for _, p := range rr.Properties {
				tr.Properties = append(tr.Properties, p)
			}
			root.Children = append(root.Children, tr)
		}
		for _, n := range rr.Children {
			if _, noMap := n.LookProperty("no-map"); !noMap {
				continue
			}
			if _, exists := tr.LookupChildByName(n.Name); exists {
				continue
			}
			tr.Children = append(tr.Children, withoutRefs(n))
		}
	}

	chosen, ok := root.LookupChildByName("chosen")
	if !ok {
		chosen = dt.NewNode("chosen")
		root.Children = append(root.Children, chosen)
	}
	if rc, ok := running.RootNode.LookupChildByName("chosen"); ok {
		for _, n := range rc.Children {
			p, ok := n.LookProperty("compatible")
			if !ok || string(p.Value) != "simple-framebuffer\x00" {
				continue
			}
			if _, exists := chosen.LookupChildByName(n.Name); !exists {
				chosen.Children = append(chosen.Children, withoutRefs(n))
			}
		}
	}
}

// withoutRefs returns a copy of n without properties holding phandles.
func withoutRefs(n *dt.Node) *dt.Node {
	c := dt.NewNode(n.Name)
	for _, p := range n.Properties {
		switch p.Name {
		case "phandle", "linux,phandle", "memory-region":
			continue
		}
		c.Properties = append(c.Properties, p)
	}
	return c
}
