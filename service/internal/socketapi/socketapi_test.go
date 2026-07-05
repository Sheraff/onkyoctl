package socketapi

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"onkyoctl/service/internal/controller"
)

func TestValidateRequest(t *testing.T) {
	valid := []Request{
		{Source: "airplay", Event: "playback-start"},
		{Source: "airplay", Event: "inactive"},
		{Source: "bluetooth", Event: "playback-start"},
		{Source: "bluetooth", Event: "inactive"},
		{Command: "wake"},
		{Command: "off"},
		{Command: "status"},
	}
	for _, req := range valid {
		if err := ValidateRequest(req); err != nil {
			t.Fatalf("ValidateRequest(%#v) returned error: %v", req, err)
		}
	}

	invalid := []Request{
		{},
		{Command: "bad"},
		{Source: "airplay", Event: "connected"},
		{Source: "airplay", Event: "inactive", Command: "status"},
	}
	for _, req := range invalid {
		if err := ValidateRequest(req); err == nil {
			t.Fatalf("ValidateRequest(%#v) succeeded, want error", req)
		}
	}
}

func TestDispatchStatus(t *testing.T) {
	ctl := controller.New(controller.Options{PowerOffDelay: time.Minute})
	defer ctl.Close()
	server := NewServer("/tmp/unused.sock", ctl, nil)

	if resp := server.Dispatch(Request{Source: "airplay", Event: "playback-start"}); !resp.OK {
		t.Fatalf("playback-start response = %#v", resp)
	}
	resp := server.Dispatch(Request{Command: "status"})
	if !resp.OK || resp.Status == nil {
		t.Fatalf("status response = %#v", resp)
	}
	if !resp.Status.AirPlayPlaying {
		t.Fatalf("AirPlayPlaying = false, want true")
	}
}

func TestServeSetsSocketMode(t *testing.T) {
	ctl := controller.New(controller.Options{PowerOffDelay: time.Minute})
	defer ctl.Close()

	ctx, cancel := context.WithCancel(context.Background())
	socketPath := filepath.Join(t.TempDir(), "onkyoctl.sock")
	server := NewServer(socketPath, ctl, nil)
	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Serve(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-errCh:
			if err != nil {
				t.Errorf("Serve returned error: %v", err)
			}
		case <-time.After(time.Second):
			t.Error("Serve did not stop")
		}
	})

	var lastMode os.FileMode
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		info, err := os.Stat(socketPath)
		if err == nil {
			lastMode = info.Mode().Perm()
			if lastMode == socketMode {
				return
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("stat socket: %v", err)
		}

		select {
		case err := <-errCh:
			if err != nil {
				t.Fatalf("Serve returned before socket was ready: %v", err)
			}
			t.Fatal("Serve returned before socket was ready")
		case <-time.After(10 * time.Millisecond):
		}
	}
	t.Fatalf("socket mode = %#o, want %#o", lastMode, socketMode)
}

func TestFormatStatusHuman(t *testing.T) {
	text := FormatStatusHuman(controller.Status{AirPlayPlaying: true, BluetoothConnected: true})
	for _, want := range []string{
		"AirPlay playing: yes",
		"Bluetooth connected: yes",
		"Bluetooth playing: no",
		"Power-off timer pending: no",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("status text %q does not contain %q", text, want)
		}
	}
}
