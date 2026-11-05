package ui

import (
	_ "embed"
	"crypto/sha256"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

	winMu      sync.Mutex
	lastWidth  int
	lastHeight int
	lastX      int
	lastY      int
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
		lastWidth:   780,
		lastHeight:  680,
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
				u.showWindow()
			},
		},
		Assets: application.AssetOptions{
			Handler: application.BundledAssetFileServer(u.assets),
		},
		Windows: application.WindowsOptions{
			DisableQuitOnLastWindowClosed: true,
		},
	})
	u.app = app

	policy := u.getMemoryPolicy()
	if startMinimized && policy == "hide" {
		// Launch quietly into system tray without initializing WebView2, saving ~200MB+ RAM!
		u.window = nil
	} else {
		u.createWindow(startMinimized)
	}

	// Setup System Tray
	tray := app.SystemTray.New()
	if len(u.iconBytes) > 0 {
		tray.SetIcon(u.iconBytes)
	}

	trayMenu := app.Menu.New()
	trayMenu.Add("Show Dashboard").OnClick(func(ctx *application.Context) {
		u.showWindow()
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
		u.toggleWindow()
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

func (u *AppUI) getMemoryPolicy() string {
	if u.configMgr == nil {
		return "none"
	}
	p := strings.ToLower(strings.TrimSpace(u.configMgr.Get().MemoryPolicy))
	if p == "hide" || p == "minimize" || p == "tray" {
		return "hide"
	}
	return "none"
}

func (u *AppUI) saveWindowBounds(w *application.WebviewWindow) {
	if w == nil {
		return
	}
	width := w.Width()
	height := w.Height()
	if width > 100 && height > 100 {
		u.lastWidth = width
		u.lastHeight = height
	}
	x, y := w.Position()
	if x > 0 && y > 0 {
		u.lastX = x
		u.lastY = y
	}
}

func (u *AppUI) getDashboardURL() string {
	apiPort := 9999
	if u.apiServer != nil {
		apiPort = u.apiServer.Port()
	}
	return fmt.Sprintf("/?apiPort=%d", apiPort)
}

func (u *AppUI) setupWindowHooks(win *application.WebviewWindow) {
	// Intercept window closing (clicking "X" button)
	win.RegisterHook(events.Common.WindowClosing, func(event *application.WindowEvent) {
		policy := u.getMemoryPolicy()
		if policy == "hide" {
			u.saveWindowBounds(win)
			u.winMu.Lock()
			u.window = nil
			u.winMu.Unlock()
			go func() {
				time.Sleep(200 * time.Millisecond)
				setEfficiencyMode(true)
				trimMemory()
			}()
			return // Allow close without cancelling -> Wails destroys HWND and WebView2 terminates
		}
		event.Cancel()
		setEfficiencyMode(true)
		win.ExecJS("if (window.stopTimers) window.stopTimers();")
		win.Hide()
		go func() {
			time.Sleep(200 * time.Millisecond)
			setEfficiencyMode(true)
			trimMemory()
		}()
	})

	// Hook window minimize event (clicking "—" button)
	win.OnWindowEvent(events.Common.WindowMinimise, func(event *application.WindowEvent) {
		// Pause frontend JS polling timers to avoid continuous background HTTP requests
		win.ExecJS("if (window.stopTimers) window.stopTimers();")

		// Activate Windows Efficiency Mode (EcoQoS / Green Leaf 🍃) on all WebView2 processes
		setEfficiencyMode(true)

		go func() {
			time.Sleep(200 * time.Millisecond)
			setEfficiencyMode(true)
			trimMemory()
		}()
	})

	// Hook window unminimize / restore events (clicking window on taskbar)
	restoreHandler := func(event *application.WindowEvent) {
		// Restore normal CPU priority and clear Efficiency Mode
		setEfficiencyMode(false)

		// Resume frontend JS polling timers
		win.ExecJS("if (window.startTimers) { window.startTimers(); window.updateStatus(); window.updateStats(); window.updateLogs(); }")
	}
	win.OnWindowEvent(events.Common.WindowUnMinimise, restoreHandler)
	win.OnWindowEvent(events.Common.WindowRestore, restoreHandler)
}

func (u *AppUI) createWindow(hidden bool) *application.WebviewWindow {
	apiPort := 9999
	if u.apiServer != nil {
		apiPort = u.apiServer.Port()
	}

	title := u.getStatusTitle()
	u.lastTitle = title

	w := u.lastWidth
	h := u.lastHeight
	if w <= 100 {
		w = 780
	}
	if h <= 100 {
		h = 680
	}

	opts := application.WebviewWindowOptions{
		Title:         title,
		Width:         w,
		Height:        h,
		Hidden:        hidden,
		DisableResize: false,
		URL:           u.getDashboardURL(),
		JS:            fmt.Sprintf("window.GOFOOM_API_PORT = %d; window.GOFOOM_VERSION = %q;", apiPort, version.Version),
	}

	if u.lastX > 0 && u.lastY > 0 {
		opts.InitialPosition = application.WindowXY
		opts.X = u.lastX
		opts.Y = u.lastY
	} else {
		opts.InitialPosition = application.WindowCentered
	}

	win := u.app.Window.NewWithOptions(opts)
	u.window = win

	u.setupWindowHooks(win)

	if !hidden {
		win.Show()
		win.Focus()
	}

	return win
}

func (u *AppUI) showWindow() {
	u.winMu.Lock()
	defer u.winMu.Unlock()

	setEfficiencyMode(false)

	if u.window != nil {
		if u.window.IsMinimised() {
			u.window.UnMinimise()
		}
		u.window.Show()
		u.window.Focus()
		u.window.ExecJS("if (window.startTimers) { window.startTimers(); window.updateStatus(); window.updateStats(); window.updateLogs(); }")
		return
	}

	u.createWindow(false)
}

func (u *AppUI) toggleWindow() {
	u.winMu.Lock()
	defer u.winMu.Unlock()

	if u.window != nil {
		if u.window.IsVisible() && !u.window.IsMinimised() {
			policy := u.getMemoryPolicy()
			if policy == "hide" {
				u.saveWindowBounds(u.window)
				w := u.window
				u.window = nil
				w.Close()
				return
			}
			setEfficiencyMode(true)
			u.window.ExecJS("if (window.stopTimers) window.stopTimers();")
			u.window.Hide()
			go func() {
				time.Sleep(200 * time.Millisecond)
				setEfficiencyMode(true)
				trimMemory()
			}()
			return
		}

		setEfficiencyMode(false)
		if u.window.IsMinimised() {
			u.window.UnMinimise()
		}
		u.window.Show()
		u.window.Focus()
		u.window.ExecJS("if (window.startTimers) { window.startTimers(); window.updateStatus(); window.updateStats(); window.updateLogs(); }")
		return
	}

	setEfficiencyMode(false)
	u.createWindow(false)
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
	u.winMu.Lock()
	w := u.window
	u.winMu.Unlock()
	if w != nil && u.lastTitle != title {
		u.lastTitle = title
		w.SetTitle(title)
	}
}
