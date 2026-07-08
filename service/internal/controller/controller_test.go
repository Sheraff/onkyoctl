package controller

import (
	"context"
	"reflect"
	"testing"
	"time"
)

var testWakeCodes = []string{"0x0D9", "0x020"}

const testWakeGapMS = 200

func TestBluetoothConnectSendsWakeAndStartsPowerOffTimerWithoutMarkingPlaybackActive(t *testing.T) {
	sender := newRecordingSender()
	clock := &fakeClock{}
	ctl := newTestController(sender, clock)
	defer ctl.Close()

	if err := ctl.BluetoothConnected(); err != nil {
		t.Fatalf("BluetoothConnected returned error: %v", err)
	}
	seq := sender.wait(t)
	if seq.gapMS != testWakeGapMS || !reflect.DeepEqual(seq.codes, testWakeCodes) {
		t.Fatalf("sequence = %#v, want wake", seq)
	}

	status := ctl.Status()
	if !status.BluetoothConnected {
		t.Fatalf("BluetoothConnected = false, want true")
	}
	if status.BluetoothPlaying {
		t.Fatalf("BluetoothPlaying = true, want false")
	}
	if !status.PowerOffPending {
		t.Fatalf("PowerOffPending = false after Bluetooth connected without playback")
	}

	clock.last(t).Fire()
	seq = sender.wait(t)
	if seq.gapMS != 0 || !reflect.DeepEqual(seq.codes, []string{"0x0DA"}) {
		t.Fatalf("sequence = %#v, want off", seq)
	}
}

func TestRepeatedPlaybackStartsSendRepeatedWakeSequences(t *testing.T) {
	sender := newRecordingSender()
	clock := &fakeClock{}
	ctl := newTestController(sender, clock)
	defer ctl.Close()

	if err := ctl.AirPlayPlaybackStart(); err != nil {
		t.Fatalf("AirPlayPlaybackStart returned error: %v", err)
	}
	if err := ctl.AirPlayPlaybackStart(); err != nil {
		t.Fatalf("second AirPlayPlaybackStart returned error: %v", err)
	}
	sender.wait(t)
	sender.wait(t)
}

func TestPowerOffTimerOnlyStartsWhenAllPlaybackSourcesInactive(t *testing.T) {
	sender := newRecordingSender()
	clock := &fakeClock{}
	ctl := newTestController(sender, clock)
	defer ctl.Close()

	if err := ctl.AirPlayPlaybackStart(); err != nil {
		t.Fatalf("AirPlayPlaybackStart returned error: %v", err)
	}
	sender.wait(t)
	if err := ctl.BluetoothPlaybackStart(); err != nil {
		t.Fatalf("BluetoothPlaybackStart returned error: %v", err)
	}
	sender.wait(t)

	if err := ctl.AirPlayInactive(); err != nil {
		t.Fatalf("AirPlayInactive returned error: %v", err)
	}
	if ctl.Status().PowerOffPending {
		t.Fatalf("PowerOffPending = true while Bluetooth is still playing")
	}

	if err := ctl.BluetoothInactive(); err != nil {
		t.Fatalf("BluetoothInactive returned error: %v", err)
	}
	if !ctl.Status().PowerOffPending {
		t.Fatalf("PowerOffPending = false after all playback inactive")
	}
}

func TestPowerOffTimerIsCancelledWhenPlaybackRestarts(t *testing.T) {
	sender := newRecordingSender()
	clock := &fakeClock{}
	ctl := newTestController(sender, clock)
	defer ctl.Close()

	if err := ctl.AirPlayPlaybackStart(); err != nil {
		t.Fatalf("AirPlayPlaybackStart returned error: %v", err)
	}
	sender.wait(t)
	if err := ctl.AirPlayInactive(); err != nil {
		t.Fatalf("AirPlayInactive returned error: %v", err)
	}
	firstTimer := clock.last(t)

	if err := ctl.AirPlayPlaybackStart(); err != nil {
		t.Fatalf("second AirPlayPlaybackStart returned error: %v", err)
	}
	sender.wait(t)
	if ctl.Status().PowerOffPending {
		t.Fatalf("PowerOffPending = true after playback restart")
	}
	firstTimer.Fire()
	sender.assertNoSequence(t)
}

