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
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: "com.gofoom.proxy.facade",
			OnSecondInstanceLaunch: func(data application.SecondInstanceData) {
				if u.window != nil {
					u.window.Show()
					u.window.Focus()
				}
			},
		},
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

	trayMenu := app.Menu.New()
	trayMenu.Add("Show Dashboard").OnClick(func(ctx *application.Context) {
		win.Show()
		win.Focus()
	})

	trayMenu.AddSeparator()

	// Dynamic status indicator and single toggle action
	statusItem := trayMenu.Add("Status: STOPPED")
	statusItem.SetEnabled(false)

	var autoStartItem, startupItem *application.MenuItem

	toggleItem := trayMenu.Add("Start Proxy")
	toggleItem.OnClick(func(ctx *application.Context) {
		if u.proxyServer.IsRunning() {
			_ = u.proxyServer.Stop()
		} else {
			sc := u.configMgr.ToServerConfig()
			u.proxyServer.UpdateConfig(sc)
			_ = u.proxyServer.Start()
		}
		u.updateTrayState(tray, statusItem, toggleItem, autoStartItem, startupItem)
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

	autoStartItem = trayMenu.AddCheckbox("Auto-start Proxy on Launch", u.configMgr.Get().AutoStart)
	autoStartItem.OnClick(func(ctx *application.Context) {
		cur := u.configMgr.Get()
		cur.AutoStart = !cur.AutoStart
		_ = u.configMgr.Update(cur)
		autoStartItem.SetChecked(cur.AutoStart)
	})

	startupItem = trayMenu.AddCheckbox("Launch at Windows Startup", sysproxy.IsWindowsStartupEnabled())
	startupItem.OnClick(func(ctx *application.Context) {
		cur := sysproxy.IsWindowsStartupEnabled()
		next := !cur
		_ = sysproxy.SetWindowsStartup(next)
		startupItem.SetChecked(next)
		cfg := u.configMgr.Get()
		cfg.LaunchOnStartup = next
		_ = u.configMgr.Update(cfg)
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
	u.updateTrayState(tray, statusItem, toggleItem, autoStartItem, startupItem)

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

	// Periodic stats and tray status emitter
	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			stats := u.proxyServer.GetStats()
			app.Event.Emit("gofoom:stats", stats)
			u.updateTrayState(tray, statusItem, toggleItem, autoStartItem, startupItem)
		}
	}()

	return app.Run()
}

func (u *AppUI) updateTrayState(tray *application.SystemTray, statusItem, toggleItem, autoStartItem, startupItem *application.MenuItem) {
	running := u.proxyServer.IsRunning()
	status := "STOPPED"
	toggleLabel := "Start Proxy"
	if running {
		status = "RUNNING"
		toggleLabel = "Stop Proxy"
	}
	cfg := u.configMgr.Get()
	statusItem.SetLabel(fmt.Sprintf("Status: %s", status))
	toggleItem.SetLabel(toggleLabel)
	if autoStartItem != nil {
		autoStartItem.SetChecked(cfg.AutoStart)
	}
	if startupItem != nil {
		startupItem.SetChecked(sysproxy.IsWindowsStartupEnabled())
	}
	tray.SetTooltip(fmt.Sprintf("Gofoom: %s (:%d -> %s)", status, cfg.LocalPort, cfg.ProxyType))
}
