//go:build windows || darwin || linux

package main

import (
	"flag"
	"fmt"
	"os"

	"nodefy/agent/internal/bridge"
	"nodefy/agent/internal/config"
	"nodefy/agent/internal/dialog"
	"nodefy/agent/internal/files"
	"nodefy/agent/internal/logging"
	"nodefy/agent/internal/server"
	"nodefy/agent/internal/tray"
	"nodefy/agent/internal/watcher"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

var (
	version = "0.2.0"

	// Global references for message handling
	globalWatcher      *watcher.Watcher
	globalLocalServer  *server.LocalServer
	globalBridgeClient *bridge.Client
)

func main() {
	// Setup file logging first (before anything else)
	logFile, err := logging.SetupFileLogging()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: Failed to setup file logging: %v\n", err)
	}
	if logFile != nil {
		defer logFile.Close()
	}

	// Panic recovery with error dialog (especially useful on Windows)
	defer logging.RecoverWithDialog()

	// Command line flags
	port := flag.String("port", "", "WebSocket server port (default: 9081)")
	portShort := flag.String("p", "", "WebSocket server port (shorthand)")

	debug := flag.Bool("debug", false, "Enable debug logging")
	debugShort := flag.Bool("d", false, "Enable debug logging (shorthand)")

	showVersion := flag.Bool("version", false, "Show version")
	showVersionShort := flag.Bool("v", false, "Show version (shorthand)")

	configPath := flag.String("config", "", "Path to config file")
	configPathShort := flag.String("c", "", "Path to config file (shorthand)")

	initConfig := flag.Bool("init", false, "Create default config file")

	flag.Parse()

	// Resolve shorthand flags
	if *portShort != "" {
		*port = *portShort
	}
	if *debugShort {
		*debug = true
	}
	if *showVersionShort {
		*showVersion = true
	}
	if *configPathShort != "" {
		*configPath = *configPathShort
	}

	// Show version
	if *showVersion {
		fmt.Printf("Nodefy Agent v%s\n", version)
		return
	}

	// Load configuration
	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to load config")
	}

	// Override config with command line flags
	if *port != "" {
		cfg.Port = *port
	}
	if *debug {
		cfg.Debug = true
	}

	// Initialize config file
	if *initConfig {
		path := *configPath
		if path == "" {
			path = config.ConfigPath()
		}
		if err := cfg.Save(path); err != nil {
			log.Fatal().Err(err).Msg("Failed to create config file")
		}
		fmt.Printf("Created config file: %s\n", path)
		return
	}

	// Setup console logging based on debug flag
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	if cfg.Debug {
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
		log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})
	} else {
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
		log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})
	}

	// Run the agent
	runAgent(cfg)
}

func runAgent(cfg *config.Config) {
	log.Info().
		Str("version", version).
		Str("port", cfg.Port).
		Msg("Starting Nodefy Agent")

	// Create local WebSocket server
	localServer := server.NewLocalServer(cfg.Port, handleMessage)
	globalLocalServer = localServer

	// Create Adapt Bridge client and event forwarder
	bridgeClient := bridge.NewClient()
	globalBridgeClient = bridgeClient
	eventForwarder := bridge.NewEventForwarder(localServer)
	bridgeClient.SetEventHandler(eventForwarder.HandleBridgeEvent)

	// Register bridge REST endpoints
	bridgeHandlers := bridge.NewHandlers(bridgeClient)
	localServer.AddRouteRegistrar(bridgeHandlers.RegisterRoutes)

	// Register file export/import endpoints
	localServer.AddRouteRegistrar(files.RegisterRoutes)

	// Create file watcher
	fileWatcher, err := watcher.New(
		cfg.FileTypes,
		cfg.Recursive,
		func(event watcher.Event) {
			handleFileEvent(localServer, event)
		},
	)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to create file watcher")
	}
	globalWatcher = fileWatcher

	// Start local server
	if err := localServer.Start(); err != nil {
		log.Fatal().Err(err).Msg("Failed to start local server")
	}

	// Start file watcher
	fileWatcher.Start()

	log.Info().
		Str("ws", "ws://localhost:"+cfg.Port+"/ws").
		Str("status", "http://localhost:"+cfg.Port+"/status").
		Str("api", "http://localhost:"+cfg.Port+"/api/adapt/*").
		Msg("Agent ready")

	// Cleanup function
	cleanup := func() {
		log.Info().Msg("Shutting down...")
		bridgeClient.Disconnect()
		localServer.Stop()
		fileWatcher.Stop()
		log.Info().Msg("Agent stopped")
	}

	// System tray (blocking - runs on main thread)
	sysTray := tray.New(localServer, cleanup)
	sysTray.Run()
}

