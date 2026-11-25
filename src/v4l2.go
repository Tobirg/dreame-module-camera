package main

import (
	"encoding/binary"
	"errors"
	"image"
	"syscall"
	"time"
	"unsafe"
)

// V4L2 ioctls and struct layouts for linux/arm64 (sunxi-vin, multi-planar capture).
const (
	vidiocSFmt     = 0xc0d05605
	vidiocReqBufs  = 0xc0145608
	vidiocQueryBuf = 0xc0585609
	vidiocQBuf     = 0xc058560f
	vidiocDQBuf    = 0xc0585611
	vidiocStreamOn = 0x40045612
	vidiocStreamOf = 0x40045613
	vidiocSInput   = 0xc0045627
	bufTypeMplane  = 9
	memoryMmap     = 1
	numBufs        = 3
)

type camera struct {
	fd   int
	w, h int
	mem  [][]byte
}

func ioctl(fd int, req uintptr, p unsafe.Pointer) error {
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), req, uintptr(p)); e != 0 {
		return e
	}
	return nil
}

// buffer is a v4l2_buffer with its single v4l2_plane.
type buffer struct {
	b     [88]byte
	plane [64]byte
}

func newBuffer(i int) *buffer {
	x := &buffer{}
	binary.LittleEndian.PutUint32(x.b[0:], uint32(i))
	binary.LittleEndian.PutUint32(x.b[4:], bufTypeMplane)
	binary.LittleEndian.PutUint32(x.b[60:], memoryMmap)
	binary.LittleEndian.PutUint64(x.b[64:], uint64(uintptr(unsafe.Pointer(&x.plane))))
	binary.LittleEndian.PutUint32(x.b[72:], 1)
	return x
}

var errAsleep = errors.New("camera sensor is not powered (robot asleep)")

