package adapt

import (
	"context"
	"fmt"

	"github.com/Order52/truesense/internal/api"
)

func (b *guiBackend) registerSystem() {
	h := b.handlers
	h["gui/autostart"] = func(_ context.Context, a args) (any, error) {
		return onOff(a, b.a.Autostart, b.a.SetAutostart, "on", "off", "status")
	}
	h["update/check"] = func(ctx context.Context, a args) (any, error) {
		force, err := a.boolean("force")
		if err != nil {
			return nil, err
		}
		return b.a.CheckUpdate(ctx, force)
	}
	h["update/apply"] = func(ctx context.Context, a args) (any, error) {
		yes, err := a.boolean("yes")
		if err != nil {
			return nil, err
		}
		if !yes { // the page asks the user first; the page is not trusted to be the only guard
			return nil, fmt.Errorf("an update runs an installer as root: it needs yes: true")
		}
		plan, err := b.a.PlanUpdate(ctx, "")
		if err != nil {
			return nil, err
		}
		if !plan.Needed {
			return nil, fmt.Errorf("TrueSense %s is already the newest version", plan.Status.Current)
		}
		return b.a.ApplyUpdate(ctx, plan.Tag)
	}
	h["update/auto"] = func(_ context.Context, a args) (any, error) {
		return onOff(a, b.a.AutoUpdateCheck, b.a.SetAutoUpdateCheck, "on", "off", "status")
	}
	h["doctor/run"] = func(ctx context.Context, _ args) (any, error) { return b.a.RunDoctor(ctx), nil }
	h["service/check"] = func(ctx context.Context, _ args) (any, error) { return b.a.CheckServices(ctx), nil }
	h["service/logs"] = func(ctx context.Context, _ args) (any, error) {
		text, err := b.a.ServiceLogText(ctx)
		return map[string]string{"text": text}, err
	}
	h["service/repair"] = func(ctx context.Context, _ args) (any, error) {
		type row struct {
			Step string `json:"step"`
			OK   bool   `json:"ok"`
		}
		var rows []row
		for _, s := range b.a.RepairUserServices(ctx) {
			rows = append(rows, row{s.What, s.OK})
		}
		return rows, nil
	}
	h["selftest/run"] = func(ctx context.Context, a args) (any, error) {
		skip, err := a.boolean("skip_fans")
		if err != nil {
			return nil, err
		}
		ok, out := b.a.SelfTestText(ctx, skip)
		return map[string]any{"ok": ok, "output": out}, nil
	}
	h["config/show"] = func(context.Context, args) (any, error) { return b.a.ConfigValues() }
	h["config/validate"] = func(context.Context, args) (any, error) { return names(b.a.ValidateConfig()), nil }
	h["config/reset-all"] = func(ctx context.Context, _ args) (any, error) {
		return map[string][]api.ResetStep{"steps": b.a.ResetAll(ctx)}, nil
	}
}
