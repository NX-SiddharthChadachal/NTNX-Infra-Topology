package main

import (
	"fmt"
	"log/slog"
	"os"

	tea "charm.land/bubbletea/v2"
	"github.com/nutanix/ntnx-topo/internal/config"
	"github.com/nutanix/ntnx-topo/internal/ui"
	"github.com/sirupsen/logrus"
)

func main() {
	realStderr := os.Stderr

	logFile, err := os.OpenFile("ntnx-topo.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		fmt.Fprintf(realStderr, "warning: cannot open log file: %v\n", err)
		slog.SetDefault(slog.New(slog.NewTextHandler(realStderr, nil)))
	} else {
		defer logFile.Close()
		slog.SetDefault(slog.New(slog.NewTextHandler(logFile, &slog.HandlerOptions{Level: slog.LevelDebug})))
		logrus.SetOutput(logFile)
		logrus.SetLevel(logrus.WarnLevel)
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(realStderr, "configuration error: %v\n", err)
		os.Exit(1)
	}

	// Redirect after config so startup errors still print. The v4 SDK logs
	// every HTTP call to stderr; that scrolled the TUI after each poll.
	if logFile != nil {
		os.Stderr = logFile
	}

	slog.Info("starting ntnx-topo",
		"pc_ip", cfg.PrismCentralIP,
		"pe_ip", cfg.PrismElementIP,
		"pc_count", len(cfg.PCEndpoints()),
		"needs_wizard", cfg.NeedsWizard(),
		"poll_interval", cfg.PollInterval,
		"tls_verified", !cfg.Insecure,
	)

	if cfg.Insecure {
		slog.Warn("TLS certificate verification is disabled; credentials are sent over an unverified connection. Pass --verify-tls to enable it.")
	}

	m := ui.NewRoot(cfg)
	p := tea.NewProgram(m)

	if _, err := p.Run(); err != nil {
		slog.Error("program error", "error", err)
		fmt.Fprintf(realStderr, "error: %v\n", err)
		os.Exit(1)
	}
}