// openCamera starts NV21 capture; the sensor answers only while the robot is awake.
func openCamera(dev string, w, h int) (*camera, error) {
	fd, err := syscall.Open(dev, syscall.O_RDWR|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	c := &camera{fd: fd}
	in := int32(0)
	if ioctl(fd, vidiocSInput, unsafe.Pointer(&in)) != nil {
		c.close()
		return nil, errAsleep
	}
	var f [208]byte
	binary.LittleEndian.PutUint32(f[0:], bufTypeMplane)
	binary.LittleEndian.PutUint32(f[8:], uint32(w))
	binary.LittleEndian.PutUint32(f[12:], uint32(h))
	copy(f[16:20], "NV21")
	binary.LittleEndian.PutUint32(f[20:], 1) // V4L2_FIELD_NONE
	f[188] = 1                               // one plane
	if err := ioctl(fd, vidiocSFmt, unsafe.Pointer(&f)); err != nil {
		c.close()
		return nil, err
	}
	c.w, c.h = int(binary.LittleEndian.Uint32(f[8:])), int(binary.LittleEndian.Uint32(f[12:]))
	var rb [20]byte
	binary.LittleEndian.PutUint32(rb[0:], numBufs)
	binary.LittleEndian.PutUint32(rb[4:], bufTypeMplane)
	binary.LittleEndian.PutUint32(rb[8:], memoryMmap)
	if err := ioctl(fd, vidiocReqBufs, unsafe.Pointer(&rb)); err != nil {
		c.close()
		return nil, err
	}
	for i := 0; i < int(binary.LittleEndian.Uint32(rb[0:])); i++ {
		x := newBuffer(i)
		if err := ioctl(fd, vidiocQueryBuf, unsafe.Pointer(&x.b)); err != nil {
			c.close()
			return nil, err
		}
		m, err := syscall.Mmap(fd, int64(binary.LittleEndian.Uint32(x.plane[8:])), int(binary.LittleEndian.Uint32(x.plane[4:])),
			syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
		if err != nil {
			c.close()
			return nil, err
		}
		c.mem = append(c.mem, m)
		if err := ioctl(fd, vidiocQBuf, unsafe.Pointer(&newBuffer(i).b)); err != nil {
			c.close()
			return nil, err
		}
	}
	t := int32(bufTypeMplane)
	if err := ioctl(fd, vidiocStreamOn, unsafe.Pointer(&t)); err != nil {
		c.close()
		return nil, err
	}
	return c, nil
}

// frame waits for the next frame and returns it as an image (NV21: Y plane, then V/U pairs).
func (c *camera) frame(timeout time.Duration) (image.Image, error) {
	deadline := time.Now().Add(timeout)
	x := newBuffer(0)
	for ioctl(c.fd, vidiocDQBuf, unsafe.Pointer(&x.b)) != nil {
		if time.Now().After(deadline) {
			return nil, errors.New("no frame from the camera")
		}
		time.Sleep(15 * time.Millisecond)
	}
	i := int(binary.LittleEndian.Uint32(x.b[0:]))
	// Older frames may wait in the queue: take the newest one, so the picture lags by no more than a frame.
	for {
		y := newBuffer(0)
		if ioctl(c.fd, vidiocDQBuf, unsafe.Pointer(&y.b)) != nil {
			break
		}
		_ = ioctl(c.fd, vidiocQBuf, unsafe.Pointer(&newBuffer(i).b))
		i = int(binary.LittleEndian.Uint32(y.b[0:]))
	}
	defer ioctl(c.fd, vidiocQBuf, unsafe.Pointer(&newBuffer(i).b))
	return nv21(c.mem[i], c.w, c.h)
}

func nv21(data []byte, w, h int) (image.Image, error) {
	if len(data) < w*h*3/2 {
		return nil, errors.New("short frame")
	}
	img := image.NewYCbCr(image.Rect(0, 0, w, h), image.YCbCrSubsampleRatio420)
	copy(img.Y, data[:w*h])
	uv := data[w*h:]
	for i := range img.Cb {
		img.Cr[i], img.Cb[i] = uv[2*i], uv[2*i+1]
	}
	return img, nil
}

func (c *camera) close() {
	if c.mem != nil {
		t := int32(bufTypeMplane)
		_ = ioctl(c.fd, vidiocStreamOf, unsafe.Pointer(&t))
	}
	for _, m := range c.mem {
		_ = syscall.Munmap(m)
	}
	_ = syscall.Close(c.fd)
}

// balance stands in for the ISP's auto white balance and exposure, which run in the firmware's own
// camera stack and not for plain V4L2 capture: gray-world colour (mean chroma to neutral) and a
// brightness gain towards a mid-grey mean, at most 4x.
func balance(img *image.YCbCr) {
	if len(img.Y) == 0 || len(img.Cb) == 0 {
		return
	}
	var y, cb, cr int
	for _, v := range img.Y {
		y += int(v)
	}
	for i := range img.Cb {
		cb, cr = cb+int(img.Cb[i]), cr+int(img.Cr[i])
	}
	meanY := float64(y) / float64(len(img.Y))
	gain := 110 / max(meanY, 1)
	gain = min(max(gain, 1), 4)
	dcb, dcr := 128-cb/len(img.Cb), 128-cr/len(img.Cr)
	clamp := func(v float64) uint8 { return uint8(min(max(v, 0), 255)) }
	// Lookup tables instead of float maths per pixel: about 0.6 M samples a frame on the robot's CPU.
	var ly, lcb, lcr [256]uint8
	for v := range 256 {
		ly[v] = clamp(float64(v) * gain)
		lcb[v] = clamp(128 + (float64(v+dcb)-128)*gain)
		lcr[v] = clamp(128 + (float64(v+dcr)-128)*gain)
	}
	for i, v := range img.Y {
		img.Y[i] = ly[v]
	}
	for i := range img.Cb {
		img.Cb[i], img.Cr[i] = lcb[img.Cb[i]], lcr[img.Cr[i]]
	}
}
