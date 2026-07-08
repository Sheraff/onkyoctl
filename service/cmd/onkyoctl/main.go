package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"onkyoctl/service/internal/bluetooth"
	"onkyoctl/service/internal/config"
	"onkyoctl/service/internal/controller"
	"onkyoctl/service/internal/serialri"
	"onkyoctl/service/internal/socketapi"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout io.Writer, stderr io.Writer) int {
	args, configPath, configExplicit, err := extractConfigFlag(args)
	if err != nil {
		fmt.Fprintln(stderr, err)
		printUsage(stderr)
		return 2
	}
	if len(args) == 0 {
		printUsage(stderr)
		return 2
	}

	switch args[0] {
	case "serve":
		if len(args) != 1 {
			fmt.Fprintln(stderr, "serve does not accept positional arguments")
			printUsage(stderr)
			return 2
		}
		if err := serve(configPath); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		return 0
	case "airplay", "bluetooth", "wake", "off", "status", "volume":
		cfg, err := loadClientConfig(configPath, configExplicit)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		if err := sendClientRequest(args, cfg.SocketPath, stdout); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		return 0
	default:
		fmt.Fprintf(stderr, "unknown command %q\n", args[0])
		printUsage(stderr)
		return 2
	}
}

func serve(configPath string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("load config %s: %w", configPath, err)
	}

	logger := log.New(os.Stderr, "onkyoctl: ", log.LstdFlags|log.Lmicroseconds)
	logger.Printf("loaded config: socket=%s serial=%s baud=%d bluetooth_transport=%t", cfg.SocketPath, cfg.SerialDevice, cfg.SerialBaud, cfg.BluetoothUseTransportState)

	serialClient := serialri.New(serialri.Options{
		Device:    cfg.SerialDevice,
		Baud:      cfg.SerialBaud,
		OpenDelay: cfg.SerialOpenDelay(),
		Logger:    logger,
	})
	defer serialClient.Close()

	ctl := controller.New(controller.OptionsFromConfig(cfg, serialClient, logger))
	defer ctl.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var wg sync.WaitGroup
	if cfg.BluetoothUseTransportState || cfg.WakeOnBluetoothConnect {
		watcher := &bluetooth.Watcher{
			Handler:           ctl,
			UseTransportState: cfg.BluetoothUseTransportState,
			Logger:            logger,
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := watcher.Run(ctx); err != nil && ctx.Err() == nil {
				logger.Printf("bluetooth watcher stopped: %v", err)
			}
		}()
	} else {
		logger.Printf("bluetooth watcher disabled by config")
	}

	server := socketapi.NewServer(cfg.SocketPath, ctl, logger)
	err = server.Serve(ctx)
	stop()
	wg.Wait()
	if err != nil {
		return fmt.Errorf("serve socket: %w", err)
	}
	return nil
}

func sendClientRequest(args []string, socketPath string, stdout io.Writer) error {
	req, wantsStatus, err := requestFromArgs(args)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := socketapi.Send(ctx, socketPath, req)
	if err != nil {
		return err
	}
	if wantsStatus {
		if resp.Status == nil {
			return errors.New("daemon returned no status")
		}
		fmt.Fprint(stdout, socketapi.FormatStatusHuman(*resp.Status))
	}
	return nil
}

