package ui

import (
	_ "embed"
	"crypto/sha256"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gofoom/pkg/api"
	"gofoom/pkg/config"
	"gofoom/pkg/proxy"
	"gofoom/pkg/sysproxy"
	"gofoom/pkg/version"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

func init() {
	// Optimize WebView2 memory: limit V8 heap, disable disk shader cache, minimize background footprint
	existingArgs := os.Getenv("WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS")
	optArgs := "--js-flags=\"--max-old-space-size=64\" --disable-gpu-shader-disk-cache --disable-features=Translate,MediaRouter"
	if existingArgs == "" {
		_ = os.Setenv("WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS", optArgs)
	}
}

// AppUI coordinates the Wails v3 desktop GUI and system tray.
type AppUI struct {
	app         *application.App
	window      *application.WebviewWindow
	proxyServer *proxy.ProxyServer
	configMgr   *config.ConfigManager
	apiServer   *api.Server
	iconBytes   []byte
	assets      fs.FS
	lastTitle   string
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
	exePath, _ := os.Executable()
	dirClean := strings.ToLower(filepath.Clean(filepath.Dir(exePath)))
	h := sha256.Sum256([]byte(dirClean))
	uniqueID := fmt.Sprintf("com.gofoom.proxy.%x", h[:4])

	app := application.New(application.Options{
		Name:        "Gofoom",
		Description: "High-Performance Proxy Facade",
		Icon:        u.iconBytes,
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: uniqueID,
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

	apiPort := 9999
	if u.apiServer != nil {
		apiPort = u.apiServer.Port()
	}

	initialTitle := u.getStatusTitle()
	u.lastTitle = initialTitle

	// Create Webview Window
	win := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:         initialTitle,
		Width:         620,
		Height:        780,
		Hidden:        startMinimized,
		DisableResize: false,
		URL:           fmt.Sprintf("/?apiPort=%d", apiPort),
		JS:            fmt.Sprintf("window.GOFOOM_API_PORT = %d; window.GOFOOM_VERSION = %q;", apiPort, version.Version),
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

	var autoStartItem, startMinItem, startupItem *application.MenuItem

	toggleItem := trayMenu.Add("Start Proxy")
	toggleItem.OnClick(func(ctx *application.Context) {
		if u.proxyServer.IsRunning() {
			_ = u.proxyServer.Stop()
		} else {
			sc := u.configMgr.ToServerConfig()
			u.proxyServer.UpdateConfig(sc)
			_ = u.proxyServer.Start()
		}
		u.updateTrayState(tray, statusItem, toggleItem, autoStartItem, startMinItem, startupItem)
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

	startMinItem = trayMenu.AddCheckbox("Start Minimized to Tray", u.configMgr.Get().StartMinimized)
	startMinItem.OnClick(func(ctx *application.Context) {
		cur := u.configMgr.Get()
		cur.StartMinimized = !cur.StartMinimized
		_ = u.configMgr.Update(cur)
		startMinItem.SetChecked(cur.StartMinimized)
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
	u.updateTrayState(tray, statusItem, toggleItem, autoStartItem, startMinItem, startupItem)

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
			u.updateTrayState(tray, statusItem, toggleItem, autoStartItem, startMinItem, startupItem)
		}
	}()

	return app.Run()
}

func (u *AppUI) getStatusTitle() string {
	running := u.proxyServer.IsRunning()
	status := "STOPPED"
	if running {
		status = "RUNNING"
	}
	cfg := u.configMgr.Get()
	exePath, _ := os.Executable()
	dirName := filepath.Base(filepath.Dir(exePath))
	if dirName == "" || dirName == "." || dirName == "bin" {
		parent := filepath.Base(filepath.Dir(filepath.Dir(exePath)))
		if parent != "" && parent != "." {
			dirName = parent
		}
	}
	return fmt.Sprintf("Gofoom [%s]: %s (:%d -> %s)", dirName, status, cfg.LocalPort, cfg.ProxyType)
}

func (u *AppUI) updateTrayState(tray *application.SystemTray, statusItem, toggleItem, autoStartItem, startMinItem, startupItem *application.MenuItem) {
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
	if startMinItem != nil {
		startMinItem.SetChecked(cfg.StartMinimized)
	}
	if startupItem != nil {
		startupItem.SetChecked(sysproxy.IsWindowsStartupEnabled())
	}

	title := u.getStatusTitle()
	tray.SetTooltip(title)
	if u.window != nil && u.lastTitle != title {
		u.lastTitle = title
		u.window.SetTitle(title)
	}
}