func handleFileEvent(srv *server.LocalServer, event watcher.Event) {
	log.Debug().
		Str("path", event.Path).
		Str("name", event.Name).
		Str("op", event.Operation).
		Msg("File event")

	switch event.Operation {
	case "create", "modify":
		if err := srv.SendFileWithContent(event.Path, event.Name, event.Operation); err != nil {
			log.Error().Err(err).Msg("Failed to send file event")
		}
	case "delete", "rename":
		if err := srv.SendFileChanged(event.Path, event.Name, event.Operation); err != nil {
			log.Error().Err(err).Msg("Failed to send file event")
		}
	}
}

func handleMessage(msg server.Message) {
	log.Debug().Str("type", msg.Type).Msg("Received message")

	switch msg.Type {
	case server.TypeWatch:
		// Add new watch path
		log.Info().Str("path", msg.Path).Bool("recursive", msg.Recursive).Msg("Adding watch path")
		if globalWatcher != nil {
			if err := globalWatcher.Watch(msg.Path); err != nil {
				log.Error().Err(err).Str("path", msg.Path).Msg("Failed to add watch path")
			} else {
				log.Info().Str("path", msg.Path).Msg("Watch path added")
				if globalLocalServer != nil {
					globalLocalServer.SendWatchStarted(msg.Path)
				}
			}
		}
	case server.TypeUnwatch:
		// Remove watch path
		log.Info().Str("path", msg.Path).Msg("Removing watch path")
		if globalWatcher != nil {
			if err := globalWatcher.Unwatch(msg.Path); err != nil {
				log.Error().Err(err).Str("path", msg.Path).Msg("Failed to remove watch path")
			} else {
				log.Info().Str("path", msg.Path).Msg("Watch path removed")
			}
		}
	case server.TypeOpenFileDialog:
		// Open native file dialog
		go handleFileDialog(msg)
	}
}

func handleFileDialog(msg server.Message) {
	log.Info().Str("title", msg.Title).Strs("filters", msg.Filters).Msg("Opening file dialog")

	title := msg.Title
	if title == "" {
		title = "Select a file"
	}

	fileInfo, err := dialog.OpenFileDialog(title, msg.Filters)
	if err != nil {
		log.Error().Err(err).Msg("File dialog error")
		if globalLocalServer != nil {
			globalLocalServer.Broadcast(server.Message{
				Type:      server.TypeError,
				Error:     err.Error(),
				RequestID: msg.RequestID,
			})
		}
		return
	}

	if fileInfo == nil {
		// User cancelled
		log.Info().Msg("File dialog cancelled")
		if globalLocalServer != nil {
			globalLocalServer.Broadcast(server.Message{
				Type:      server.TypeDialogCanceled,
				RequestID: msg.RequestID,
			})
		}
		return
	}

	log.Info().Str("path", fileInfo.Path).Str("name", fileInfo.Name).Int64("size", fileInfo.Size).Msg("File selected")

	// Auto-start watching the file
	if globalWatcher != nil {
		if err := globalWatcher.Watch(fileInfo.Path); err != nil {
			log.Warn().Err(err).Str("path", fileInfo.Path).Msg("Failed to auto-watch selected file")
		} else {
			log.Info().Str("path", fileInfo.Path).Msg("Auto-watching selected file")
		}
	}

	// Send file_selected with content
	if globalLocalServer != nil {
		globalLocalServer.SendFileSelected(fileInfo.Path, fileInfo.Name, fileInfo.Size, msg.RequestID)
	}
}