func requestFromArgs(args []string) (socketapi.Request, bool, error) {
	switch args[0] {
	case "airplay":
		if len(args) != 2 {
			return socketapi.Request{}, false, errors.New("usage: onkyoctl airplay playback-start|inactive")
		}
		switch args[1] {
		case "playback-start", "inactive":
			return socketapi.Request{Source: "airplay", Event: args[1]}, false, nil
		default:
			return socketapi.Request{}, false, fmt.Errorf("unknown AirPlay event %q", args[1])
		}
	case "bluetooth":
		if len(args) != 2 {
			return socketapi.Request{}, false, errors.New("usage: onkyoctl bluetooth playback-start|inactive")
		}
		switch args[1] {
		case "playback-start", "inactive":
			return socketapi.Request{Source: "bluetooth", Event: args[1]}, false, nil
		default:
			return socketapi.Request{}, false, fmt.Errorf("unknown Bluetooth event %q", args[1])
		}
	case "wake", "off":
		if len(args) != 1 {
			return socketapi.Request{}, false, fmt.Errorf("%s does not accept positional arguments", args[0])
		}
		return socketapi.Request{Command: args[0]}, false, nil
	case "volume":
		return volumeRequestFromArgs(args)
	case "status":
		if len(args) != 1 {
			return socketapi.Request{}, false, errors.New("status does not accept positional arguments")
		}
		return socketapi.Request{Command: "status"}, true, nil
	default:
		return socketapi.Request{}, false, fmt.Errorf("unknown command %q", args[0])
	}
}

func volumeRequestFromArgs(args []string) (socketapi.Request, bool, error) {
	if len(args) < 2 || len(args) > 3 {
		return socketapi.Request{}, false, errors.New("usage: onkyoctl volume up|down [steps] or onkyoctl volume +N|-N")
	}

	if strings.HasPrefix(args[1], "+") || strings.HasPrefix(args[1], "-") {
		if len(args) != 2 {
			return socketapi.Request{}, false, errors.New("usage: onkyoctl volume +N|-N")
		}
		steps, err := strconv.Atoi(args[1])
		if err != nil || steps == 0 {
			return socketapi.Request{}, false, fmt.Errorf("volume delta must be a non-zero signed integer: %s", args[1])
		}
		direction := "up"
		if steps < 0 {
			direction = "down"
			steps = -steps
		}
		return socketapi.Request{Command: "volume", Direction: direction, Steps: steps}, false, nil
	}

	direction := args[1]
	if direction != "up" && direction != "down" {
		return socketapi.Request{}, false, fmt.Errorf("volume direction must be up or down: %s", direction)
	}
	steps := 1
	if len(args) == 3 {
		parsed, err := strconv.Atoi(args[2])
		if err != nil || parsed <= 0 {
			return socketapi.Request{}, false, fmt.Errorf("volume steps must be a positive integer: %s", args[2])
		}
		steps = parsed
	}
	return socketapi.Request{Command: "volume", Direction: direction, Steps: steps}, false, nil
}

func loadClientConfig(path string, explicit bool) (config.Config, error) {
	if explicit || fileExists(path) {
		return config.Load(path)
	}
	cfg := config.Default()
	if err := cfg.Validate(); err != nil {
		return config.Config{}, err
	}
	return cfg, nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func extractConfigFlag(args []string) ([]string, string, bool, error) {
	configPath := config.DefaultConfigPath
	clean := make([]string, 0, len(args))
	configExplicit := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--config" {
			if i+1 >= len(args) {
				return nil, "", false, errors.New("--config requires a path")
			}
			configPath = args[i+1]
			configExplicit = true
			i++
			continue
		}
		if strings.HasPrefix(arg, "--config=") {
			configPath = strings.TrimPrefix(arg, "--config=")
			if configPath == "" {
				return nil, "", false, errors.New("--config requires a path")
			}
			configExplicit = true
			continue
		}
		clean = append(clean, arg)
	}
	if configExplicit && !filepath.IsAbs(configPath) {
		abs, err := filepath.Abs(configPath)
		if err != nil {
			return nil, "", false, err
		}
		configPath = abs
	}
	return clean, configPath, configExplicit, nil
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, "usage:")
	fmt.Fprintln(w, "  onkyoctl serve --config /etc/onkyoctl/config.toml")
	fmt.Fprintln(w, "  onkyoctl airplay playback-start|inactive [--config PATH]")
	fmt.Fprintln(w, "  onkyoctl bluetooth playback-start|inactive [--config PATH]")
	fmt.Fprintln(w, "  onkyoctl wake|off|status [--config PATH]")
	fmt.Fprintln(w, "  onkyoctl volume up|down [steps] [--config PATH]")
	fmt.Fprintln(w, "  onkyoctl volume +N|-N [--config PATH]")
}
