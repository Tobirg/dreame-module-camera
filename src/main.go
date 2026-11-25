// camera-live is the camera module's service. It captures the front camera (/dev/video0) only
// while a viewer keeps asking for frames (the handler touches <dir>/want), and keeps the newest frame in <dir>/live.jpg. The firmware's AI uses the same sensor
// through /dev/video12; the driver counts users, so either side opening or closing leaves the other
// streaming (checked on r2505 both ways at 15 fps). The camera LED is
// on while it captures, as the firmware does for its own video monitoring, and the fill light too
// when the light flag is set.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"time"
)

const (
	device = "/dev/video0"
	// While the agent drives (its hold file is fresh) the firmware's video monitoring needs
	// /dev/video0, so the view moves to video12, another vin node on the same sensor.
	holdDevice = "/dev/video12"
	holdFor    = 20 * time.Second                // the agent refreshes it with each keep_alive (8 s)
	led        = "/sys/class/gpio/gpio229/value" // "ai monitor led" in the firmware's set_gpio.sh
	fill       = "/sys/class/pwm/pwmchip0/pwm14" // the front fill light, as pwm_control_led.sh drives it
	wantFor    = 8 * time.Second                 // viewers ask at least every second
	frameGap   = 20 * time.Millisecond           // the sensor gives 15 fps; frame() waits for the next one
)

var dir = envOr("CAMERA_LIVE", "/tmp/dreame-camera")

func envOr(k, v string) string {
	if s := os.Getenv(k); s != "" {
		return s
	}
	return v
}

