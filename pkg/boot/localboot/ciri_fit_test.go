package localboot

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/u-root/u-root/pkg/boot/fit"
	"github.com/u-root/u-root/pkg/dt"
)

func TestScanBoardFIT(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "boot", "fit")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	board := &dt.FDT{Header: dt.Header{Magic: 0xd00dfeed, Version: 17, LastCompVersion: 16},
		RootNode: dt.NewNode("", dt.WithProperty(dt.PropertyString("compatible", "google,ciri-sku0")))}
	var dtb bytes.Buffer
	if _, err := board.Write(&dtb); err != nil {
		t.Fatal(err)
	}
	img := func(name, typ string, data []byte) *dt.Node {
		return dt.NewNode(name, dt.WithProperty(dt.PropertyString("type", typ),
			dt.PropertyString("compression", "none"), dt.Property{Name: "data", Value: data}))
	}
	container := &dt.FDT{Header: dt.Header{Magic: 0xd00dfeed, Version: 17, LastCompVersion: 16},
		RootNode: dt.NewNode("", dt.WithChildren(
			dt.NewNode("images", dt.WithChildren(img("kernel", "kernel", []byte("Image")),
				img("fdt", "flat_dt", dtb.Bytes()))),
			dt.NewNode("configurations", dt.WithChildren(dt.NewNode("ciri", dt.WithProperty(
				dt.PropertyString("compatible", "google,ciri-sku0"),
				dt.PropertyString("kernel", "kernel"), dt.PropertyString("fdt", "fdt"),
				dt.PropertyString("bootargs", "root=UUID=test ro")))))))}
	file, err := os.Create(filepath.Join(dir, "ciri-test.itb"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := container.Write(file); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if got := scanFITs(root, "google,ciri-sku1"); len(got) != 0 {
		t.Fatalf("wrong board: %v", got)
	}
	got := scanFITs(root, "google,ciri-sku0")
	if len(got) != 1 {
		t.Fatalf("wanted one FIT, got %v", got)
	}
	image, ok := got[0].(*fit.Image)
	if !ok || image.DeviceTree != "fdt" || image.Cmdline != "root=UUID=test ro" {
		t.Fatalf("invalid FIT selection: %v", got[0])
	}
}
