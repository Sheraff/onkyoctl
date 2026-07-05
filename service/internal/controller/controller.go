package controller

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"sync"
	"time"

	"onkyoctl/service/internal/config"
)

var ErrClosed = errors.New("controller is closed")

type SequenceSender interface {
	SendSequence(ctx context.Context, gapMS int, codes []string) error
}

type Options struct {
	Sender SequenceSender
	Logger *log.Logger

	WakeCodes []string
	WakeGapMS int

	PowerOffCodes []string
	PowerOffGapMS int
	PowerOffDelay time.Duration

	WakeOnBluetoothConnect bool
	WakeOnPlaybackStart    bool

	AfterFunc func(time.Duration, func()) Timer
}

type Timer interface {
	Stop() bool
}

type Status struct {
	AirPlayPlaying     bool `json:"airplay_playing"`
	BluetoothConnected bool `json:"bluetooth_connected"`
	BluetoothPlaying   bool `json:"bluetooth_playing"`
	PowerOffPending    bool `json:"power_off_pending"`
}

type Controller struct {
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	sender SequenceSender
	logger *log.Logger

	wakeCodes []string
	wakeGapMS int

	powerOffCodes []string
	powerOffGapMS int
	powerOffDelay time.Duration

	wakeOnBluetoothConnect bool
	wakeOnPlaybackStart    bool

	afterFunc func(time.Duration, func()) Timer
	queue     chan sequenceRequest

	mu                 sync.Mutex
	airPlayPlaying     bool
	bluetoothConnected bool
	bluetoothPlaying   bool
	powerOffPending    bool
	powerOffTimer      Timer
	closed             bool
}

type sequenceRequest struct {
	name  string
	gapMS int
	codes []string
}

func OptionsFromConfig(cfg config.Config, sender SequenceSender, logger *log.Logger) Options {
	return Options{
		Sender: sender,
		Logger: logger,

		WakeCodes: cfg.WakeCodes,
		WakeGapMS: cfg.WakeGapMS,

		PowerOffCodes: cfg.PowerOffCodes,
		PowerOffGapMS: cfg.PowerOffGapMS,
		PowerOffDelay: cfg.PowerOffDelay(),

		WakeOnBluetoothConnect: cfg.WakeOnBluetoothConnect,
		WakeOnPlaybackStart:    cfg.WakeOnPlaybackStart,
	}
}

func New(opts Options) *Controller {
	logger := opts.Logger
	if logger == nil {
		logger = log.New(io.Discard, "", 0)
	}
	sender := opts.Sender
	if sender == nil {
		sender = noopSender{}
	}
	afterFunc := opts.AfterFunc
	if afterFunc == nil {
		afterFunc = func(d time.Duration, f func()) Timer { return time.AfterFunc(d, f) }
	}
	ctx, cancel := context.WithCancel(context.Background())
	c := &Controller{
		ctx:    ctx,
		cancel: cancel,

		sender: sender,
		logger: logger,

		wakeCodes: append([]string(nil), opts.WakeCodes...),
		wakeGapMS: opts.WakeGapMS,

		powerOffCodes: append([]string(nil), opts.PowerOffCodes...),
		powerOffGapMS: opts.PowerOffGapMS,
		powerOffDelay: opts.PowerOffDelay,

		wakeOnBluetoothConnect: opts.WakeOnBluetoothConnect,
		wakeOnPlaybackStart:    opts.WakeOnPlaybackStart,

		afterFunc: afterFunc,
		queue:     make(chan sequenceRequest, 32),
	}
	c.wg.Add(1)
	go c.worker()
	return c
}

func (c *Controller) Close() {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	c.cancelPowerOffTimerLocked()
	c.mu.Unlock()

	c.cancel()
	c.wg.Wait()
}

func (c *Controller) Status() Status {
	c.mu.Lock()
	defer c.mu.Unlock()
	return Status{
		AirPlayPlaying:     c.airPlayPlaying,
		BluetoothConnected: c.bluetoothConnected,
		BluetoothPlaying:   c.bluetoothPlaying,
		PowerOffPending:    c.powerOffPending,
	}
}

func (c *Controller) AirPlayPlaybackStart() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return ErrClosed
	}
	c.airPlayPlaying = true
	c.cancelPowerOffTimerLocked()
	c.logger.Printf("controller: AirPlay playback started")
	if c.wakeOnPlaybackStart {
		return c.enqueueLocked("wake", c.wakeGapMS, c.wakeCodes)
	}
	return nil
}

