package fit

import (
	"bytes"
	"io"
	"testing"

	"github.com/u-root/u-root/pkg/boot"
	"github.com/u-root/u-root/pkg/dt"
)

func testBoardFIT(t *testing.T) *Image {
	t.Helper()
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
				img("ramdisk", "ramdisk", []byte("initrd")), img("fdt", "flat_dt", dtb.Bytes()))),
			dt.NewNode("configurations", dt.WithChildren(dt.NewNode("ciri", dt.WithProperty(
				dt.PropertyString("compatible", "google,ciri-sku0"),
				dt.PropertyString("kernel", "kernel"), dt.PropertyString("ramdisk", "ramdisk"),
				dt.PropertyString("fdt", "fdt"), dt.PropertyString("bootargs", "root=UUID=test ro")))))))}
	return &Image{Root: container}
}

func TestSelectBoardFIT(t *testing.T) {
	i := testBoardFIT(t)
	if err := i.SelectForBoard("google,ciri-sku1"); err == nil {
		t.Fatal("selected wrong board")
	}
	if err := i.SelectForBoard("google,ciri-sku0"); err != nil {
		t.Fatal(err)
	}
	if i.Kernel != "kernel" || i.DeviceTree != "fdt" || i.Cmdline != "root=UUID=test ro" {
		t.Fatalf("wrong selection: %+v", i)
	}
	old := loadImage
	defer func() { loadImage = old }()
	loadImage = func(li *boot.LinuxImage, _ ...boot.LoadOption) error {
		if !li.LoadSyscall || li.FileLoadFallback || li.DTB == nil {
			t.Fatal("FIT must pass its DTB via kexec_load without fallback")
		}
		var magic [4]byte
		if _, err := li.DTB.ReadAt(magic[:], 0); err != nil || !bytes.Equal(magic[:], []byte{0xd0, 0x0d, 0xfe, 0xed}) {
			t.Fatalf("invalid DTB: %x: %v", magic, err)
		}
		kernel := make([]byte, 5)
		if _, err := li.Kernel.ReadAt(kernel, 0); err != nil && err != io.EOF {
			t.Fatal(err)
		}
		if string(kernel) != "Image" {
			t.Fatalf("kernel = %q", kernel)
		}
		return nil
	}
	if err := i.Load(); err != nil {
		t.Fatal(err)
	}
}

func TestSelectBoardFITRejectsCompressionAndBadDTB(t *testing.T) {
	i := testBoardFIT(t)
	images, _ := i.Root.RootNode.LookupChildByName("images")
	kernel, _ := images.LookupChildByName("kernel")
	compression, _ := kernel.LookProperty("compression")
	*compression = dt.PropertyString("compression", "gzip")
	if err := i.SelectForBoard("google,ciri-sku0"); err == nil {
		t.Fatal("accepted compressed kernel")
	}
	*compression = dt.PropertyString("compression", "none")
	fdt, _ := images.LookupChildByName("fdt")
	data, _ := fdt.LookProperty("data")
	data.Value = []byte("not a DTB")
	if err := i.SelectForBoard("google,ciri-sku0"); err == nil {
		t.Fatal("accepted invalid DTB")
	}
}
