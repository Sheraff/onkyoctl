package serialri

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestFormatSequenceCanonicalizes(t *testing.T) {
	line, err := FormatSequence(1000, []string{"0xd9", "0X020"})
	if err != nil {
		t.Fatalf("FormatSequence returned error: %v", err)
	}
	if line != "SEQ 1000 0x0D9 0x020\n" {
		t.Fatalf("line = %q", line)
	}
}

func TestFormatSequenceRejectsInvalidInput(t *testing.T) {
	for _, tc := range []struct {
		name  string
		gapMS int
		codes []string
	}{
		{name: "no codes", gapMS: 0, codes: nil},
		{name: "bad code", gapMS: 0, codes: []string{"123"}},
		{name: "zero gap multi", gapMS: 0, codes: []string{"0x0D9", "0x020"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := FormatSequence(tc.gapMS, tc.codes); err == nil {
				t.Fatalf("FormatSequence succeeded, want error")
			}
		})
	}
}

func TestClientSendSequenceWaitsForOK(t *testing.T) {
	fake := newFakePort("READY onkyo-ri seq-v1 safe=0\n", "OK SEQ 0 0x02F\n")
	client := New(Options{
		Device:    "/dev/fake",
		Baud:      115200,
		OpenDelay: 50 * time.Millisecond,
		Opener: func(string, int) (Port, error) {
			return fake, nil
		},
		ResponseTimeout: func(int, int) time.Duration { return 50 * time.Millisecond },
	})

	if err := client.SendSequence(context.Background(), 0, []string{"0x02f"}); err != nil {
		t.Fatalf("SendSequence returned error: %v", err)
	}
	if got := fake.Written(); got != "SEQ 0 0x02F\n" {
		t.Fatalf("written = %q", got)
	}
}

func TestClientReturnsArduinoError(t *testing.T) {
	fake := newFakePort("READY onkyo-ri seq-v1 safe=0\n", "ERR BAD_CODE 0x1234\n")
	client := New(Options{
		Device:    "/dev/fake",
		Baud:      115200,
		OpenDelay: 50 * time.Millisecond,
		Opener: func(string, int) (Port, error) {
			return fake, nil
		},
		ResponseTimeout: func(int, int) time.Duration { return 50 * time.Millisecond },
	})

	err := client.SendSequence(context.Background(), 0, []string{"0x02F"})
	if err == nil || !strings.Contains(err.Error(), "ERR BAD_CODE") {
		t.Fatalf("err = %v, want Arduino error", err)
	}
}

func TestClientTimesOutWaitingForResponse(t *testing.T) {
	fake := newFakePort("READY onkyo-ri seq-v1 safe=0\n")
	client := New(Options{
		Device:    "/dev/fake",
		Baud:      115200,
		OpenDelay: 50 * time.Millisecond,
		Opener: func(string, int) (Port, error) {
			return fake, nil
		},
		ResponseTimeout: func(int, int) time.Duration { return 15 * time.Millisecond },
	})

	err := client.SendSequence(context.Background(), 0, []string{"0x02F"})
	if err == nil || !strings.Contains(err.Error(), ErrTimeout.Error()) {
		t.Fatalf("err = %v, want timeout", err)
	}
}

func TestClientRequiresReadyBeforeSending(t *testing.T) {
	fake := newFakePort()
	client := New(Options{
		Device:    "/dev/fake",
		Baud:      115200,
		OpenDelay: 15 * time.Millisecond,
		Opener: func(string, int) (Port, error) {
			return fake, nil
		},
		ResponseTimeout: func(int, int) time.Duration { return 15 * time.Millisecond },
	})

	err := client.SendSequence(context.Background(), 0, []string{"0x02F"})
	if err == nil || !strings.Contains(err.Error(), ErrReadyTimeout.Error()) {
		t.Fatalf("err = %v, want READY timeout", err)
	}
	if got := fake.Written(); got != "" {
		t.Fatalf("written = %q, want no command before READY", got)
	}
}

type fakePort struct {
	readCh  chan byte
	timeout time.Duration

	mu     sync.Mutex
	writes bytes.Buffer
	closed bool
}

func newFakePort(lines ...string) *fakePort {
	total := 1
	for _, line := range lines {
		total += len(line)
	}
	p := &fakePort{readCh: make(chan byte, total), timeout: time.Millisecond}
	for _, line := range lines {
		for i := 0; i < len(line); i++ {
			p.readCh <- line[i]
		}
	}
	return p
}

func (p *fakePort) Read(b []byte) (int, error) {
	p.mu.Lock()
	timeout := p.timeout
	closed := p.closed
	p.mu.Unlock()
	if closed {
		return 0, context.Canceled
	}
	if timeout <= 0 {
		timeout = time.Millisecond
	}
	select {
	case value := <-p.readCh:
		b[0] = value
		return 1, nil
	case <-time.After(timeout):
		return 0, nil
	}
}

func (p *fakePort) Write(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.writes.Write(b)
}

func (p *fakePort) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	return nil
}

func (p *fakePort) SetReadTimeout(timeout time.Duration) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.timeout = timeout
	return nil
}

func (p *fakePort) Written() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.writes.String()
}
