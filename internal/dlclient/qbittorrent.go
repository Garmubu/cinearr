package dlclient

import (
	"context"
	"strings"
	"sync"

	"cinearr/internal/i18n"
	"cinearr/internal/qbit"
)

// qbitClient adapts the qBittorrent Web API client.
type qbitClient struct {
	c        *qbit.Client
	mu       sync.Mutex
	prepared bool
}

func newQBit(s Settings) *qbitClient {
	return &qbitClient{c: qbit.New(qbit.Settings{URL: s.URL, Username: s.Username, Password: s.Password, Category: s.Category,
		SavePath: s.SavePath, Sequential: s.Sequential, RatioLimit: s.RatioLimit, SeedingTimeLimit: s.SeedingTimeLimit})}
}

func (q *qbitClient) Name() string { return "qBittorrent" }

func (q *qbitClient) Test(ctx context.Context) (TestResult, error) {
	v, err := q.c.Version(ctx)
	if err != nil {
		return TestResult{}, err
	}
	res := TestResult{Version: v}
	if err := q.c.EnsureCategory(ctx); err != nil {
		res.Warning = i18n.Tf("category not created: %v", err)
	} else {
		q.mu.Lock()
		q.prepared = true
		q.mu.Unlock()
	}
	return res, nil
}

func (q *qbitClient) prepare(ctx context.Context) error {
	q.mu.Lock()
	done := q.prepared
	q.mu.Unlock()
	if done {
		return nil
	}
	_, err := q.Test(ctx)
	return err
}

func (q *qbitClient) Add(ctx context.Context, r AddRequest) error {
	if err := q.prepare(ctx); err != nil {
		return err
	}
	o := qbit.AddOptions{Tags: r.Tag}
	if len(r.File) > 0 {
		return q.c.AddTorrentFile(ctx, r.FileName, r.File, o)
	}
	return q.c.AddMagnet(ctx, r.Magnet, o)
}

func (q *qbitClient) Torrents(ctx context.Context, hashes []string) ([]Torrent, error) {
	if len(hashes) == 0 {
		return nil, nil
	}
	list, err := q.c.Torrents(ctx, hashes...)
	if err != nil {
		return nil, err
	}
	out := make([]Torrent, 0, len(list))
	for _, t := range list {
		status, completed := qbit.Status(t.State)
		content := t.ContentPath
		if content == "" {
			content = joinRemote(t.SavePath, t.Name)
		}
		eta := t.ETA
		if eta >= 8640000 { // qBittorrent's "infinity"
			eta = -1
		}
		out = append(out, Torrent{
			Hash: strings.ToUpper(t.Hash), Name: t.Name, Size: t.Size, Progress: t.Progress, DlSpeed: t.DlSpeed, ETA: eta,
			Seeds: t.NumSeeds, Status: status, Completed: completed,
			SeedingDone: t.State == "pausedUP" || t.State == "stoppedUP",
			ContentPath: content, State: t.State,
		})
	}
	return out, nil
}

func (q *qbitClient) Files(ctx context.Context, hash string) ([]File, error) {
	list, err := q.c.Files(ctx, hash)
	if err != nil {
		return nil, err
	}
	out := make([]File, len(list))
	for i, f := range list {
		out[i] = File{Path: f.Name, Size: f.Size}
	}
	return out, nil
}

func (q *qbitClient) Remove(ctx context.Context, hash string, deleteData bool) error {
	return q.c.Delete(ctx, hash, deleteData)
}
