// camera-live is the camera module's service. It captures the front camera (/dev/video0) only
// while the panel keeps asking for frames (the handler touches <dir>/want), the module is enabled
// and the robot is not cleaning, and keeps the newest frame in <dir>/live.jpg. The camera LED is
// on while it captures, as the firmware does for its own video monitoring.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"image/jpeg"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

const (
	device   = "/dev/video0"
	led      = "/sys/class/gpio/gpio229/value" // "ai monitor led" in the firmware's set_gpio.sh
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
	stopCam := func(state, reason string) {
		if cam != nil {
			cam.close()
			cam = nil
			_ = os.WriteFile(led, []byte("0"), 0644)
			_ = os.Remove(filepath.Join(dir, "live.jpg"))
		}
		setStatus(state, reason)
	}
	defer stopCam("off", "")
	busyAt, busy := time.Time{}, false
	for ctx.Err() == nil {
		time.Sleep(frameGap)
		st, err := os.Stat(filepath.Join(dir, "want"))
		_, enabled := os.Stat(filepath.Join(os.Getenv("DREAME_MODULE_DATA"), "enabled"))
		if err != nil || time.Since(st.ModTime()) > wantFor || enabled != nil {
			stopCam("off", "")
			continue
		}
		if time.Since(busyAt) > 3*time.Second {
			busyAt, busy = time.Now(), cleaning(ctx)
		}
		if busy {
			// shortcut: not tested whether capturing disturbs the AI obstacle camera, so no live view while cleaning.
			stopCam("busy", "робот убирает: камера занята распознаванием препятствий")
			continue
		}
		if cam == nil {
			if cam, err = openCamera(device, 864, 480); err != nil {
				msg := err.Error()
				if err == errAsleep {
					msg = "робот спит: питание камеры выключено"
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
	}
}

func setStatus(state, reason string) {
	b, _ := json.Marshal(map[string]string{"state": state, "reason": reason})
	_ = os.WriteFile(filepath.Join(dir, "status.json"), b, 0600)
}

// cleaning asks the agent whether the robot is out on a run; unknown counts as not cleaning.
func cleaning(ctx context.Context) bool {
	req, _ := http.NewRequestWithContext(ctx, "GET", os.Getenv("DREAME_API")+"/api/v1/state", nil)
	req.Header.Set("Authorization", "Bearer "+os.Getenv("DREAME_MODULE_TOKEN"))
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer res.Body.Close()
	var snap struct {
		Result struct {
			State struct {
				Activity string `json:"activity"`
			} `json:"state"`
		} `json:"result"`
	}
	_ = json.NewDecoder(res.Body).Decode(&snap)
	a := snap.Result.State.Activity
	return a == "cleaning" || a == "paused" || a == "returning"
}
