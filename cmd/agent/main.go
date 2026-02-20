//go:build windows || darwin || linux

package main

import (
	"flag"
	"fmt"
	"os"

	"nodefy/agent/assets"
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

var version = "0.2.0"

func main() {
	logFile, err := logging.SetupFileLogging()
	if err != nil {
		// Startup warning to stderr — error intentionally ignored (non-fatal).
		_, _ = fmt.Fprintf(os.Stderr, "Warning: Failed to setup file logging: %v\n", err)
	}
	if logFile != nil {
		defer func() {
			if err := logFile.Close(); err != nil {
				// Log file close at shutdown — best effort, stderr fallback.
				_, _ = fmt.Fprintf(os.Stderr, "Warning: Failed to close log file: %v\n", err)
			}
		}()
	}

	defer logging.RecoverWithDialog()

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

	if *showVersion {
		fmt.Printf("Nodefy Agent v%s\n", version)
		return
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to load config")
	}

	if *port != "" {
		cfg.Port = *port
	}
	if *debug {
		cfg.Debug = true
	}

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

	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	if cfg.Debug {
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	} else {
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	}
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})

	runAgent(cfg)
}

func runAgent(cfg *config.Config) {
	dialog.Init()

	log.Info().
		Str("version", version).
		Str("port", cfg.Port).
		Msg("Starting Nodefy Agent")

	var fileWatcher *watcher.Watcher
	var localServer *server.LocalServer

	handleMessage := func(msg server.Message) {
		log.Debug().Str("type", msg.Type).Msg("Received message")

		switch msg.Type {
		case server.TypeWatch:
			log.Info().Str("path", msg.Path).Bool("recursive", msg.Recursive).Msg("Adding watch path")
			if err := fileWatcher.Watch(msg.Path); err != nil {
				log.Error().Err(err).Str("path", msg.Path).Msg("Failed to add watch path")
			} else {
				log.Info().Str("path", msg.Path).Msg("Watch path added")
				if err := localServer.SendWatchStarted(msg.Path); err != nil {
					log.Warn().Err(err).Str("path", msg.Path).Msg("Failed to send watch-started notification")
				}
			}
		case server.TypeUnwatch:
			log.Info().Str("path", msg.Path).Msg("Removing watch path")
			if err := fileWatcher.Unwatch(msg.Path); err != nil {
				log.Error().Err(err).Str("path", msg.Path).Msg("Failed to remove watch path")
			} else {
				log.Info().Str("path", msg.Path).Msg("Watch path removed")
			}
		case server.TypeOpenFileDialog:
			go handleFileDialog(localServer, fileWatcher, msg)
		}
	}

	localServer = server.NewLocalServer(cfg.Port, version, handleMessage)

	bridgeClient := bridge.NewClient()
	eventForwarder := bridge.NewEventForwarder(localServer)
	bridgeClient.SetEventHandler(eventForwarder.HandleBridgeEvent)

	bridgeHandlers := bridge.NewHandlers(bridgeClient)
	localServer.AddRouteRegistrar(bridgeHandlers.RegisterRoutes)
	localServer.AddRouteRegistrar(files.RegisterRoutes)

	var err error
	fileWatcher, err = watcher.New(
		cfg.FileTypes,
		cfg.Recursive,
		func(event watcher.Event) {
			handleFileEvent(localServer, event)
		},
	)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to create file watcher")
	}

	if err := localServer.Start(); err != nil {
		log.Fatal().Err(err).Msg("Failed to start local server")
	}

	fileWatcher.Start()

	log.Info().
		Str("ws", "ws://localhost:"+cfg.Port+"/ws").
		Str("status", "http://localhost:"+cfg.Port+"/status").
		Str("api", "http://localhost:"+cfg.Port+"/api/adapt/*").
		Msg("Agent ready")

	cleanup := func() {
		log.Info().Msg("Shutting down...")
		if err := bridgeClient.Disconnect(); err != nil {
			log.Warn().Err(err).Msg("Error disconnecting bridge client")
		}
		localServer.Stop()
		if err := fileWatcher.Stop(); err != nil {
			log.Warn().Err(err).Msg("Error stopping file watcher")
		}
		log.Info().Msg("Agent stopped")
	}

	tray.SetIcons(assets.IconWhite, assets.IconBlue)
	sysTray := tray.New(localServer, cleanup)
	sysTray.Run()
}

func handleFileEvent(srv *server.LocalServer, event watcher.Event) {
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

func handleFileDialog(localServer *server.LocalServer, fileWatcher *watcher.Watcher, msg server.Message) {
	defer func() {
		if r := recover(); r != nil {
			log.Error().Msgf("Recovered from panic in handleFileDialog: %v", r)
			localServer.Broadcast(server.Message{
				Type:      server.TypeError,
				Error:     fmt.Sprintf("dialog crashed: %v", r),
				RequestID: msg.RequestID,
			})
		}
	}()

	log.Info().Str("title", msg.Title).Strs("filters", msg.Filters).Msg("Opening file dialog")

	title := msg.Title
	if title == "" {
		title = "Select a file"
	}

	fileInfo, err := dialog.OpenFileDialog(title, msg.Filters)
	if err != nil {
		log.Error().Err(err).Msg("File dialog error")
		localServer.Broadcast(server.Message{
			Type:      server.TypeError,
			Error:     err.Error(),
			RequestID: msg.RequestID,
		})
		return
	}

	if fileInfo == nil {
		log.Info().Msg("File dialog cancelled")
		localServer.Broadcast(server.Message{
			Type:      server.TypeDialogCanceled,
			RequestID: msg.RequestID,
		})
		return
	}

	log.Info().Str("path", fileInfo.Path).Str("name", fileInfo.Name).Int64("size", fileInfo.Size).Msg("File selected")

	if err := fileWatcher.Watch(fileInfo.Path); err != nil {
		log.Warn().Err(err).Str("path", fileInfo.Path).Msg("Failed to auto-watch selected file")
	} else {
		log.Info().Str("path", fileInfo.Path).Msg("Auto-watching selected file")
	}

	if err := localServer.SendFileSelected(fileInfo.Path, fileInfo.Name, fileInfo.Size, msg.RequestID); err != nil {
		log.Warn().Err(err).Str("path", fileInfo.Path).Msg("Failed to send file-selected event")
	}
}
