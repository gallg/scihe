package editor

import (
	"errors"
	"strings"

	"scihe/internal/config"
)

// saveSettings stores the interactively chosen theme and transparency in
// the config file, keeping the rest of the file as written.
func (e *Editor) saveSettings() {
	if configPath == "" {
		return
	}
	err := config.SaveSettings(configPath, map[string]any{
		"theme":       e.cfg.Theme,
		"transparent": e.cfg.Transparent,
	})
	if err != nil {
		e.warnf("config not saved: %v", err)
	}
}

// exConfig opens, saves or shows the config file.
func exConfig(e *Editor, a *exArgs) error {
	switch strings.TrimSpace(a.arg) {
	case "", "edit":
		if configPath == "" {
			e.infof("no config file in use")
			return nil
		}
		return e.runEx("edit " + configPath)
	case "save", "write":
		if configPath == "" {
			e.infof("no config file in use")
			return nil
		}
		e.saveSettings()
		e.infof("theme and transparency saved to %s", configPath)
	case "path":
		e.infof("%s", configPathHint())
	default:
		return errors.New("usage: :config [edit | save | path]")
	}
	return nil
}
