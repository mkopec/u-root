// Copyright 2026 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package pez contains a Extractor for the PE compressed Linux Image (vmlinuz, ZBOOT).
package pez

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/klauspost/compress/zstd"
)

// Linux PE zboot header as defined in Linux source
// drivers/firmware/efi/libstub/zboot-header.S
type Header struct {
	Magic       uint32
	Type        uint32
	Offset      uint32
	Size        uint32
	Reserved    [2]uint32
	Compression [4]byte
}

const (
	magic      = 0x00005a4d
	typeZImage = 0x676d697a // "zimg"

	// dosHeaderPEOffsetAddr is the offset in the MS-DOS header (0x3c) where
	// the 4-byte offset to the PE header is located.
	dosHeaderPEOffsetAddr = 0x3c

	// peHeaderOffsetSize is the size of the PE header offset pointer (4 bytes).
	peHeaderOffsetSize = 4

	// peSignature is the 4-byte signature "PE\x00\x00" that marks the start of the PE header.
	peSignature    = "PE\x00\x00"
	peSignatureLen = 4

	// coffNumSectionsOffset is the offset in bytes from the start of the PE header
	// to the NumberOfSections field in the COFF File Header.
	// PE Signature (4 bytes) + Machine (2 bytes) = 6 bytes.
	coffNumSectionsOffset = 6
	coffNumSectionsSize   = 2

	// coffOptHeaderSizeOffset is the offset in bytes from the start of the PE header
	// to the SizeOfOptionalHeader field in the COFF File Header.
	// PE Signature (4 bytes) + COFF File Header fields up to SizeOfOptionalHeader (16 bytes) = 20 bytes.
	coffOptHeaderSizeOffset = 20
	coffOptHeaderSizeSize   = 2

	// coffHeaderSize is the size of the combined PE signature (4 bytes) and COFF File Header (20 bytes).
	coffHeaderSize = 24

	// peSectionEntrySize is the size of each section header entry in the Section Table (40 bytes).
	peSectionEntrySize = 40

	// peSectionNameSize is the maximum size of a section name in the section header (8 bytes).
	peSectionNameSize = 8

	// peSectionVirtualSizeOffset is the offset of the VirtualSize field
	// relative to the start of a section header entry.
	peSectionVirtualSizeOffset = 8
	// peSectionRawSizeOffset is the offset of the SizeOfRawData field relative to the start of a section header entry.
	peSectionRawSizeOffset = 16

	// peSectionRawOffsetOffset is the offset of the PointerToRawData field relative to the start of a section header entry.
	peSectionRawOffsetOffset = 20

	// pezHeaderSize is the total size of the PEZ (ZBOOT) compressed image header (28 bytes).
	pezHeaderSize = 28
)

var ErrImageTooSmall = errors.New("image too small")
var ErrMagicMismatch = errors.New("magic number mismatch")
var ErrNotZImage = errors.New("not a zimg")
var ErrUnsupportedCompression = errors.New("unsupported compression type")

// readAt is a helper function that reads a specified number of bytes from the io.ReaderAt stream.
// It handles EOF, unexpected EOF, and partial reads cleanly.
func readAt(img io.ReaderAt, offset int64, size int) ([]byte, error) {
	buf := make([]byte, size)
	n, err := img.ReadAt(buf, offset)
	if err != nil && err != io.EOF {
		return nil, err
	}
	// If we read 0 bytes because we are exactly at the end of the file (offset == file size),
	// we want to return a clean io.EOF rather than io.ErrUnexpectedEOF.
	if n == 0 && err == io.EOF {
		return nil, io.EOF
	}
	if n < size {
		return nil, io.ErrUnexpectedEOF
	}
	return buf, nil
}

// section is a section of a PE/COFF image.
type section struct {
	name   string
	offset int64
	size   int64
}

// sections returns the sections of a PE/COFF image, in the order of the
// section table.
func sections(img io.ReaderAt) ([]section, error) {
	// Read the offset pointing to the start of the PE header from the DOS stub (0x3c).
	buf, err := readAt(img, dosHeaderPEOffsetAddr, peHeaderOffsetSize)
	if err != nil {
		return nil, err
	}
	peOffset := binary.LittleEndian.Uint32(buf)

	// Read and verify the PE signature ("PE\x00\x00") at the start of the PE header.
	buf, err = readAt(img, int64(peOffset), peSignatureLen)
	if err != nil {
		return nil, err
	}
	if string(buf) != peSignature {
		return nil, fmt.Errorf("invalid PE signature")
	}

	// Read the number of sections from the COFF File Header.
	buf, err = readAt(img, int64(peOffset)+coffNumSectionsOffset, coffNumSectionsSize)
	if err != nil {
		return nil, err
	}
	numSections := binary.LittleEndian.Uint16(buf)

	// Read the optional header size from the COFF File Header to compute where the Section Table begins.
	buf, err = readAt(img, int64(peOffset)+coffOptHeaderSizeOffset, coffOptHeaderSizeSize)
	if err != nil {
		return nil, err
	}
	optHeaderSize := binary.LittleEndian.Uint16(buf)

	// The Section Table starts immediately after the Optional Header (PE Signature + COFF Header + Optional Header).
	sectionTableOffset := int64(peOffset) + coffHeaderSize + int64(optHeaderSize)
	var secs []section
	for i := 0; i < int(numSections); i++ {
		// Seek to each 40-byte section entry and read it.
		entryOffset := sectionTableOffset + int64(i*peSectionEntrySize)
		buf, err = readAt(img, entryOffset, peSectionEntrySize)
		if err != nil {
			return nil, err
		}

		// Read the 8-character section name and trim trailing null bytes.
		name := string(bytes.TrimRight(buf[0:peSectionNameSize], "\x00"))
		virtSize := binary.LittleEndian.Uint32(buf[peSectionVirtualSizeOffset : peSectionVirtualSizeOffset+4])
		rawSize := binary.LittleEndian.Uint32(buf[peSectionRawSizeOffset : peSectionRawSizeOffset+4])
		rawOffset := binary.LittleEndian.Uint32(buf[peSectionRawOffsetOffset : peSectionRawOffsetOffset+4])

		// The raw data is padded to the file alignment. The data itself
		// is VirtualSize bytes long, if that is set.
		size := rawSize
		if virtSize != 0 && virtSize < rawSize {
			size = virtSize
		}
		secs = append(secs, section{name: name, offset: int64(rawOffset), size: int64(size)})
	}
	return secs, nil
}

