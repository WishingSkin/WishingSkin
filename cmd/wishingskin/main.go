package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"wishingskin/internal/backup"
	"wishingskin/internal/bsmigration"
	"wishingskin/internal/config"
	"wishingskin/internal/httpapi"
	"wishingskin/internal/store"
	"wishingskin/internal/turnstile"
	"wishingskin/web"
)

func main() {
	if err := run(); err != nil {
		slog.Error("command failed", "reason", err.Error())
		os.Exit(1)
	}
}
func run() error {
	command := "serve"
	if len(os.Args) > 1 {
		command = os.Args[1]
	}
	if command == "preflight" {
		flags := flag.NewFlagSet(command, flag.ContinueOnError)
		path := flags.String("snapshot", "", "offline snapshot JSON")
		source := flags.String("source-textures", "", "source texture directory")
		if err := flags.Parse(os.Args[2:]); err != nil {
			return err
		}
		snap, err := bsmigration.Load(*path)
		if err != nil {
			return err
		}
		report, err := bsmigration.Preflight(snap, *source)
		_ = json.NewEncoder(os.Stdout).Encode(report)
		return err
	}
	if command == "export-bs" {
		return exportBS()
	}
	if command != "serve" && command != "migrate" && command != "import-bs" && command != "create-admin" && command != "backup" && command != "restore" {
		return fmt.Errorf("unknown command: %s (serve, migrate, export-bs, preflight, import-bs, create-admin, backup, restore)", command)
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	openCtx, stop := context.WithTimeout(ctx, 15*time.Second)
	db, err := store.Open(openCtx, cfg.Driver, cfg.DSN)
	stop()
	if err != nil {
		return fmt.Errorf("database connection failed (check driver and DSN)")
	}
	defer db.Close()
	if command == "backup" || command == "restore" {
		flags := flag.NewFlagSet(command, flag.ContinueOnError)
		path := flags.String("directory", "", "backup directory (new for backup, existing for restore)")
		offline := flags.Bool("offline", false, "confirm all application and CLI writes have stopped")
		if err := flags.Parse(os.Args[2:]); err != nil {
			return err
		}
		if *path == "" || !*offline || flags.NArg() != 0 {
			return fmt.Errorf("directory and offline confirmation required; stop all writes first")
		}
		var report backup.Report
		if command == "backup" {
			report, err = backup.Export(ctx, db, cfg.TextureDir, *path, cfg.PluginDir)
		} else {
			report, err = backup.Restore(ctx, db, *path, cfg.TextureDir, cfg.PluginDir)
		}
		if err == nil {
			err = json.NewEncoder(os.Stdout).Encode(report)
		}
		return err
	}
	migrateCtx, stop := context.WithTimeout(ctx, 60*time.Second)
	err = db.Migrate(migrateCtx)
	stop()
	if err != nil {
		return fmt.Errorf("schema migration failed: %w", err)
	}
	if command == "migrate" {
		slog.Info("schema ready")
		return nil
	}
	if command == "create-admin" {
		return createAdmin(ctx, db)
	}
	if command == "import-bs" {
		flags := flag.NewFlagSet(command, flag.ContinueOnError)
		path := flags.String("snapshot", "", "offline snapshot JSON")
		source := flags.String("source-textures", "", "source texture directory")
		if err := flags.Parse(os.Args[2:]); err != nil {
			return err
		}
		snap, err := bsmigration.Load(*path)
		if err != nil {
			return err
		}
		keyPath := os.Getenv("WS_SETTINGS_KEY_FILE")
		if keyPath == "" {
			keyPath = filepath.Join(cfg.TextureDir, ".settings.key")
		}
		settingsKey, err := config.SettingsKey(keyPath)
		if err != nil {
			return fmt.Errorf("settings key unavailable: %w", err)
		}
		report, err := bsmigration.ImportWithSettingsKey(ctx, db, snap, *source, cfg.TextureDir, settingsKey)
		_ = json.NewEncoder(os.Stdout).Encode(report)
		return err
	}
	assets, err := fs.Sub(web.Assets, "dist")
	if err != nil {
		return err
	}
	apiConfig := httpapi.Config{PluginDir: cfg.PluginDir, CommunityPlugins: communityPlugins(), PublicURL: cfg.PublicURL, TextureDir: cfg.TextureDir, Assets: assets, TurnstileSiteKey: cfg.TurnstileSiteKey, TrustedProxies: cfg.TrustedProxies}
	apiConfig.TextureAccountLimitBytes = cfg.TextureAccountLimitBytes
	apiConfig.TextureSiteLimitBytes = cfg.TextureSiteLimitBytes
	keyPath := os.Getenv("WS_SETTINGS_KEY_FILE")
	if keyPath == "" {
		keyPath = filepath.Join(cfg.TextureDir, ".settings.key")
	}
	// A restored encrypted configuration must never silently generate a replacement key.
	var encryptedCount int
	if err := db.DB.QueryRowContext(ctx, "SELECT (SELECT COUNT(*) FROM settings WHERE setting_key IN ('service_config','oidc_config','oidc_providers','object_storage_config','external_connectors','oauth_signing_key')) + (SELECT COUNT(*) FROM webhook_subscriptions) + (SELECT COUNT(*) FROM plugins WHERE settings_json<>'{}' AND plugin_id NOT LIKE 'wishingskin.%')").Scan(&encryptedCount); err != nil {
		return fmt.Errorf("cannot inspect service settings")
	}
	if encryptedCount > 0 {
		if _, err := os.Stat(keyPath); err != nil {
			return fmt.Errorf("service settings key missing; restore the original key file")
		}
	}
	apiConfig.SettingsKey, err = config.SettingsKey(keyPath)
	if err != nil {
		return fmt.Errorf("settings key unavailable: %w", err)
	}
	apiConfig.SMTP = cfg.SMTP
	apiConfig.TurnstileSecret = cfg.TurnstileSecret
	if cfg.SMTP != nil {
		apiConfig.MailSender = cfg.SMTP
	}
	if cfg.TurnstileSecret != "" {
		origin, err := url.Parse(cfg.PublicURL)
		if err != nil {
			return fmt.Errorf("invalid public origin")
		}
		apiConfig.Challenge = &turnstile.Client{Secret: cfg.TurnstileSecret, Hostname: origin.Hostname()}
	}
	app, err := httpapi.New(db, apiConfig)
	if err != nil {
		return err
	}
	workerCtx, stopWorker := context.WithCancel(ctx)
	workerDone := make(chan struct{})
	go func() { defer close(workerDone); app.RunBackground(workerCtx) }()
	defer func() { stopWorker(); <-workerDone; app.WaitBackground() }()
	server := &http.Server{Addr: cfg.Listen, Handler: app, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second}
	stopped := make(chan error, 1)
	go func() { stopped <- server.ListenAndServe() }()
	slog.Info("WishingSkin listening", "address", cfg.Listen)
	select {
	case err := <-stopped:
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, done := context.WithTimeout(context.Background(), 10*time.Second)
		defer done()
		return server.Shutdown(shutdownCtx)
	}
}

func exportBS() error {
	flags := flag.NewFlagSet("export-bs", flag.ContinueOnError)
	info := flags.String("source-info", "", "confirmed source metadata JSON (Source object)")
	output := flags.String("output", "", "new snapshot JSON file")
	prefix := flags.String("table-prefix", "", "source table prefix")
	if err := flags.Parse(os.Args[2:]); err != nil {
		return err
	}
	if *output == "" || *info == "" || os.Getenv("WS_BS_SOURCE_DSN") == "" {
		return fmt.Errorf("source-info, output and WS_BS_SOURCE_DSN are required")
	}
	f, err := os.Open(*info)
	if err != nil {
		return err
	}
	defer f.Close()
	var source bsmigration.Source
	decoder := json.NewDecoder(f)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&source); err != nil {
		return fmt.Errorf("invalid source metadata JSON")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	db, err := store.Open(ctx, "mysql", os.Getenv("WS_BS_SOURCE_DSN"))
	if err != nil {
		return fmt.Errorf("cannot connect to source database (check source DSN)")
	}
	defer db.Close()
	snap, err := bsmigration.ExportMySQL(ctx, db.DB, bsmigration.ExportOptions{Source: source, TablePrefix: *prefix})
	if err != nil {
		return err
	}
	out, err := os.OpenFile(*output, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	err = json.NewEncoder(out).Encode(snap)
	if err == nil {
		err = out.Sync()
	}
	closeErr := out.Close()
	if err != nil {
		return err
	}
	return closeErr
}