func TestPowerOffTimerIsCancelledWhenManualWakeRequested(t *testing.T) {
	sender := newRecordingSender()
	clock := &fakeClock{}
	ctl := newTestController(sender, clock)
	defer ctl.Close()

	if err := ctl.AirPlayPlaybackStart(); err != nil {
		t.Fatalf("AirPlayPlaybackStart returned error: %v", err)
	}
	sender.wait(t)
	if err := ctl.AirPlayInactive(); err != nil {
		t.Fatalf("AirPlayInactive returned error: %v", err)
	}
	firstTimer := clock.last(t)

	if err := ctl.Wake(); err != nil {
		t.Fatalf("Wake returned error: %v", err)
	}
	sender.wait(t)
	if ctl.Status().PowerOffPending {
		t.Fatalf("PowerOffPending = true after manual wake")
	}
	firstTimer.Fire()
	sender.assertNoSequence(t)
}

func TestPowerOffTimerIsReplacedWhenBluetoothConnectsWithoutPlayback(t *testing.T) {
	sender := newRecordingSender()
	clock := &fakeClock{}
	ctl := newTestController(sender, clock)
	defer ctl.Close()

	if err := ctl.AirPlayPlaybackStart(); err != nil {
		t.Fatalf("AirPlayPlaybackStart returned error: %v", err)
	}
	sender.wait(t)
	if err := ctl.AirPlayInactive(); err != nil {
		t.Fatalf("AirPlayInactive returned error: %v", err)
	}
	firstTimer := clock.last(t)

	if err := ctl.BluetoothConnected(); err != nil {
		t.Fatalf("BluetoothConnected returned error: %v", err)
	}
	sender.wait(t)
	if !ctl.Status().PowerOffPending {
		t.Fatalf("PowerOffPending = false after Bluetooth connected without playback")
	}
	firstTimer.Fire()
	sender.assertNoSequence(t)

	secondTimer := clock.last(t)
	if secondTimer == firstTimer {
		t.Fatalf("BluetoothConnected reused existing power-off timer")
	}
	secondTimer.Fire()
	seq := sender.wait(t)
	if seq.gapMS != 0 || !reflect.DeepEqual(seq.codes, []string{"0x0DA"}) {
		t.Fatalf("sequence = %#v, want off", seq)
	}
}

func TestBluetoothPlaybackStartDoesNotSendDuplicateWakeWhileAlreadyPlaying(t *testing.T) {
	sender := newRecordingSender()
	clock := &fakeClock{}
	ctl := newTestController(sender, clock)
	defer ctl.Close()

	if err := ctl.BluetoothPlaybackStart(); err != nil {
		t.Fatalf("BluetoothPlaybackStart returned error: %v", err)
	}
	sender.wait(t)
	if err := ctl.BluetoothPlaybackStart(); err != nil {
		t.Fatalf("second BluetoothPlaybackStart returned error: %v", err)
	}
	sender.assertNoSequence(t)
}

func TestPowerOffTimerQueuesOffWhenPlaybackRemainsInactive(t *testing.T) {
	sender := newRecordingSender()
	clock := &fakeClock{}
	ctl := newTestController(sender, clock)
	defer ctl.Close()

	if err := ctl.BluetoothPlaybackStart(); err != nil {
		t.Fatalf("BluetoothPlaybackStart returned error: %v", err)
	}
	sender.wait(t)
	if err := ctl.BluetoothInactive(); err != nil {
		t.Fatalf("BluetoothInactive returned error: %v", err)
	}

	clock.last(t).Fire()
	seq := sender.wait(t)
	if seq.gapMS != 0 || !reflect.DeepEqual(seq.codes, []string{"0x0DA"}) {
		t.Fatalf("sequence = %#v, want off", seq)
	}
	if ctl.Status().PowerOffPending {
		t.Fatalf("PowerOffPending = true after timer fired")
	}
}

