package services

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// desktopPrefs is the small, user-editable desktop configuration persisted next
// to the database. Only the download directory is user-editable today.
type desktopPrefs struct {
	DownloadDir string `json:"download_dir"`
}

// LoadDesktopPrefs reads the persisted preferences (missing/corrupt → zero).
func LoadDesktopPrefs(dataDir string) desktopPrefs {
	if dataDir == "" {
		return desktopPrefs{}
	}
	b, err := os.ReadFile(filepath.Join(dataDir, "desktop.json"))
	if err != nil {
		return desktopPrefs{}
	}
	var p desktopPrefs
	if json.Unmarshal(b, &p) != nil {
		return desktopPrefs{}
	}
	return p
}

func (p *desktopPrefs) save(dataDir string) error {
	if dataDir == "" {
		return nil
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return err
	}
	b, err := json.Marshal(p)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dataDir, "desktop.json"), b, 0o644)
}
