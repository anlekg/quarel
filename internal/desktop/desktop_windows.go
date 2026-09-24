// Package desktop runs a Quarel server as a Windows tray application: an
// icon next to the clock, a menu to open the administration page, start with
// Windows, or quit. The service itself is unchanged.
package desktop

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"fyne.io/systray"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// App describes the tray application of one server.
type App struct {
	ID       string // registry and mutex name, e.g. "QuarelServer"
	Title    string // tooltip and menu title
	Icon     []byte // .ico
	AdminURL string
	// Status returns a short French state for the menu ("En marche"…).
	Status func() string
	// FirstRun: open the administration page at start (no admin password yet).
	FirstRun bool
	// Run runs the service until ctx ends.
	Run func(ctx context.Context) error
}

// Main runs the tray application; it returns when the user quits.
func Main(app App) error {
	if already(app.ID) {
		OpenBrowser(app.AdminURL) // a second launch just shows the page
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	var runErr error
	wg.Add(1)
	go func() {
		defer wg.Done()
		runErr = app.Run(ctx)
		systray.Quit()
	}()
	systray.Run(func() { onReady(ctx, app, cancel) }, func() {})
	cancel()
	wg.Wait()
	return runErr
}

func onReady(ctx context.Context, app App, quit context.CancelFunc) {
	systray.SetIcon(app.Icon)
	systray.SetTitle(app.Title)
	systray.SetTooltip(app.Title)
	open := systray.AddMenuItem("Ouvrir l'administration", "Ouvrir la page d'administration dans le navigateur")
	status := systray.AddMenuItem("État : démarrage…", "")
	status.Disable()
	systray.AddSeparator()
	auto := systray.AddMenuItemCheckbox("Lancer au démarrage de Windows", "", AutostartEnabled(app.ID))
	systray.AddSeparator()
	exit := systray.AddMenuItem("Quitter", "Arrêter le serveur")
	systray.SetOnTapped(func() { OpenBrowser(app.AdminURL) }) // left click

	if app.FirstRun {
		go func() {
			time.Sleep(1500 * time.Millisecond)
			OpenBrowser(app.AdminURL)
		}()
	}
	go func() {
		t := time.NewTicker(2 * time.Second)
		defer t.Stop()
		for {
			if app.Status != nil {
				s := app.Status()
				status.SetTitle("État : " + s)
				systray.SetTooltip(app.Title + " — " + s)
			}
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			case <-open.ClickedCh:
				OpenBrowser(app.AdminURL)
			case <-auto.ClickedCh:
				if auto.Checked() {
					if SetAutostart(app.ID, false) == nil {
						auto.Uncheck()
					}
				} else if SetAutostart(app.ID, true) == nil {
					auto.Check()
				}
			case <-exit.ClickedCh:
				quit()
				return
			}
		}
	}()
}

// already reports whether another instance runs (named mutex, per user session).
func already(id string) bool {
	name, _ := windows.UTF16PtrFromString("Local\\" + id)
	_, err := windows.CreateMutex(nil, false, name)
	return errors.Is(err, windows.ERROR_ALREADY_EXISTS)
}

const runKey = `Software\Microsoft\Windows\CurrentVersion\Run`

// AutostartEnabled reports whether the program starts with the user's session.
func AutostartEnabled(id string) bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	_, _, err = k.GetStringValue(id)
	return err == nil
}

// SetAutostart adds or removes the program from the user's startup programs.
func SetAutostart(id string, on bool) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if !on {
		return k.DeleteValue(id)
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	return k.SetStringValue(id, `"`+exe+`"`)
}

// OpenBrowser opens url in the default browser.
func OpenBrowser(url string) {
	cmd := exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	cmd.Start()
}

// DataDir is the per-user data folder of a server (%LOCALAPPDATA%\Quarel\<name>).
func DataDir(name string) string {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		base, _ = os.UserConfigDir()
	}
	return filepath.Join(base, "Quarel", name)
}

// ExeDir is the folder of the running program (bundled files live next to it).
func ExeDir() string {
	exe, err := os.Executable()
	if err != nil {
		return "."
	}
	return filepath.Dir(exe)
}
