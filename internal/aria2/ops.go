package aria2

import (
	"context"
	"fmt"
	"sort"
)

// AddURI enqueues a download and returns its GID.
func (c *Client) AddURI(ctx context.Context, uris []string, opts AddURIOptions) (string, error) {
	if len(uris) == 0 {
		return "", fmt.Errorf("aria2: no URIs given")
	}
	raw, err := jsonMarshal(opts)
	if err != nil {
		return "", err
	}
	var gid string
	if err := c.Call(ctx, "aria2.addUri", &gid, uris, raw); err != nil {
		return "", err
	}
	return gid, nil
}

// AddTorrent is exposed for completeness (unused by the Bunkr flow).
func (c *Client) AddTorrent(ctx context.Context, torrent []byte, opts map[string]any) (string, error) {
	var gid string
	if err := c.Call(ctx, "aria2.addTorrent", &gid, string(torrent), mustJSON(opts)); err != nil {
		return "", err
	}
	return gid, nil
}

// TellStatus returns the full status of one GID.
func (c *Client) TellStatus(ctx context.Context, gid string, keys ...string) (FileStatus, error) {
	var st FileStatus
	params := []any{gid}
	if len(keys) > 0 {
		params = append(params, keys)
	}
	if err := c.Call(ctx, "aria2.tellStatus", &st, params...); err != nil {
		return st, err
	}
	return st, nil
}

// TellStatusBatch fetches many GIDs in a single round trip.
func (c *Client) TellStatusBatch(ctx context.Context, gids []string, keys ...string) (map[string]FileStatus, error) {
	out := make(map[string]FileStatus, len(gids))
	if len(gids) == 0 {
		return out, nil
	}
	calls := make([]Call, 0, len(gids))
	for _, g := range gids {
		params := []any{g}
		if len(keys) > 0 {
			params = append(params, keys)
		}
		calls = append(calls, Call{Method: "aria2.tellStatus", Params: params, Auth: true})
	}
	results, err := c.SystemMulticall(ctx, calls)
	if err != nil {
		return nil, err
	}
	for _, r := range results {
		if r.Err != nil {
			continue
		}
		var st FileStatus
		if err := unmarshal(r.Value, &st); err != nil || st.GID == "" {
			continue
		}
		out[st.GID] = st
	}
	return out, nil
}

// TellActive lists all active downloads.
func (c *Client) TellActive(ctx context.Context) ([]FileStatus, error) {
	var out []FileStatus
	if err := c.Call(ctx, "aria2.tellActive", &out); err != nil {
		return nil, err
	}
	return out, nil
}

// TellWaiting lists queued downloads (offset/count paging).
func (c *Client) TellWaiting(ctx context.Context, offset, count int) ([]FileStatus, error) {
	var out []FileStatus
	if err := c.Call(ctx, "aria2.tellWaiting", &out, offset, count); err != nil {
		return nil, err
	}
	return out, nil
}

// TellStopped lists finished downloads (offset/count paging).
func (c *Client) TellStopped(ctx context.Context, offset, count int) ([]FileStatus, error) {
	var out []FileStatus
	if err := c.Call(ctx, "aria2.tellStopped", &out, offset, count); err != nil {
		return nil, err
	}
	return out, nil
}

// GetGlobalStat returns the daemon-wide counters.
func (c *Client) GetGlobalStat(ctx context.Context) (GlobalStat, error) {
	var st GlobalStat
	err := c.Call(ctx, "aria2.getGlobalStat", &st)
	return st, err
}

// GetVersion returns the aria2c version string.
func (c *Client) GetVersion(ctx context.Context) (Version, error) {
	var v Version
	err := c.Call(ctx, "aria2.getVersion", &v)
	return v, err
}

// PauseAll suspends one download.
func (c *Client) PauseAll(ctx context.Context, gid string) error {
	return c.Call(ctx, "aria2.pause", nil, gid)
}

// UnpauseAll resumes one download.
func (c *Client) UnpauseAll(ctx context.Context, gid string) error {
	return c.Call(ctx, "aria2.unpause", nil, gid)
}

// PauseGlobal suspends every active download.
func (c *Client) PauseGlobal(ctx context.Context) error {
	return c.Call(ctx, "aria2.pauseAll", nil)
}

// ForceRemove detaches a download without waiting (leaves the .aria2 file).
func (c *Client) ForceRemove(ctx context.Context, gid string) error {
	return c.Call(ctx, "aria2.forceRemove", nil, gid)
}

// Remove detaches a download after it stops.
func (c *Client) Remove(ctx context.Context, gid string) error {
	return c.Call(ctx, "aria2.remove", nil, gid)
}

// PurgeDownloadResult clears a stopped download's result record.
func (c *Client) PurgeDownloadResult(ctx context.Context) error {
	return c.Call(ctx, "aria2.purgeDownloadResult", nil)
}

// ChangeGlobalOption updates daemon-wide options (e.g. max-concurrent-downloads).
func (c *Client) ChangeGlobalOption(ctx context.Context, opts map[string]any) error {
	return c.Call(ctx, "aria2.changeGlobalOption", nil, mustJSON(opts))
}

// ChangeOption updates options of a single download.
func (c *Client) ChangeOption(ctx context.Context, gid string, opts map[string]any) error {
	return c.Call(ctx, "aria2.changeOption", nil, gid, mustJSON(opts))
}

// RemoveDownloadResult drops a specific stopped record.
func (c *Client) RemoveDownloadResult(ctx context.Context, gid string) error {
	return c.Call(ctx, "aria2.removeDownloadResult", nil, gid)
}

// SortGIDs is a small helper used by the poller for deterministic output.
func SortGIDs(m map[string]FileStatus) []string {
	out := make([]string, 0, len(m))
	for g := range m {
		out = append(out, g)
	}
	sort.Strings(out)
	return out
}
