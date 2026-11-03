package tests

import (
	"bytes"
	"encoding/binary"
	"image/png"
	"testing"

	"houridle/src/tray"
)

// The tray icon is drawn in code; check it's a valid .ico Windows can load.
func TestTrayIconIsValidICO(t *testing.T) {
	ico := tray.Icon()
	var header [3]uint16 // reserved, type, image count
	binary.Read(bytes.NewReader(ico), binary.LittleEndian, &header)
	if header[0] != 0 || header[1] != 1 || header[2] == 0 {
		t.Fatalf("bad ICO header %v", header)
	}

	for i := range int(header[2]) {
		entry := ico[6+16*i:]
		size := int(entry[0])
		if entry[1] != entry[0] {
			t.Fatalf("image %d isn't square: %dx%d", i, entry[0], entry[1])
		}
		length := binary.LittleEndian.Uint32(entry[8:])
		offset := binary.LittleEndian.Uint32(entry[12:])
		img, err := png.Decode(bytes.NewReader(ico[offset : offset+length]))
		if err != nil {
			t.Fatalf("image %d doesn't decode: %v", i, err)
		}
		if img.Bounds().Dx() != size {
			t.Fatalf("image %d is %dpx, header says %d", i, img.Bounds().Dx(), size)
		}
		if _, _, _, a := img.At(size/2, size/2).RGBA(); a == 0 {
			t.Fatalf("image %d: the middle should be filled", i)
		}
		if _, _, _, a := img.At(0, 0).RGBA(); a != 0 {
			t.Fatalf("image %d: the corner should be transparent", i)
		}
	}
}
