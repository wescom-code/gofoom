package main

import (
	"flag"
	"fmt"
	"io/fs"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"gofoom/build"
	"gofoom/frontend"
	"gofoom/pkg/api"
	"gofoom/pkg/config"
	"gofoom/pkg/proxy"
	"gofoom/pkg/ui"
)

func main() {
	guiMode := flag.Bool("gui", true, "Launch in GUI & System Tray mode (default: true)")
	headless := flag.Bool("headless", false, "Run in headless daemon mode without GUI")
	minimized := flag.Bool("minimized", false, "Start minimized to system tray (GUI mode)")
	configFile := flag.String("config", "gofoom.json", "Path to gofoom configuration file")
	portFlag := flag.Int("port", 0, "Override local proxy port")
	apiPort := flag.Int("api-port", 9999, "Local management API port (for foomcli compatibility)")
	flag.Parse()

	// If headless flag is explicitly set, disable GUI
	if *headless {
		*guiMode = false
	}

	// Initialize configuration
	cfgMgr := config.NewConfigManager(*configFile)
	appCfg := cfgMgr.Get()

	// Check CLI port override
	if *portFlag > 0 {
		appCfg.LocalPort = *portFlag
		_ = cfgMgr.Update(appCfg)
	}

	serverCfg := cfgMgr.ToServerConfig()
	proxySrv := proxy.NewServer(serverCfg)

	// Setup shutdown handler
	shutdownSignal := make(chan struct{})
	onShutdown := func() {
		close(shutdownSignal)
	}

	// Start local API server (compatible with foomcli)
	effectiveApiPort := *apiPort
	if appCfg.ApiPort > 0 && *apiPort == 9999 {
		effectiveApiPort = appCfg.ApiPort
	}
	apiSrv := api.NewServer(effectiveApiPort, proxySrv, cfgMgr, onShutdown)
	if err := apiSrv.Start(); err != nil {
		log.Printf("Warning: Failed to start API server: %v", err)
	} else {
		log.Printf("Gofoom API server active on http://127.0.0.1:%d", apiSrv.Port())
	}

	// Auto-start proxy if configured
	if appCfg.AutoStart {
		if err := proxySrv.Start(); err != nil {
			log.Printf("Failed to autostart proxy server: %v", err)
		} else {
			log.Printf("Proxy autostarted on 127.0.0.1:%d", serverCfg.LocalPort)
		}
	}

	if *guiMode {
		distFS, err := fs.Sub(frontend.DistFS, "dist")
		if err != nil {
			log.Fatalf("Failed to locate frontend assets: %v", err)
		}

		appUI := ui.NewAppUI(proxySrv, cfgMgr, apiSrv, build.IconBytes, distFS)
		log.Printf("Launching Gofoom GUI (Wails v3)...")
		startMinimized := *minimized || appCfg.StartMinimized
		if err := appUI.Run(startMinimized); err != nil {
			log.Fatalf("GUI error: %v", err)
		}
		return
	}

	// Headless Daemon Mode
	fmt.Println("==================================================")
	fmt.Println("   Gofoom: High-Performance Proxy Facade (Headless)")
	fmt.Println("==================================================")
	fmt.Printf(" Local Proxy Port : %d\n", serverCfg.LocalPort)
	fmt.Printf(" Upstream Type    : %s\n", serverCfg.Upstream.Type)
	fmt.Printf(" Management API   : http://127.0.0.1:%d\n", *apiPort)
	fmt.Println("==================================================")

	// Stream logs to console in headless mode
	logChan, unsub := proxySrv.SubscribeLogs(200)
	defer unsub()

	go func() {
		for ev := range logChan {
			ts := ev.Timestamp.Format("15:04:05.000")
			if ev.Method != "" {
				fmt.Printf("[%s] [%-5s] %s %s -> %d (%s) [%v]\n",
					ts, ev.Level, ev.Method, ev.Target, ev.Status, ev.Message, ev.Duration.Round(time.Millisecond))
			} else {
				fmt.Printf("[%s] [%-5s] %s\n", ts, ev.Level, ev.Message)
			}
		}
	}()

	// Wait for OS interrupt or API shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	select {
	case <-sigChan:
		fmt.Println("\nReceived interrupt signal, stopping Gofoom...")
	case <-shutdownSignal:
		fmt.Println("\nReceived API shutdown request, stopping Gofoom...")
	}

	_ = proxySrv.Stop()
	_ = apiSrv.Stop()
	fmt.Println("Gofoom daemon terminated cleanly.")
}
