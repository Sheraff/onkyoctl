package serialri

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.bug.st/serial"
)

const (
	MaxSequenceCodes = 8
	MaxGapMS         = 10000
)

var (
	ErrTimeout      = errors.New("serial response timeout")
	ErrReadyTimeout = errors.New("serial READY timeout")
)

type Port interface {
	io.ReadWriteCloser
	SetReadTimeout(time.Duration) error
}

type Opener func(device string, baud int) (Port, error)

type Options struct {
	Device          string
	Baud            int
	OpenDelay       time.Duration
	Logger          *log.Logger
	Opener          Opener
	ResponseTimeout func(gapMS int, codeCount int) time.Duration
}

type Client struct {
	device          string
	baud            int
	openDelay       time.Duration
	logger          *log.Logger
	opener          Opener
	responseTimeout func(gapMS int, codeCount int) time.Duration

	mu   sync.Mutex
	port Port
}

func New(opts Options) *Client {
	logger := opts.Logger
	if logger == nil {
		logger = log.New(io.Discard, "", 0)
	}
	opener := opts.Opener
	if opener == nil {
		opener = OpenSerialPort
	}
	responseTimeout := opts.ResponseTimeout
	if responseTimeout == nil {
		responseTimeout = DefaultResponseTimeout
	}

	return &Client{
		device:          opts.Device,
		baud:            opts.Baud,
		openDelay:       opts.OpenDelay,
		logger:          logger,
		opener:          opener,
		responseTimeout: responseTimeout,
	}
}

func OpenSerialPort(device string, baud int) (Port, error) {
	p, err := serial.Open(device, &serial.Mode{BaudRate: baud})
	if err != nil {
		return nil, err
	}
	if err := p.SetReadTimeout(100 * time.Millisecond); err != nil {
		_ = p.Close()
		return nil, err
	}
	return p, nil
}

func DefaultResponseTimeout(gapMS int, codeCount int) time.Duration {
	if codeCount < 1 {
		codeCount = 1
	}
	gapCount := codeCount - 1
	if gapCount < 0 {
		gapCount = 0
	}
	timeout := 3*time.Second + time.Duration(gapMS*gapCount)*time.Millisecond
	return timeout
}

func CanonicalCode(code string) (string, error) {
	value, err := ParseCode(code)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("0x%03X", value), nil
}

func ParseCode(code string) (uint16, error) {
	if !strings.HasPrefix(code, "0x") && !strings.HasPrefix(code, "0X") {
		return 0, fmt.Errorf("RI code %q must use 0xNNN hex format", code)
	}
	value, err := strconv.ParseUint(code[2:], 16, 16)
	if err != nil {
		return 0, fmt.Errorf("invalid RI code %q: %w", code, err)
	}
	if value == 0 || value > 0xFFF {
		return 0, fmt.Errorf("RI code %q outside 12-bit range 0x001-0xFFF", code)
	}
	return uint16(value), nil
}

func FormatSequence(gapMS int, codes []string) (string, error) {
	if gapMS < 0 || gapMS > MaxGapMS {
		return "", fmt.Errorf("gap_ms must be between 0 and %d", MaxGapMS)
	}
	if len(codes) == 0 {
		return "", errors.New("at least one RI code is required")
	}
	if len(codes) > MaxSequenceCodes {
		return "", fmt.Errorf("too many RI codes: got %d, max %d", len(codes), MaxSequenceCodes)
	}
	if len(codes) > 1 && gapMS == 0 {
		return "", errors.New("multi-code sequences require a non-zero gap_ms")
	}

	canonical := make([]string, 0, len(codes))
	for _, code := range codes {
		formatted, err := CanonicalCode(code)
		if err != nil {
			return "", err
		}
		canonical = append(canonical, formatted)
	}

	return fmt.Sprintf("SEQ %d %s\n", gapMS, strings.Join(canonical, " ")), nil
}

