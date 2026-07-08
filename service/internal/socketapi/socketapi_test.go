package socketapi

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
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
		{Command: "volume", Direction: "up", Steps: 1},
		{Command: "volume", Direction: "down", Steps: 20},
	}
	for _, req := range valid {
		if err := ValidateRequest(req); err != nil {
			t.Fatalf("ValidateRequest(%#v) returned error: %v", req, err)
		}
	}

	invalid := []Request{
		{},
		{Command: "bad"},
		{Command: "volume", Direction: "sideways", Steps: 1},
		{Command: "volume", Direction: "up"},
		{Command: "wake", Steps: 1},
		{Source: "airplay", Event: "inactive", Steps: 1},
		{Source: "airplay", Event: "connected"},
		{Source: "airplay", Event: "inactive", Command: "status"},
	}
	for _, req := range invalid {
		if err := ValidateRequest(req); err == nil {
			t.Fatalf("ValidateRequest(%#v) succeeded, want error", req)
		}
	}
}

func TestDispatchVolume(t *testing.T) {
	sender := &recordingSender{ch: make(chan sentSequence, 1)}
	ctl := controller.New(controller.Options{
		Sender: sender,

		VolumeUpCode:    "0x002",
		VolumeDownCode:  "0x003",
		VolumeStepGapMS: 50,
		MaxVolumeSteps:  40,
	})
	defer ctl.Close()
	server := NewServer("/tmp/unused.sock", ctl, nil)

	resp := server.Dispatch(Request{Command: "volume", Direction: "down", Steps: 3})
	if !resp.OK {
		t.Fatalf("volume response = %#v", resp)
	}
	seq := sender.wait(t)
	if seq.gapMS != 50 || !reflect.DeepEqual(seq.codes, []string{"0x003", "0x003", "0x003"}) {
		t.Fatalf("sequence = %#v, want volume down", seq)
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

type sentSequence struct {
	gapMS int
	codes []string
}

type recordingSender struct {
	ch chan sentSequence
}

func (s *recordingSender) SendSequence(ctx context.Context, gapMS int, codes []string) error {
	seq := sentSequence{gapMS: gapMS, codes: append([]string(nil), codes...)}
	select {
	case s.ch <- seq:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *recordingSender) wait(t *testing.T) sentSequence {
	t.Helper()
	select {
	case seq := <-s.ch:
		return seq
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for sequence")
		return sentSequence{}
	}
}
