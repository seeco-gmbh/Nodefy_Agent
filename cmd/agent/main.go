//go:build windows || darwin || linux

package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"nodefy/agent/internal/config"
	"nodefy/agent/internal/server"
	"nodefy/agent/internal/tray"
	"nodefy/agent/internal/watcher"
	"nodefy/agent/internal/websocket"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

var (
	version = "0.1.0"
	
	// Global references for server message handling
	globalWatcher     *watcher.Watcher
	globalWsClient    *websocket.Client
	globalLocalServer *server.LocalServer
)

func main() {
	// Command line flags
	configPath := flag.String("config", "", "Path to config file")
	configPathShort := flag.String("c", "", "Path to config file (shorthand)")
	
	orchestratorURL := flag.String("orchestrator", "", "Orchestrator WebSocket URL")
	orchestratorURLShort := flag.String("o", "", "Orchestrator WebSocket URL (shorthand)")
	// Keep old flag for backward compatibility
	orchestratorURLOld := flag.String("url", "", "Orchestrator WebSocket URL (deprecated, use -o)")
	
	sessionKey := flag.String("key", "", "Session key")
	sessionKeyShort := flag.String("k", "", "Session key (shorthand)")
	// Keep old flag for backward compatibility
	sessionKeyOld := flag.String("session", "", "Session key (deprecated, use -k)")
	
	debug := flag.Bool("debug", false, "Enable debug logging")
	debugShort := flag.Bool("d", false, "Enable debug logging (shorthand)")
	
	showVersion := flag.Bool("version", false, "Show version")
	showVersionShort := flag.Bool("v", false, "Show version (shorthand)")
	
	initConfig := flag.Bool("init", false, "Create default config file")
	
	// Local mode - run as local server for browser connections
	localMode := flag.Bool("local", false, "Run as local server (for browser connections)")
	localModeShort := flag.Bool("l", false, "Run as local server (shorthand)")
	localPort := flag.String("port", "9081", "Local server port")
	localPortShort := flag.String("p", "", "Local server port (shorthand)")
	
	flag.Parse()
	
	// Resolve local mode flags
	if *localModeShort {
		*localMode = true
	}
	if *localPortShort != "" {
		*localPort = *localPortShort
	}
	
	// Resolve shorthand flags
	if *configPathShort != "" {
		*configPath = *configPathShort
	}
	if *orchestratorURLShort != "" {
		*orchestratorURL = *orchestratorURLShort
	} else if *orchestratorURLOld != "" {
		*orchestratorURL = *orchestratorURLOld
	}
	if *sessionKeyShort != "" {
		*sessionKey = *sessionKeyShort
	} else if *sessionKeyOld != "" {
		*sessionKey = *sessionKeyOld
	}
	if *debugShort {
		*debug = true
	}
	if *showVersionShort {
		*showVersion = true
	}

	// Setup logging
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	if *debug {
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
		log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})
	} else {
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
		log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})
	}

	// Show version
	if *showVersion {
		fmt.Printf("Nodefy Agent v%s\n", version)
		return
	}

	// Initialize config file
	if *initConfig {
		cfg := config.DefaultConfig()
		path := *configPath
		if path == "" {
			path = config.ConfigPath()
		}
		if err := cfg.Save(path); err != nil {
			log.Fatal().Err(err).Msg("Failed to create config file")
		}
		fmt.Printf("Created config file: %s\n", path)
		fmt.Println("Please edit the file and set your session_key and watch_paths")
		return
	}

	// Load configuration
	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to load config")
	}

	// Override config with command line flags
	if *orchestratorURL != "" {
		cfg.OrchestratorURL = *orchestratorURL
	}
	if *sessionKey != "" {
		cfg.SessionKey = *sessionKey
	}

	// Add watch paths from remaining arguments
	watchPaths := flag.Args()
	if len(watchPaths) > 0 {
		cfg.WatchPaths = append(cfg.WatchPaths, watchPaths...)
	}

	// Determine mode: local or orchestrator
	// If no orchestrator URL and no session key, default to local mode
	if *orchestratorURL == "" && *sessionKey == "" && cfg.OrchestratorURL == "" {
		*localMode = true
	}

	if *localMode {
		runLocalMode(cfg, *localPort)
	} else {
		runOrchestratorMode(cfg)
	}
}