// findLinuxSection locates the ".linux" section of a PE/COFF image, which
// houses the kernel in a unified kernel image (UKI).
// Returns the file offset and size of the section if found, or an error.
func findLinuxSection(img io.ReaderAt) (int64, int64, error) {
	secs, err := sections(img)
	if err != nil {
		return 0, 0, err
	}
	for _, s := range secs {
		if s.name == ".linux" {
			return s.offset, s.size, nil
		}
	}
	return 0, 0, fmt.Errorf("no .linux section found")
}

// ErrNotUKI is returned for images without a .linux section.
var ErrNotUKI = errors.New("not a unified kernel image")

// UKI holds the sections of a unified kernel image (UKI) or a kernel image
// with a stub such as systemd-stub or stubble, that a loader that doesn't run
// the stub needs.
type UKI struct {
	// Linux is the kernel, which may be compressed itself.
	Linux io.ReaderAt
	// DTB is the device tree of the .dtb section, or nil.
	DTB io.ReaderAt
	// DTBAuto are the device trees of the .dtbauto sections, among
	// which the stub picks the one matching the board.
	DTBAuto []io.ReaderAt
}

// ParseUKI returns the sections of a unified kernel image, or ErrNotUKI.
func ParseUKI(img io.ReaderAt) (*UKI, error) {
	secs, err := sections(img)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrNotUKI, err)
	}
	u := &UKI{}
	for _, s := range secs {
		r := io.NewSectionReader(img, s.offset, s.size)
		switch s.name {
		case ".linux":
			u.Linux = r
		case ".dtb":
			u.DTB = r
		case ".dtbauto":
			u.DTBAuto = append(u.DTBAuto, r)
		}
	}
	if u.Linux == nil {
		return nil, ErrNotUKI
	}
	return u, nil
}

// Extract extracts and decompresses the embedded bootable ARM64 kernel payload
// from either a raw ZBOOT EFI executable or an outer PE/COFF Unified Kernel Image (UKI).
// It returns an io.ReaderAt stream for the raw decompressed kernel image.
func Extract(img io.ReaderAt) (io.ReaderAt, error) {
	// First, check if the input image is wrapped inside a PE/COFF executable.
	// If so, locate and extract the ".linux" section.
	if offset, size, err := findLinuxSection(img); err == nil {
		img = io.NewSectionReader(img, offset, size)
	}

	// Read the 28-byte PEZ / ZBOOT header.
	buf, err := readAt(img, 0, pezHeaderSize)
	if err != nil {
		return nil, err
	}

	header := Header{
		Magic:  binary.LittleEndian.Uint32(buf[0:4]),
		Type:   binary.LittleEndian.Uint32(buf[4:8]),
		Offset: binary.LittleEndian.Uint32(buf[8:12]),
		Size:   binary.LittleEndian.Uint32(buf[12:16]),
	}
	header.Reserved[0] = binary.LittleEndian.Uint32(buf[16:20])
	header.Reserved[1] = binary.LittleEndian.Uint32(buf[20:24])
	copy(header.Compression[:], buf[24:28])

	// Validate the ZBOOT magic and image type.
	if header.Magic != magic {
		return nil, fmt.Errorf("found magic %#x but PEZ expects %#x: %w", header.Magic, magic, ErrMagicMismatch)
	}
	if header.Type != typeZImage {
		return nil, fmt.Errorf("found type %#x but PEZ expects %#x: %w", header.Type, typeZImage, ErrNotZImage)
	}

	// Slice the raw compressed payload based on header offset and size.
	payload := io.NewSectionReader(img, int64(header.Offset), int64(header.Size))

	// Trim trailing null bytes from the compression algorithm name without extra allocations.
	compression := string(bytes.TrimRight(header.Compression[:], "\x00"))

	// Decompress the payload according to its compression algorithm.
	switch compression {
	case "zstd":
		r, err := zstd.NewReader(payload)
		if err != nil {
			return nil, err
		}
		decompressed, err := io.ReadAll(r)
		if err != nil {
			return nil, err
		}
		return bytes.NewReader(decompressed), nil
	default:
		return nil, fmt.Errorf("unsupported compression type %q (supported compressions: zstd): %w", compression, ErrUnsupportedCompression)
	}
}
