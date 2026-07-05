package socketapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"onkyoctl/service/internal/controller"
)

type Request struct {
	Source  string `json:"source,omitempty"`
	Event   string `json:"event,omitempty"`
	Command string `json:"command,omitempty"`
}

type Response struct {
	OK     bool               `json:"ok"`
	Error  string             `json:"error,omitempty"`
	Status *controller.Status `json:"status,omitempty"`
}

const socketMode os.FileMode = 0o666

type Server struct {
	path       string
	controller *controller.Controller
	logger     *log.Logger
}

func NewServer(path string, ctl *controller.Controller, logger *log.Logger) *Server {
	if logger == nil {
		logger = log.New(io.Discard, "", 0)
	}
	return &Server{path: path, controller: ctl, logger: logger}
}

func (s *Server) Serve(ctx context.Context) error {
	if err := prepareSocketPath(s.path); err != nil {
		return err
	}
	listener, err := net.Listen("unix", s.path)
	if err != nil {
		return err
	}
	defer listener.Close()
	defer os.Remove(s.path)
	if err := os.Chmod(s.path, socketMode); err != nil {
		return err
	}

	s.logger.Printf("socket listening: %s", s.path)
	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()

	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		go s.handleConn(conn)
	}
}

func (s *Server) handleConn(conn net.Conn) {
	defer conn.Close()
	var req Request
	if err := json.NewDecoder(conn).Decode(&req); err != nil {
		s.writeResponse(conn, Response{OK: false, Error: fmt.Sprintf("decode request: %v", err)})
		return
	}
	resp := s.Dispatch(req)
	s.writeResponse(conn, resp)
}

func (s *Server) Dispatch(req Request) Response {
	if err := ValidateRequest(req); err != nil {
		return Response{OK: false, Error: err.Error()}
	}

	if req.Command != "" {
		s.logger.Printf("socket command: %s", req.Command)
		switch req.Command {
		case "wake":
			return fromError(s.controller.Wake())
		case "off":
			return fromError(s.controller.Off())
		case "status":
			status := s.controller.Status()
			return Response{OK: true, Status: &status}
		default:
			return Response{OK: false, Error: "unknown command"}
		}
	}

	s.logger.Printf("socket event: source=%s event=%s", req.Source, req.Event)
	switch req.Source {
	case "airplay":
		switch req.Event {
		case "playback-start":
			return fromError(s.controller.AirPlayPlaybackStart())
		case "inactive":
			return fromError(s.controller.AirPlayInactive())
		}
	case "bluetooth":
		switch req.Event {
		case "playback-start":
			return fromError(s.controller.BluetoothPlaybackStart())
		case "inactive":
			return fromError(s.controller.BluetoothInactive())
		case "connected":
			return fromError(s.controller.BluetoothConnected())
		case "disconnected":
			return fromError(s.controller.BluetoothDisconnected())
		}
	}
	return Response{OK: false, Error: "unknown source event"}
}

func (s *Server) writeResponse(w io.Writer, resp Response) {
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		s.logger.Printf("socket response write failed: %v", err)
	}
}

func ValidateRequest(req Request) error {
	if req.Command != "" && (req.Source != "" || req.Event != "") {
		return errors.New("request must contain either command or source/event, not both")
	}
	if req.Command != "" {
		switch req.Command {
		case "wake", "off", "status":
			return nil
		default:
			return fmt.Errorf("unknown command %q", req.Command)
		}
	}
	if req.Source == "" || req.Event == "" {
		return errors.New("source and event are required for source requests")
	}
	switch req.Source {
	case "airplay":
		if req.Event == "playback-start" || req.Event == "inactive" {
			return nil
		}
	case "bluetooth":
		if req.Event == "playback-start" || req.Event == "inactive" || req.Event == "connected" || req.Event == "disconnected" {
			return nil
		}
	}
	return fmt.Errorf("unknown source event %q/%q", req.Source, req.Event)
}

func Send(ctx context.Context, socketPath string, req Request) (Response, error) {
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "unix", socketPath)
	if err != nil {
		return Response{}, err
	}
	defer conn.Close()

	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(5 * time.Second)
	}
	_ = conn.SetDeadline(deadline)

	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return Response{}, err
	}

	var resp Response
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return Response{}, err
	}
	if !resp.OK {
		return resp, errors.New(resp.Error)
	}
	return resp, nil
}

func FormatStatusHuman(status controller.Status) string {
	var b strings.Builder
	fmt.Fprintf(&b, "AirPlay playing: %s\n", yesNo(status.AirPlayPlaying))
	fmt.Fprintf(&b, "Bluetooth connected: %s\n", yesNo(status.BluetoothConnected))
	fmt.Fprintf(&b, "Bluetooth playing: %s\n", yesNo(status.BluetoothPlaying))
	fmt.Fprintf(&b, "Power-off timer pending: %s\n", yesNo(status.PowerOffPending))
	return b.String()
}

func fromError(err error) Response {
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	return Response{OK: true}
}

func yesNo(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}

func prepareSocketPath(path string) error {
	if path == "" {
		return errors.New("socket path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("refusing to remove non-socket path %s", path)
	}
	return os.Remove(path)
}
