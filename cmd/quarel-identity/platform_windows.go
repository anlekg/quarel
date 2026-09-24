package main

import (
	"context"
	_ "embed"

	"github.com/anlekg/quarel/internal/adminui"
	"github.com/anlekg/quarel/internal/desktop"
	"github.com/anlekg/quarel/internal/settings"
)

//go:embed quarel.ico
var trayIcon []byte

// defaultAdminAddr: this machine only; the tray menu opens it.
var defaultAdminAddr = "127.0.0.1:8081"

func init() {
	settings.Default = desktop.DataDir("Identite")
}

// run: tray application (no console window); logs in the data folder.
func run() error {
	dir := settings.DataDir()
	logf, err := desktop.LogFile(dir, "quarel-identity.log", 10<<20)
	if err != nil {
		return err
	}
	defer logf.Close()
	_, noPassword := adminui.PendingSetup(dir)
	return desktop.Main(desktop.App{
		ID:       "QuarelIdentity",
		Title:    "Quarel — service d'identité",
		Icon:     trayIcon,
		AdminURL: "http://" + adminAddr(),
		FirstRun: noPassword,
		Status: func() string {
			if ui := currentUI.Load(); ui != nil {
				return ui.StateText()
			}
			return "démarrage…"
		},
		Run: func(ctx context.Context) error { return runService(ctx, logf) },
	})
}
