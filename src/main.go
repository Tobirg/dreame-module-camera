// camera-live is the camera module's service. It captures the front camera (/dev/video0) only
// while the panel keeps asking for frames (the handler touches <dir>/want) and the module is
// enabled, and keeps the newest frame in <dir>/live.jpg. The firmware's AI uses the same sensor
// through /dev/video12; the driver counts users, so either side opening or closing leaves the other
// streaming (checked on r2505 both ways at 15 fps). The camera LED is
// on while it captures, as the firmware does for its own video monitoring, and the fill light too
// when the light flag is set.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"image/jpeg"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

const (
	device   = "/dev/video0"
	led      = "/sys/class/gpio/gpio229/value" // "ai monitor led" in the firmware's set_gpio.sh
	fill     = "/sys/class/pwm/pwmchip0/pwm14"         // the front fill light, as pwm_control_led.sh drives it
	wantFor  = 8 * time.Second                 // the panel asks about every second
	frameGap = 250 * time.Millisecond
)

var dir = envOr("CAMERA_LIVE", "/tmp/dreame-camera")

func envOr(k, v string) string {
	if s := os.Getenv(k); s != "" {
		return s
	}
	return v
}

func main() {
	if len(os.Args) != 2 || os.Args[1] != "run" {
		log.Fatal("run by the Dreame Local agent: camera-live run")
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	_ = os.MkdirAll(dir, 0700)
	run(ctx)
}

func run(ctx context.Context) {
	var cam *camera
	lit := false
	stopCam := func(state, reason string) {
		if lit {
			setLight(false)
			lit = false
		}
		if cam != nil {
			cam.close()
			cam = nil
			_ = os.WriteFile(led, []byte("0"), 0644)
			_ = os.Remove(filepath.Join(dir, "live.jpg"))
		}
		setStatus(state, reason)
	}
	defer stopCam("off", "")
	for ctx.Err() == nil {
		time.Sleep(frameGap)
		st, err := os.Stat(filepath.Join(dir, "want"))
		_, enabled := os.Stat(filepath.Join(os.Getenv("DREAME_MODULE_DATA"), "enabled"))
		if err != nil || time.Since(st.ModTime()) > wantFor || enabled != nil {
			stopCam("off", "")
			continue
		}
		if cam == nil {
			if cam, err = openCamera(device, 864, 480); err != nil {
				msg := err.Error()
				if err == errAsleep {
					msg = "робот спит на базе, питание камеры выключено: кнопка движения сведёт его с базы и разбудит"
				}
				setStatus("error", msg)
				time.Sleep(5 * time.Second)
				continue
			}
			_ = os.WriteFile(led, []byte("1"), 0644)
		}
		img, err := cam.frame(3 * time.Second)
		if err != nil {
			stopCam("error", err.Error())
			continue
		}
		var b bytes.Buffer
		if jpeg.Encode(&b, img, &jpeg.Options{Quality: 70}) == nil {
			tmp := filepath.Join(dir, "live.tmp")
			if os.WriteFile(tmp, b.Bytes(), 0600) == nil {
				_ = os.Rename(tmp, filepath.Join(dir, "live.jpg"))
			}
		}
		setStatus("live", "")
		if _, err := os.Stat(filepath.Join(os.Getenv("DREAME_MODULE_DATA"), "light")); (err == nil) != lit {
			lit = !lit
			setLight(lit)
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