func main() {
	if len(os.Args) == 3 && os.Args[1] == "probe" {
		probe(os.Args[2])
		return
	}
	if len(os.Args) != 2 || os.Args[1] != "run" {
		log.Fatal("run by the Dreame Local agent: camera-live run")
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	_ = os.MkdirAll(dir, 0700)
	run(ctx)
}

func run(ctx context.Context) {
	// Colour balance and JPEG run beside the capture on two cores, so the view keeps up with the
	// sensor's 15 fps (one core managed about 8).
	frames := make(chan image.Image, 1)
	var seq, shown atomic.Int64
	for range 2 {
		go func() {
			for img := range frames {
				n := seq.Add(1)
				yc := img.(*image.YCbCr)
				balance(yc)
				var b bytes.Buffer
				if jpeg.Encode(&b, yc, &jpeg.Options{Quality: 70}) != nil {
					continue
				}
				if n < shown.Load() {
					continue // the other worker already wrote a newer frame
				}
				shown.Store(n)
				tmp := filepath.Join(dir, fmt.Sprintf("live.%d.tmp", n%2))
				if os.WriteFile(tmp, b.Bytes(), 0600) == nil {
					_ = os.Rename(tmp, filepath.Join(dir, "live.jpg"))
				}
			}
		}()
	}
	defer close(frames)
	var cam *camera
	camDev := ""
	lit := false
	// What the firmware had lit before the view (e.g. its own light for the AI while cleaning in
	// the dark): closing the view puts these back instead of switching them off.
	ledWas, fillWas := "0", false
	stopCam := func(state, reason string) {
		if lit {
			if !fillWas {
				setLight(false)
			}
			lit = false
		}
		if cam != nil {
			cam.close()
			cam = nil
			camDev = ""
			_ = os.WriteFile(led, []byte(ledWas), 0644)
			_ = os.Remove(filepath.Join(dir, "live.jpg"))
		}
		setStatus(state, reason)
	}
	defer stopCam("off", "")
	for ctx.Err() == nil {
		time.Sleep(frameGap)
		st, err := os.Stat(filepath.Join(dir, "want"))
		if err != nil || time.Since(st.ModTime()) > wantFor {
			stopCam("off", "")
			continue
		}
		dev := device
		if st, err := os.Stat(filepath.Join(dir, "hold")); err == nil && time.Since(st.ModTime()) < holdFor {
			dev = holdDevice
		}
		if cam != nil && camDev != dev {
			cam.close() // the agent's hold came or went: hand video0 over before the firmware asks for it
			cam = nil
			_ = os.WriteFile(led, []byte(ledWas), 0644)
			setStatus("switching", "")
		}
		if cam == nil {
			if cam, err = openCamera(dev, 864, 480); err != nil {
				msg := err.Error()
				if err == errAsleep {
					msg = "робот спит на базе, питание камеры выключено: кнопка движения сведёт его с базы и разбудит"
				}
				setStatus("error", msg)
				time.Sleep(5 * time.Second)
				continue
			}
			camDev = dev
			ledWas = "0"
			if b, _ := os.ReadFile(led); len(b) > 0 && b[0] == '1' {
				ledWas = "1"
			}
			_ = os.WriteFile(led, []byte("1"), 0644)
		}
		img, err := cam.frame(3 * time.Second)
		if err != nil {
			stopCam("error", err.Error())
			continue
		}
		select { // a busy encoder drops this frame: the picture stays fresh rather than late
		case frames <- img:
		default:
		}
		setStatusDev("live", camDev)
		if _, err := os.Stat(filepath.Join(os.Getenv("DREAME_MODULE_DATA"), "light")); (err == nil) != lit {
			lit = !lit
			if lit {
				b, _ := os.ReadFile(fill + "/enable")
				fillWas = len(b) > 0 && b[0] == '1'
			} else {
				fillWas = false // the viewer switched it off on purpose
			}
			setLight(lit)
		} else if lit {
			// The firmware runs pwm_control_led.sh off on its own (e.g. after a manual-drive step): relight.
			if b, _ := os.ReadFile(fill + "/enable"); len(b) > 0 && b[0] == '0' {
				setLight(true)
			}
		}
	}
}

// setLight switches the fill light at the brightness the firmware's video monitoring uses (0.6).
// shortcut: off ends any light the firmware lit itself meanwhile; it relights it when it needs it.
func setLight(on bool) {
	if !on {
		_ = os.WriteFile(fill+"/enable", []byte("0"), 0644)
		return
	}
	_ = os.WriteFile(fill+"/period", []byte("20833"), 0644)
	_ = os.WriteFile(fill+"/duty_cycle", []byte("8333"), 0644) // inverted output: 40 % duty is 60 % light
	_ = os.WriteFile(fill+"/enable", []byte("1"), 0644)
}

func setStatus(state, reason string) {
	b, _ := json.Marshal(map[string]string{"state": state, "reason": reason})
	_ = os.WriteFile(filepath.Join(dir, "status.json"), b, 0600)
}

// setStatusDev reports which device the view holds: the agent waits until it is not /dev/video0.
func setStatusDev(state, dev string) {
	b, _ := json.Marshal(map[string]string{"state": state, "reason": "", "device": dev})
	_ = os.WriteFile(filepath.Join(dir, "status.json"), b, 0600)
}

// probe <device>: a diagnostic run by hand over ADB. Opens the device as the service does, takes
// 45 frames and prints the size and rate, and saves the last one to /tmp/camera-probe.jpg.
func probe(dev string) {
	cam, err := openCamera(dev, 864, 480)
	if err != nil {
		fmt.Println("open:", err)
		os.Exit(1)
	}
	defer cam.close()
	start := time.Now()
	var img image.Image
	for i := 0; i < 45; i++ {
		if img, err = cam.frame(3 * time.Second); err != nil {
			fmt.Println("frame", i, err)
			os.Exit(1)
		}
	}
	d := time.Since(start)
	fmt.Printf("%s %dx%d %.1f fps\n", dev, cam.w, cam.h, 45/d.Seconds())
	yc := img.(*image.YCbCr)
	balance(yc)
	var b bytes.Buffer
	_ = jpeg.Encode(&b, yc, &jpeg.Options{Quality: 70})
	_ = os.WriteFile("/tmp/camera-probe.jpg", b.Bytes(), 0600)
}
