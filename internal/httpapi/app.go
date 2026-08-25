package httpapi

import (
	"context"

	"verdant-leaf-fixation-gate/internal/arbiter"
	"verdant-leaf-fixation-gate/internal/catalog"
	"verdant-leaf-fixation-gate/internal/device"
	"verdant-leaf-fixation-gate/internal/ledger"
	"verdant-leaf-fixation-gate/internal/recovery"
	"verdant-leaf-fixation-gate/internal/store"
	"verdant-leaf-fixation-gate/internal/task"
	"verdant-leaf-fixation-gate/internal/withering"
)

// App wires together every business service and the device adapter. It is the
// single composition root used by the HTTP server and by integration tests.
type App struct {
	Store     *store.Store
	Catalog   *catalog.Service
	Ledger    *ledger.Service
	Tasks     *task.TaskService
	Withering *withering.Service
	Arbiter   *arbiter.Service
	Recovery  *recovery.Service
	Device    *device.Adapter
}

// NewApp builds an App over the given store and device adapter.
func NewApp(s *store.Store, dev *device.Adapter) *App {
	cat := catalog.NewService(s)
	led := ledger.NewService(s)
	return &App{
		Store:     s,
		Catalog:   cat,
		Ledger:    led,
		Tasks:     task.NewService(s, cat, led),
		Withering: withering.NewService(s),
		Arbiter:   arbiter.NewService(s),
		Recovery:  recovery.NewService(s),
		Device:    dev,
	}
}

// SeedDemo populates the catalog with the standard demo directory.
func (a *App) SeedDemo(ctx context.Context) error {
	return catalog.SeedDemo(ctx, a.Catalog)
}
