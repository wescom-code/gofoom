package ui

import (
	_ "embed"
	"fmt"
	"io/fs"
	"time"

	"gofoom/pkg/api"
	"gofoom/pkg/config"
	"gofoom/pkg/proxy"
	"gofoom/pkg/sysproxy"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// AppUI coordinates the Wails v3 desktop GUI and system tray.
type AppUI struct {
	app         *application.App
	window      *application.WebviewWindow
	proxyServer *proxy.ProxyServer
	configMgr   *config.ConfigManager
	apiServer   *api.Server
	iconBytes   []byte
	assets      fs.FS
}

// NewAppUI initializes the AppUI manager.
func NewAppUI(
	ps *proxy.ProxyServer,
	cm *config.ConfigManager,
	apiSrv *api.Server,
	icon []byte,
	assets fs.FS,
) *AppUI {
	return &AppUI{
		proxyServer: ps,
		configMgr:   cm,
		apiServer:   apiSrv,
		iconBytes:   icon,
		assets:      assets,
	}
}

// Run starts the Wails v3 event loop.
func (u *AppUI) Run(startMinimized bool) error {
	app := application.New(application.Options{
		Name:        "Gofoom",
		Description: "High-Performance Proxy Facade",
		Icon:        u.iconBytes,
		Assets: application.AssetOptions{
			Handler: application.BundledAssetFileServer(u.assets),
		},
	})
	u.app = app

	// Create Webview Window
	win := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:         "Gofoom",
		Width:         620,
		Height:        780,
		Hidden:        startMinimized,
		DisableResize: false,
	})
	u.window = win

	// Hide instead of quitting when close button is clicked
	win.RegisterHook(events.Common.WindowClosing, func(event *application.WindowEvent) {
		event.Cancel()
		win.Hide()
	})

	// Setup System Tray
	tray := app.SystemTray.New()
	if len(u.iconBytes) > 0 {
		tray.SetIcon(u.iconBytes)
	}
	tray.SetTooltip("Gofoom - Proxy Facade")

	trayMenu := app.Menu.New()
	trayMenu.Add("Show Dashboard").OnClick(func(ctx *application.Context) {
		win.Show()
		win.Focus()
	})

	trayMenu.AddSeparator()

	startItem := trayMenu.Add("Start Proxy")
	startItem.OnClick(func(ctx *application.Context) {
		sc := u.configMgr.ToServerConfig()
		u.proxyServer.UpdateConfig(sc)
		_ = u.proxyServer.Start()
		u.updateTrayState(tray, true)
	})

	stopItem := trayMenu.Add("Stop Proxy")
	stopItem.OnClick(func(ctx *application.Context) {
		_ = u.proxyServer.Stop()
		u.updateTrayState(tray, false)
	})

	trayMenu.Add("Auto-Detect Proxy").OnClick(func(ctx *application.Context) {
		if !u.proxyServer.IsRunning() {
			dp, err := sysproxy.DetectSystemProxy()
			if err == nil && dp != nil {
				cur := u.configMgr.Get()
				cur.ProxyType = dp.Type
				cur.ProxyHost = dp.Host
				cur.ProxyPort = dp.Port
				cur.PacFileLocation = dp.PacURL
				_ = u.configMgr.Update(cur)
			}
		}
	})

	trayMenu.AddSeparator()
	trayMenu.Add("Quit Gofoom").OnClick(func(ctx *application.Context) {
		_ = u.proxyServer.Stop()
		if u.apiServer != nil {
			_ = u.apiServer.Stop()
		}
		app.Quit()
	})

	tray.SetMenu(trayMenu)

	// Left click on tray toggles the window
	tray.OnClick(func() {
		if win.IsVisible() {
			win.Hide()
		} else {
			win.Show()
			win.Focus()
		}
	})

	// Stream proxy logs to Wails event bus
	logChan, unsub := u.proxyServer.SubscribeLogs(200)
	defer unsub()

	go func() {
		for ev := range logChan {
			app.Event.Emit("gofoom:log", ev)
		}
	}()

	// Periodic stats emitter
	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			stats := u.proxyServer.GetStats()
			app.Event.Emit("gofoom:stats", stats)
		}
	}()

	return app.Run()
}

func (u *AppUI) updateTrayState(tray *application.SystemTray, running bool) {
	status := "STOPPED"
	if running {
		status = "RUNNING"
	}
	cfg := u.configMgr.Get()
	tray.SetTooltip(fmt.Sprintf("Gofoom: %s (:%d -> %s)", status, cfg.LocalPort, cfg.ProxyType))
}