func (c *Client) SendSequence(ctx context.Context, gapMS int, codes []string) error {
	line, err := FormatSequence(gapMS, codes)
	if err != nil {
		return err
	}
	request := strings.TrimSpace(line)

	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.ensureOpen(ctx); err != nil {
		return err
	}

	c.logger.Printf("serial send: %s", request)
	if _, err := io.WriteString(c.port, line); err != nil {
		c.closeLocked()
		return fmt.Errorf("write serial sequence: %w", err)
	}

	response, err := c.readResponse(ctx, c.responseTimeout(gapMS, len(codes)))
	if err != nil {
		c.closeLocked()
		return err
	}

	if strings.HasPrefix(response, "OK ") {
		echoed := strings.TrimSpace(strings.TrimPrefix(response, "OK "))
		if echoed != request {
			c.logger.Printf("serial OK echo differs: requested %q, got %q", request, echoed)
		} else {
			c.logger.Printf("serial OK: %s", response)
		}
		return nil
	}
	if strings.HasPrefix(response, "ERR ") {
		return fmt.Errorf("arduino rejected sequence: %s", response)
	}
	return fmt.Errorf("unexpected serial response: %s", response)
}

func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closeLocked()
}

func (c *Client) ensureOpen(ctx context.Context) error {
	if c.port != nil {
		return nil
	}
	if c.device == "" {
		return errors.New("serial device is not configured")
	}
	if c.baud <= 0 {
		return errors.New("serial baud must be positive")
	}

	port, err := c.opener(c.device, c.baud)
	if err != nil {
		return fmt.Errorf("open serial device %s: %w", c.device, err)
	}
	c.port = port
	c.logger.Printf("serial opened: %s at %d baud", c.device, c.baud)

	if c.openDelay <= 0 {
		c.closeLocked()
		return errors.New("serial READY wait timeout must be positive")
	}
	if err := c.waitForReady(ctx, c.openDelay); err != nil {
		c.closeLocked()
		return err
	}
	return nil
}

func (c *Client) waitForReady(ctx context.Context, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		line, err := c.readLine(ctx, time.Until(deadline))
		if errors.Is(err, ErrTimeout) {
			return fmt.Errorf("%w after %s", ErrReadyTimeout, timeout)
		}
		if err != nil {
			return err
		}
		if line == "" {
			continue
		}
		c.logger.Printf("serial startup: %s", line)
		if strings.HasPrefix(line, "READY") {
			return nil
		}
	}
	return fmt.Errorf("%w after %s", ErrReadyTimeout, timeout)
}

func (c *Client) readResponse(ctx context.Context, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		line, err := c.readLine(ctx, time.Until(deadline))
		if errors.Is(err, ErrTimeout) {
			return "", fmt.Errorf("%w after %s", ErrTimeout, timeout)
		}
		if err != nil {
			return "", err
		}
		if line == "" || strings.HasPrefix(line, "READY") {
			continue
		}
		if strings.HasPrefix(line, "OK ") || strings.HasPrefix(line, "ERR ") {
			return line, nil
		}
		c.logger.Printf("serial ignored line while waiting for response: %s", line)
	}
	return "", fmt.Errorf("%w after %s", ErrTimeout, timeout)
}

func (c *Client) readLine(ctx context.Context, timeout time.Duration) (string, error) {
	if timeout <= 0 {
		return "", ErrTimeout
	}
	reader := bufio.NewReader(&deadlineReader{ctx: ctx, reader: c.port, timeout: timeout})
	line, err := reader.ReadString('\n')
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return "", err
		}
		if errors.Is(err, ErrTimeout) {
			return "", ErrTimeout
		}
		if errors.Is(err, io.EOF) && line != "" {
			return strings.TrimSpace(line), nil
		}
		return "", err
	}
	return strings.TrimSpace(line), nil
}

func (c *Client) closeLocked() error {
	if c.port == nil {
		return nil
	}
	err := c.port.Close()
	c.port = nil
	return err
}

type deadlineReader struct {
	ctx     context.Context
	reader  io.Reader
	timeout time.Duration
}

func (r *deadlineReader) Read(p []byte) (int, error) {
	deadline := time.Now().Add(r.timeout)
	buf := make([]byte, 1)
	for {
		if err := r.ctx.Err(); err != nil {
			return 0, err
		}
		if time.Now().After(deadline) {
			return 0, ErrTimeout
		}
		n, err := r.reader.Read(buf)
		if n > 0 {
			p[0] = buf[0]
			return 1, nil
		}
		if err != nil {
			return n, err
		}
		time.Sleep(5 * time.Millisecond)
	}
}
