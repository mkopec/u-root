// Copyright 2026 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package grub

import (
	"bytes"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/u-root/u-root/pkg/dt"
)

// runningFDTPath is the FDT the current kernel was booted with.
var runningFDTPath = "/sys/firmware/fdt"

var runningCompatible = sync.OnceValue(func() string {
	fdt, err := dt.ReadFile(runningFDTPath)
	if err != nil {
		return ""
	}
	return fdtCompatible(fdt)
})

// fdtCompatible returns the first root "compatible" string, which names the
// exact board (e.g. "google,ciri-sku0").
func fdtCompatible(fdt *dt.FDT) string {
	p, ok := fdt.RootNode.LookProperty("compatible")
	if !ok {
		return ""
	}
	first, _, _ := strings.Cut(string(p.Value), "\x00")
	return first
}

// kernelVersion extracts the version from names like vmlinuz-6.12.0-1-arm64.
func kernelVersion(kernelPath string) string {
	base := filepath.Base(kernelPath)
	for _, prefix := range []string{"vmlinuz-", "vmlinux-", "Image-", "linux-", "kernel-"} {
		if v, ok := strings.CutPrefix(base, prefix); ok {
			return v
		}
	}
	return ""
}

// dtbDirs lists where distributions install the DTBs of a kernel, relative
// to the root of a filesystem. /boot may be a separate partition, hence the
// variants without the "boot/" prefix.
func dtbDirs(version string) []string {
	var dirs []string
	if version != "" {
		dirs = append(dirs,
			"usr/lib/linux-image-"+version, // Debian, Ubuntu
			"lib/linux-image-"+version,
			"boot/dtbs/"+version, "dtbs/"+version,
			"boot/dtb-"+version, "dtb-"+version, // Fedora, flash-kernel
			"lib/firmware/"+version+"/device-tree", // Ubuntu
		)
	}
	return append(dirs, "boot/dtbs", "dtbs") // Arch
}

func dtbFiles(dir string) []string {
	// Debian may make /usr/lib/linux-image-<version> a symlink into
	// /usr/lib/modules/<version>/dtb. WalkDir does not follow a symlink
	// at its root, so resolve the installation directory first.
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return nil
	}
	var files []string
	_ = filepath.WalkDir(resolved, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(resolved, path)
		if d.IsDir() && strings.Count(rel, string(filepath.Separator)) >= 2 {
			return filepath.SkipDir
		}
		if !d.IsDir() && strings.HasSuffix(d.Name(), ".dtb") {
			files = append(files, path)
		}
		return nil
	})
	return files
}

func dtbMatches(path, compatible string) bool {
	data, err := os.ReadFile(path)
	if err != nil || !bytes.Contains(data, []byte(compatible+"\x00")) {
		return false
	}
	fdt, err := dt.ReadFDT(bytes.NewReader(data))
	return err == nil && fdtCompatible(fdt) == compatible
}

// findDTBIn looks for a DTB of the running board below dir: first among files
// whose name contains the board part of the compatible, then among all.
func findDTBIn(dir, compatible string) string {
	files := dtbFiles(dir)
	_, board, _ := strings.Cut(compatible, ",")
	for _, f := range files {
		if board != "" && strings.Contains(filepath.Base(f), board) && dtbMatches(f, compatible) {
			return f
		}
	}
	for _, f := range files {
		if dtbMatches(f, compatible) {
			return f
		}
	}
	return ""
}

// GRUB configs list each kernel several times (recovery, submenus).
var dtbCache = struct {
	sync.Mutex
	paths map[string]string
}{paths: map[string]string{}}

// findDTB returns the device tree matching the running board that was
// installed with the kernel at kernelURL, or nil if there is none.
func (c *parser) findDTB(kernelURL string) io.ReaderAt {
	compatible := runningCompatible()
	if compatible == "" {
		return nil
	}
	u, err := parseURL(kernelURL, c.variables["root"])
	if err != nil || u.Scheme != "file" {
		return nil
	}
	kernelPath := u.Path

	dtbCache.Lock()
	defer dtbCache.Unlock()
	f, ok := dtbCache.paths[kernelPath]
	if !ok {
		f = c.searchDTB(kernelPath, compatible)
		dtbCache.paths[kernelPath] = f
		if f != "" {
			log.Printf("[grub] Using device tree %s for %s", f, kernelPath)
		}
	}
	if f == "" {
		return nil
	}
	dtb, err := os.Open(f)
	if err != nil {
		return nil
	}
	return dtb
}

func (c *parser) searchDTB(kernelPath, compatible string) string {
	// Search the kernel's own filesystem first, then all others.
	roots := []string{filepath.Dir(kernelPath), filepath.Dir(filepath.Dir(kernelPath))}
	for _, d := range c.devices {
		if mp, err := c.mountPool.Mount(d, mountFlags); err == nil {
			roots = append(roots, mp.Path)
		}
	}

	for _, sub := range dtbDirs(kernelVersion(kernelPath)) {
		for _, root := range roots {
			dir := filepath.Join(root, sub)
			if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
				continue
			}
			if f := findDTBIn(dir, compatible); f != "" {
				return f
			}
		}
	}
	return ""
}
