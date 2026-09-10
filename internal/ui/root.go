package ui

import (
	"context"
	"log/slog"

	tea "charm.land/bubbletea/v2"
	"github.com/nutanix/ntnx-topo/internal/config"
	"github.com/nutanix/ntnx-topo/internal/fetcher"
	"github.com/nutanix/ntnx-topo/internal/model"
)

// RootModel is the single Bubbletea program: optional setup, then the dashboard.
// Running setup as a separate tea.NewProgram left the terminal scrolling after
// each API refresh.
type RootModel struct {
	cfg    *config.Config
	setup  *setupModel
	dash   *TUIModel
	ctx    context.Context
	cancel context.CancelFunc
	width  int
	height int
}

func NewRoot(cfg *config.Config) RootModel {
	ctx, cancel := context.WithCancel(context.Background())
	r := RootModel{
		cfg:    cfg,
		ctx:    ctx,
		cancel: cancel,
	}
	if cfg.NeedsWizard() {
		s := newSetupModel()
		r.setup = &s
		return r
	}
	r.startDash()
	return r
}

func (r *RootModel) startDash() {
	updateCh := make(chan model.StateSnapshot, 1)
	f := fetcher.New(r.cfg, updateCh)
	go f.Start(r.ctx)
	m := NewTUIModel(f)
	m.width = r.width
	m.height = r.height
	r.dash = &m
}

func (r RootModel) Init() tea.Cmd {
	if r.setup != nil {
		return r.setup.Init()
	}
	if r.dash != nil {
		return r.dash.Init()
	}
	return nil
}

func (r RootModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if ws, ok := msg.(tea.WindowSizeMsg); ok {
		r.width = ws.Width
		r.height = ws.Height
	}

	if r.setup != nil {
		next, cmd := r.setup.Update(msg)
		sm, ok := next.(setupModel)
		if !ok {
			return r, cmd
		}
		r.setup = &sm
		if sm.cancelled {
			r.cancel()
			return r, tea.Quit
		}
		if sm.done {
			r.cfg.PrismCentrals = sm.endpoints
			r.setup = nil
			slog.Info("setup complete", "pc_count", len(r.cfg.PCEndpoints()))
			r.startDash()
			return r, r.dash.Init()
		}
		return r, cmd
	}

	if r.dash != nil {
		next, cmd := r.dash.Update(msg)
		dm, ok := next.(TUIModel)
		if !ok {
			return r, cmd
		}
		r.dash = &dm
		if dm.quitting {
			r.cancel()
		}
		return r, cmd
	}
	return r, nil
}

func (r RootModel) View() tea.View {
	if r.setup != nil {
		v := r.setup.View()
		v.AltScreen = true
		return v
	}
	if r.dash != nil {
		return r.dash.View()
	}
	v := tea.NewView("")
	v.AltScreen = true
	return v
}
