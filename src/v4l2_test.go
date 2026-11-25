package main

import (
	"image"
	"testing"
)

func TestNV21(t *testing.T) {
	// 2x2 frame: four Y values, then one V/U pair.
	img, err := nv21([]byte{100, 110, 110, 120, 200, 50}, 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	// Mean Y is already 110 and the one chroma pair is moved to neutral by the balance (run by the encoders).
	y := img.(*image.YCbCr)
	balance(y)
	if y.Y[0] != 100 || y.Y[3] != 120 || y.Cr[0] != 128 || y.Cb[0] != 128 {
		t.Fatal(y.Y, y.Cb, y.Cr)
	}
	dark := &image.YCbCr{Y: []byte{10, 30}, Cb: []byte{128}, Cr: []byte{140}}
	balance(dark)
	if dark.Y[0] != 40 || dark.Y[1] != 120 || dark.Cr[0] != 128 {
		t.Fatal("4x gain at most, chroma neutral", dark.Y, dark.Cr)
	}
	if _, err := nv21(make([]byte, 5), 2, 2); err == nil {
		t.Fatal("short frame accepted")
	}
}