func runLocalMode(cfg *config.Config, port string) {
	log.Info().
		Str("version", version).
		Str("port", port).
		Msg("Starting Nodefy Agent in local mode")

	// Create local server
	localServer := server.NewLocalServer(port, handleLocalMessage)
	globalLocalServer = localServer

	// Create file watcher
	fileWatcher, err := watcher.New(
		cfg.FileTypes,
		cfg.Recursive,
		func(event watcher.Event) {
			handleLocalFileEvent(localServer, event)
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

	log.Info().Str("url", "ws://localhost:"+port+"/ws").Msg("Local server ready")

	// Cleanup function
	cleanup := func() {
		log.Info().Msg("Shutting down...")
		localServer.Stop()
		fileWatcher.Stop()
		log.Info().Msg("Agent stopped")
	}

	// System tray (blocking - runs on main thread)
	sysTray := tray.New(localServer, cleanup)
	sysTray.Run()
}

func runOrchestratorMode(cfg *config.Config) {
	// Validate configuration for orchestrator mode
	if err := cfg.Validate(); err != nil {
		log.Fatal().Err(err).Msg("Invalid configuration")
	}

	log.Info().
		Str("version", version).
		Str("orchestrator", cfg.OrchestratorURL).
		Strs("paths", cfg.WatchPaths).
		Msg("Starting Nodefy Agent in orchestrator mode")

	// Create WebSocket client
	wsClient := websocket.NewClient(
		cfg.OrchestratorURL,
		cfg.SessionKey,
		cfg.ReconnectDelay,
		handleServerMessage,
	)

	// Connect to orchestrator
	if err := wsClient.Connect(); err != nil {
		log.Fatal().Err(err).Msg("Failed to connect to Orchestrator")
	}

	// Create file watcher
	fileWatcher, err := watcher.New(
		cfg.FileTypes,
		cfg.Recursive,
		func(event watcher.Event) {
			handleFileEvent(wsClient, event)
		},
	)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to create file watcher")
	}
	
	// Set global references for server message handling
	globalWatcher = fileWatcher
	globalWsClient = wsClient

	// Add watch paths
	for _, path := range cfg.WatchPaths {
		if err := fileWatcher.Watch(path); err != nil {
			log.Error().Err(err).Str("path", path).Msg("Failed to watch path")
		} else {
			wsClient.SendWatchStarted(path)
		}
	}

	// Start the watcher and WebSocket client
	fileWatcher.Start()
	wsClient.Start()

	log.Info().Msg("Agent is running. Press Ctrl+C to stop.")

	// Wait for shutdown signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	log.Info().Msg("Shutting down...")

	fileWatcher.Stop()
	wsClient.Stop()

	log.Info().Msg("Agent stopped")
}

func handleFileEvent(client *websocket.Client, event watcher.Event) {
	log.Debug().
		Str("path", event.Path).
		Str("name", event.Name).
		Str("op", event.Operation).
		Msg("Processing file event")

	switch event.Operation {
	case "create", "modify":
		if err := client.SendFileWithContent(event.Path, event.Name, event.Operation); err != nil {
			log.Error().Err(err).Msg("Failed to send file event")
		}
	case "delete", "rename":
		if err := client.SendFileChanged(event.Path, event.Name, event.Operation); err != nil {
			log.Error().Err(err).Msg("Failed to send file event")
		}
	}
}

func handleServerMessage(msg websocket.Message) {
	log.Debug().Str("type", msg.Type).Msg("Received server message")

	switch msg.Type {
	case websocket.TypeConnected:
		log.Info().Msg("Connected to server")
	case websocket.TypeAck:
		log.Debug().Msg("Server acknowledged message")
	case websocket.TypeError:
		log.Error().Str("error", msg.Error).Msg("Server error")
	case websocket.TypeUploadRequest:
		log.Info().Str("path", msg.Path).Msg("Server requested file upload")
	case websocket.TypeWatch:
		// Add new watch path dynamically
		log.Info().Str("path", msg.Path).Bool("recursive", msg.Recursive).Msg("Adding watch path")
		if globalWatcher != nil {
			if err := globalWatcher.Watch(msg.Path); err != nil {
				log.Error().Err(err).Str("path", msg.Path).Msg("Failed to add watch path")
			} else {
				log.Info().Str("path", msg.Path).Msg("Watch path added successfully")
				if globalWsClient != nil {
					globalWsClient.SendWatchStarted(msg.Path)
				}
			}
		}
	case websocket.TypeUnwatch:
		// Remove watch path dynamically
		log.Info().Str("path", msg.Path).Msg("Removing watch path")
		if globalWatcher != nil {
			if err := globalWatcher.Unwatch(msg.Path); err != nil {
				log.Error().Err(err).Str("path", msg.Path).Msg("Failed to remove watch path")
			} else {
				log.Info().Str("path", msg.Path).Msg("Watch path removed successfully")
			}
		}
	}
}

// Local mode handlers

func handleLocalFileEvent(srv *server.LocalServer, event watcher.Event) {
	log.Debug().
		Str("path", event.Path).
		Str("name", event.Name).
		Str("op", event.Operation).
		Msg("Processing file event (local)")

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

func handleLocalMessage(msg server.Message) {
	log.Debug().Str("type", msg.Type).Msg("Received message from browser")

	switch msg.Type {
	case server.TypeWatch:
		// Add new watch path
		log.Info().Str("path", msg.Path).Bool("recursive", msg.Recursive).Msg("Adding watch path")
		if globalWatcher != nil {
			if err := globalWatcher.Watch(msg.Path); err != nil {
				log.Error().Err(err).Str("path", msg.Path).Msg("Failed to add watch path")
			} else {
				log.Info().Str("path", msg.Path).Msg("Watch path added successfully")
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
				log.Info().Str("path", msg.Path).Msg("Watch path removed successfully")
			}
		}
	}
}