func (c *Controller) AirPlayInactive() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return ErrClosed
	}
	c.airPlayPlaying = false
	c.logger.Printf("controller: AirPlay inactive")
	c.startPowerOffTimerIfInactiveLocked()
	return nil
}

func (c *Controller) BluetoothConnected() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return ErrClosed
	}
	c.bluetoothConnected = true
	c.cancelPowerOffTimerLocked()
	c.logger.Printf("controller: Bluetooth connected")
	if c.wakeOnBluetoothConnect {
		return c.enqueueLocked("wake", c.wakeGapMS, c.wakeCodes)
	}
	return nil
}

func (c *Controller) BluetoothDisconnected() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return ErrClosed
	}
	c.bluetoothConnected = false
	c.bluetoothPlaying = false
	c.logger.Printf("controller: Bluetooth disconnected")
	c.startPowerOffTimerIfInactiveLocked()
	return nil
}

func (c *Controller) BluetoothPlaybackStart() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return ErrClosed
	}
	wasPlaying := c.bluetoothPlaying
	c.bluetoothConnected = true
	c.bluetoothPlaying = true
	c.cancelPowerOffTimerLocked()
	c.logger.Printf("controller: Bluetooth playback started")
	if c.wakeOnPlaybackStart && !wasPlaying {
		return c.enqueueLocked("wake", c.wakeGapMS, c.wakeCodes)
	}
	return nil
}

func (c *Controller) BluetoothInactive() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return ErrClosed
	}
	c.bluetoothPlaying = false
	c.logger.Printf("controller: Bluetooth playback inactive")
	c.startPowerOffTimerIfInactiveLocked()
	return nil
}

func (c *Controller) Wake() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return ErrClosed
	}
	c.cancelPowerOffTimerLocked()
	c.logger.Printf("controller: manual wake requested")
	return c.enqueueLocked("wake", c.wakeGapMS, c.wakeCodes)
}

func (c *Controller) Off() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return ErrClosed
	}
	c.logger.Printf("controller: manual off requested")
	return c.enqueueLocked("off", c.powerOffGapMS, c.powerOffCodes)
}

func (c *Controller) startPowerOffTimerIfInactiveLocked() {
	if c.airPlayPlaying || c.bluetoothPlaying {
		return
	}
	if c.powerOffTimer != nil {
		c.powerOffTimer.Stop()
		c.powerOffTimer = nil
	}
	c.powerOffPending = true
	delay := c.powerOffDelay
	c.logger.Printf("controller: power-off timer started for %s", delay)
	c.powerOffTimer = c.afterFunc(delay, c.powerOffTimerExpired)
}

func (c *Controller) cancelPowerOffTimerLocked() {
	if c.powerOffTimer != nil {
		c.powerOffTimer.Stop()
		c.powerOffTimer = nil
	}
	if c.powerOffPending {
		c.logger.Printf("controller: power-off timer cancelled")
	}
	c.powerOffPending = false
}

func (c *Controller) powerOffTimerExpired() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.powerOffTimer = nil
	if c.closed {
		return
	}
	if c.airPlayPlaying || c.bluetoothPlaying {
		c.powerOffPending = false
		c.logger.Printf("controller: power-off timer expired but playback is active")
		return
	}
	c.powerOffPending = false
	c.logger.Printf("controller: power-off timer expired; queuing off")
	if err := c.enqueueLocked("off", c.powerOffGapMS, c.powerOffCodes); err != nil {
		c.logger.Printf("controller: queue off failed: %v", err)
	}
}

func (c *Controller) enqueueLocked(name string, gapMS int, codes []string) error {
	if len(codes) == 0 {
		return fmt.Errorf("%s sequence has no RI codes", name)
	}
	req := sequenceRequest{name: name, gapMS: gapMS, codes: append([]string(nil), codes...)}
	select {
	case c.queue <- req:
		c.logger.Printf("controller: queued %s sequence", name)
		return nil
	case <-c.ctx.Done():
		return ErrClosed
	default:
		return fmt.Errorf("sequence queue is full")
	}
}

func (c *Controller) worker() {
	defer c.wg.Done()
	for {
		select {
		case <-c.ctx.Done():
			return
		case req := <-c.queue:
			c.logger.Printf("controller: sending %s sequence", req.name)
			if err := c.sender.SendSequence(c.ctx, req.gapMS, req.codes); err != nil {
				c.logger.Printf("controller: %s sequence failed: %v", req.name, err)
			}
		}
	}
}

type noopSender struct{}

func (noopSender) SendSequence(context.Context, int, []string) error { return nil }
