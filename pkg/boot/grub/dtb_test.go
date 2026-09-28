package grub

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDTBFilesSymlinkedDebianDirectory(t *testing.T) {
	root := t.TempDir()
	lib := filepath.Join(root, "usr", "lib")
	dtbs := filepath.Join(lib, "modules", "7.2.6+deb14-arm64", "dtb")
	file := filepath.Join(dtbs, "mediatek", "mt8188-geralt-ciri-sku0.dtb")
	if err := os.MkdirAll(filepath.Dir(file), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("fixture"), 0644); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(lib, "linux-image-7.2.6+deb14-arm64")
	if err := os.Symlink("modules/7.2.6+deb14-arm64/dtb", alias); err != nil {
		t.Fatal(err)
	}
	files := dtbFiles(alias)
	if len(files) != 1 || files[0] != file {
		t.Fatalf("dtbFiles(%s) = %v, want [%s]", alias, files, file)
	}
}
