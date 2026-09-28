// Copyright 2026 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package boot

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/u-root/u-root/pkg/boot/kexec"
)

func setIOMem(t *testing.T, contents string) {
	t.Helper()
	old := iomemPath
	t.Cleanup(func() { iomemPath = old })
	iomemPath = filepath.Join(t.TempDir(), "iomem")
	if err := os.WriteFile(iomemPath, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestIOMemReservations(t *testing.T) {
	// Excerpt of /proc/iomem from LinuxBoot on Ciri.
	setIOMem(t, `11001100-1100111f : serial
40000000-4fffffff : System RAM
  40000000-4001dfff : reserved
  40c00000-4119ffff : Kernel code
  411a0000-4120ffff : reserved
  41210000-4133ffff : Kernel data
  41400000-417c0fff : reserved
50000000-507fffff : reserved
50800000-545fffff : System RAM
70000000-70bfffff : reserved
  70000000-70bfffff : simplefb
70c00000-fffdafff : System RAM
`)
	rs, err := iomemReservations()
	if err != nil {
		t.Fatal(err)
	}
	overlaps := func(r kexec.Range) bool {
		for _, x := range rs {
			if x.Overlaps(r) {
				return true
			}
		}
		return false
	}
	for _, r := range []kexec.Range{
		kexec.RangeFromInclusiveInterval(0x40000000, 0x4001dfff), // running FDT
		kexec.RangeFromInclusiveInterval(0x40c00000, 0x4119ffff), // Kernel code
		kexec.RangeFromInclusiveInterval(0x41210000, 0x4133ffff), // Kernel data
		kexec.RangeFromInclusiveInterval(0x41400000, 0x417c0fff),
		kexec.RangeFromInclusiveInterval(0x50000000, 0x507fffff),
		kexec.RangeFromInclusiveInterval(0x70000000, 0x70bfffff), // simplefb
	} {
		if !overlaps(r) {
			t.Errorf("%s is not reserved", r)
		}
	}
	for _, r := range []kexec.Range{
		kexec.RangeFromInclusiveInterval(0x4001e000, 0x40bfffff),
		kexec.RangeFromInclusiveInterval(0x417c1000, 0x4fffffff),
		kexec.RangeFromInclusiveInterval(0x70c00000, 0xfffdafff),
	} {
		if overlaps(r) {
			t.Errorf("free RAM %s is reserved", r)
		}
	}
}

func TestIOMemReservationsUnprivileged(t *testing.T) {
	// Without CAP_SYS_ADMIN, every address reads as zero.
	setIOMem(t, "00000000-00000000 : System RAM\n00000000-00000000 : reserved\n")
	if _, err := iomemReservations(); err == nil {
		t.Fatal("unprivileged /proc/iomem was accepted")
	}
}