func TestQueuedAutoOffIsSkippedWhenPlaybackRestartsBeforeSend(t *testing.T) {
	sender := newBlockingFirstSender()
	clock := &fakeClock{}
	ctl := newTestController(sender, clock)
	defer ctl.Close()

	if err := ctl.BluetoothPlaybackStart(); err != nil {
		t.Fatalf("BluetoothPlaybackStart returned error: %v", err)
	}
	sender.waitFirstStarted(t)

	if err := ctl.BluetoothInactive(); err != nil {
		t.Fatalf("BluetoothInactive returned error: %v", err)
	}
	clock.last(t).Fire()
	if err := ctl.BluetoothPlaybackStart(); err != nil {
		t.Fatalf("second BluetoothPlaybackStart returned error: %v", err)
	}

	sender.releaseFirst()
	seq := sender.wait(t)
	if seq.gapMS != testWakeGapMS || !reflect.DeepEqual(seq.codes, testWakeCodes) {
		t.Fatalf("first sequence = %#v, want initial wake", seq)
	}
	seq = sender.wait(t)
	if seq.gapMS != testWakeGapMS || !reflect.DeepEqual(seq.codes, testWakeCodes) {
		t.Fatalf("second sequence = %#v, want restart wake", seq)
	}
	sender.assertNoSequence(t)
}

func newTestController(sender SequenceSender, clock *fakeClock) *Controller {
	return New(Options{
		Sender: sender,

		WakeCodes: testWakeCodes,
		WakeGapMS: testWakeGapMS,

		PowerOffCodes: []string{"0x0DA"},
		PowerOffGapMS: 0,
		PowerOffDelay: time.Minute,

		WakeOnBluetoothConnect: true,
		WakeOnPlaybackStart:    true,

		AfterFunc: clock.AfterFunc,
	})
}

type sentSequence struct {
	gapMS int
	codes []string
}

type recordingSender struct {
	ch chan sentSequence
}

func newRecordingSender() *recordingSender {
	return &recordingSender{ch: make(chan sentSequence, 16)}
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

func (s *recordingSender) assertNoSequence(t *testing.T) {
	t.Helper()
	select {
	case seq := <-s.ch:
		t.Fatalf("unexpected sequence: %#v", seq)
	case <-time.After(20 * time.Millisecond):
	}
}

type blockingFirstSender struct {
	ch           chan sentSequence
	firstStarted chan struct{}
	release      chan struct{}
	blockFirst   bool
}

func newBlockingFirstSender() *blockingFirstSender {
	return &blockingFirstSender{
		ch:           make(chan sentSequence, 16),
		firstStarted: make(chan struct{}),
		release:      make(chan struct{}),
		blockFirst:   true,
	}
}

func (s *blockingFirstSender) SendSequence(ctx context.Context, gapMS int, codes []string) error {
	if s.blockFirst {
		s.blockFirst = false
		close(s.firstStarted)
		select {
		case <-s.release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	seq := sentSequence{gapMS: gapMS, codes: append([]string(nil), codes...)}
	select {
	case s.ch <- seq:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *blockingFirstSender) waitFirstStarted(t *testing.T) {
	t.Helper()
	select {
	case <-s.firstStarted:
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for first sequence to start")
	}
}

func (s *blockingFirstSender) releaseFirst() {
	close(s.release)
}

func (s *blockingFirstSender) wait(t *testing.T) sentSequence {
	t.Helper()
	select {
	case seq := <-s.ch:
		return seq
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for sequence")
		return sentSequence{}
	}
}

func (s *blockingFirstSender) assertNoSequence(t *testing.T) {
	t.Helper()
	select {
	case seq := <-s.ch:
		t.Fatalf("unexpected sequence: %#v", seq)
	case <-time.After(20 * time.Millisecond):
	}
}

type fakeClock struct {
	timers []*fakeTimer
}

func (c *fakeClock) AfterFunc(_ time.Duration, f func()) Timer {
	timer := &fakeTimer{f: f}
	c.timers = append(c.timers, timer)
	return timer
}

func (c *fakeClock) last(t *testing.T) *fakeTimer {
	t.Helper()
	if len(c.timers) == 0 {
		t.Fatalf("no timers created")
	}
	return c.timers[len(c.timers)-1]
}

type fakeTimer struct {
	stopped bool
	f       func()
}

func (t *fakeTimer) Stop() bool {
	if t.stopped {
		return false
	}
	t.stopped = true
	return true
}

func (t *fakeTimer) Fire() {
	if t.stopped {
		return
	}
	t.stopped = true
	t.f()
}
