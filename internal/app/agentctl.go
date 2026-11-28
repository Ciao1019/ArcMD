package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Ciao1019/ArcMD/internal/agentmcp"
	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/Ciao1019/ArcMD/internal/prefs"
	"github.com/Ciao1019/ArcMD/internal/space"
	"github.com/Ciao1019/ArcMD/internal/trashbin"
)

// newHeadlessApp constructs a lightweight App backed by ConfigDir and
// DocumentsDir without opening windows, scanning plugins, or loading UI caches.
func newHeadlessApp(cfg Config) (*App, error) {
	a := &App{
		cfg:       cfg,
		store:     space.NewStore(filepath.Join(cfg.ConfigDir, "spaces.json")),
		sessions:  make(map[string]*spaceSession),
		openDocs:  make(map[place]*document),
		prefStore: prefs.Store{Path: filepath.Join(cfg.ConfigDir, "prefs.json")},
		bin:       trashbin.Bin{Dir: filepath.Join(cfg.ConfigDir, "trash")},
		qa:        qaState{secrets: prefs.Secrets{Dir: filepath.Join(cfg.ConfigDir, "credentials")}},
		trashFile: trashItemHeadless,
	}
	if p, err := a.prefStore.Load(); err == nil {
		a.prefs = p
	} else {
		a.prefs, a.prefsBroken = &prefs.Prefs{Version: prefs.CurrentVersion}, true
	}
	a.systemLanguage = "zh-Hans"
	a.applyLanguage()
	st, err := a.store.Load()
	if err != nil {
		return nil, err
	}
	a.state = st
	if sp := st.Get(space.ExternalID); sp != nil {
		sp.Name = i18n.T("外部空间")
		sp.Root = filepath.Join(cfg.ConfigDir, "external")
		if err := os.MkdirAll(sp.Root, 0o700); err != nil {
			return nil, err
		}
	}
	if len(st.Spaces) == 0 {
		if err := a.firstRun(); err != nil {
			return nil, err
		}
	}
	a.syncTree()
	return a, nil
}

func (a *App) isLiveWindow() bool {
	return a.win != nil || a.controlExec != nil
}

func decodeParams(raw json.RawMessage, dst any) error {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return nil
	}
	return json.Unmarshal(trimmed, dst)
}

func (a *App) handleControl(req ControlRequest) (any, error) {
	switch strings.TrimSpace(req.Action) {
	case "status":
		return a.ctlStatus()
	case "format_guide":
		var p struct {
			Topic string `json:"topic"`
		}
		if err := decodeParams(req.Params, &p); err != nil {
			return nil, err
		}
		return map[string]any{
			"topic":    p.Topic,
			"markdown": agentmcp.FormatGuide(p.Topic),
		}, nil
	case "space.list":
		return a.ctlSpaceList()
	case "space.create":
		return a.ctlSpaceCreate(req.Params)
	case "space.update":
		return a.ctlSpaceUpdate(req.Params)
	case "space.switch":
		return a.ctlSpaceSwitch(req.Params)
	case "space.remove":
		return a.ctlSpaceRemove(req.Params)
	case "doc.list":
		return a.ctlDocList(req.Params)
	case "doc.search":
		return a.ctlDocSearch(req.Params)
	case "doc.read":
		return a.ctlDocRead(req.Params)
	case "doc.create":
		return a.ctlDocCreate(req.Params)
	case "doc.edit":
		return a.ctlDocEdit(req.Params)
	case "doc.organize":
		return a.ctlDocOrganize(req.Params)
	case "doc.delete":
		return a.ctlDocDelete(req.Params)
	case "whiteboard.write":
		return a.ctlWhiteboardWrite(req.Params)
	case "database.schema":
		return a.ctlDatabaseSchema(req.Params)
	case "database.records":
		return a.ctlDatabaseRecords(req.Params)
	case "clip.ping":
		return a.ctlClipPing()
	case "clip.context":
		return a.ctlClipContext()
	case "clip.check_url":
		return a.ctlClipCheckURL(req.Params)
	case "clip.upload_asset":
		return a.ctlClipUploadAsset(req.Params)
	case "clip.save":
		return a.ctlClipSave(req.Params)
	default:
		return nil, fmt.Errorf("unknown action %q", req.Action)
	}
}

// resolveTargetSpace finds a Space by ID, case-insensitive Name, or Root folder;
// when arg is empty, it returns the active Space.
func (a *App) resolveTargetSpace(arg string) (*space.Space, error) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		if sp := a.state.ActiveSpace(); sp != nil {
			return sp, nil
		}
		return nil, errors.New("no active space")
	}
	for _, sp := range a.state.Spaces {
		if sp.ID == arg || strings.EqualFold(sp.Name, arg) {
			return sp, nil
		}
	}
	if abs, err := filepath.Abs(arg); err == nil {
		cleanAbs := filepath.Clean(abs)
		for _, sp := range a.state.Spaces {
			if !sp.IsExternal() && filepath.Clean(sp.Root) == cleanAbs {
				return sp, nil
			}
		}
	}
	return nil, fmt.Errorf("space %q not found", arg)
}

func (a *App) saveStateStrict() error {
	a.captureSplits()
	return a.store.Save(a.state)
}
