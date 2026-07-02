package service

import (
	"context"
	"errors"
	"path"
	"strings"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

// DownloadTaskMeta carries public display metadata for a download. It is
// deliberately separate from the private torrent URL so API responses never
// need to expose tracker tokens.
type DownloadTaskMeta struct {
	SubscriptionID       string
	Title                string
	PosterURL            string
	BackdropURL          string
	Overview             string
	MediaType            string
	MediaCategory        string
	SourceCategory       string
	OriginalName         string
	OriginalLanguage     string
	Year                 int
	Rating               float32
	Genres               string
	AllowExistingLibrary bool
}

type downloadAddRequest struct {
	title        string
	savePath     string
	qbitCategory string
	meta         DownloadTaskMeta
}

// AddDownload accepts a magnet URL / HTTP URL and persists a tracking row.
func (d *DownloadService) AddDownload(ctx context.Context, userID, urlStr, savePath string) (*model.DownloadTask, error) {
	return d.AddDownloadWithMeta(ctx, userID, urlStr, savePath, DownloadTaskMeta{})
}

func (d *DownloadService) AddDownloadWithMeta(ctx context.Context, userID, urlStr, savePath string, meta DownloadTaskMeta) (*model.DownloadTask, error) {
	req, err := d.prepareDownloadAdd(ctx, urlStr, savePath, meta)
	if err != nil {
		return nil, err
	}
	if !req.meta.AllowExistingLibrary && d.localMediaAlreadyExists(ctx, req.title) {
		return nil, ErrMediaAlreadyInLibrary
	}
	if existing, ok := d.findExistingDownloadTask(ctx, req); ok {
		d.linkExistingDownloadTaskToSubscription(ctx, existing, req)
		return existing, ErrDownloadAlreadyExists
	}
	_ = d.ReloadConfig(ctx)
	if !d.qb.IsConfigured() {
		return nil, d.defaultDownloaderNotConfiguredError(ctx)
	}
	if d.torrentExistsByIdentity(ctx, req) {
		task, err := d.createTask(ctx, userID, urlStr, req.savePath, req.meta)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(req.meta.SubscriptionID) != "" {
			return task, nil
		}
		return task, ErrDownloadAlreadyExists
	}
	if err := d.addPreparedDownloadToClient(ctx, urlStr, &req); err != nil {
		if errors.Is(err, ErrDownloadAlreadyExists) && strings.TrimSpace(req.meta.SubscriptionID) != "" {
			return d.createTask(ctx, userID, urlStr, req.savePath, req.meta)
		}
		return nil, err
	}
	return d.createTask(ctx, userID, urlStr, req.savePath, req.meta)
}

func (d *DownloadService) prepareDownloadAdd(ctx context.Context, urlStr, savePath string, meta DownloadTaskMeta) (downloadAddRequest, error) {
	if urlStr == "" {
		return downloadAddRequest{}, errors.New("empty url")
	}
	title := strings.TrimSpace(meta.Title)
	if title == "" {
		title = publicDownloadTitle(urlStr)
		meta.Title = title
	}
	autoClassify := downloadSmartClassifyEnabled(ctx, d.repo, d.organizer)
	savePath, resolvedCategory := d.resolveDownloadSavePath(ctx, savePath, meta, autoClassify)
	if !autoClassify {
		meta.MediaCategory = ""
	} else if strings.TrimSpace(meta.MediaCategory) == "" {
		meta.MediaCategory = resolvedCategory
	}
	return downloadAddRequest{
		title:        title,
		savePath:     savePath,
		qbitCategory: strings.TrimSpace(meta.MediaCategory),
		meta:         meta,
	}, nil
}

func (d *DownloadService) addPreparedDownloadToClient(ctx context.Context, urlStr string, req *downloadAddRequest) error {
	var siteFetchErr error
	if d.site != nil {
		if data, name, err := d.site.FetchTorrentFile(ctx, urlStr); err == nil {
			if err := d.qb.AddTorrentFileWithCategory(ctx, data, name, req.savePath, req.qbitCategory); err != nil {
				return err
			}
			if strings.TrimSpace(req.meta.Title) == "" {
				req.meta.Title = strings.TrimSuffix(name, path.Ext(name))
			}
			return nil
		} else {
			siteFetchErr = err
		}
	}
	if err := d.qb.AddTorrentWithCategory(ctx, urlStr, req.savePath, req.qbitCategory); err != nil {
		if siteFetchErr != nil && !strings.Contains(siteFetchErr.Error(), "no matching PT site") {
			return errors.Join(err, siteFetchErr)
		}
		return err
	}
	return nil
}

func (d *DownloadService) resolveDownloadSavePath(ctx context.Context, explicitSavePath string, meta DownloadTaskMeta, autoClassify bool) (string, string) {
	if strings.TrimSpace(explicitSavePath) != "" {
		if !autoClassify {
			return explicitSavePath, ""
		}
		return explicitSavePath, strings.TrimSpace(meta.MediaCategory)
	}
	base := downloadDefaultSaveRoot(ctx, d.repo)
	if strings.TrimSpace(base) == "" {
		return "", strings.TrimSpace(meta.MediaCategory)
	}
	mediaType := normalizeMediaType(meta.MediaType, meta.Title, meta.SourceCategory)
	category := strings.TrimSpace(meta.MediaCategory)
	if category == "" {
		category = classifyMediaCategory(mediaClassifyInput{
			MediaType: mediaType,
			Title:     meta.Title,
			Category:  meta.SourceCategory,
		}, downloadCategoryMap(d.organizer))
	}
	if !autoClassify || category == "" {
		return base, ""
	}
	return downloadSavePathCategoryRoot(base, sanitizeFilename(category)), category
}

func (d *DownloadService) createTask(ctx context.Context, userID, urlStr, savePath string, meta DownloadTaskMeta) (*model.DownloadTask, error) {
	title := strings.TrimSpace(meta.Title)
	if title == "" {
		title = publicDownloadTitle(urlStr)
	}
	t := &model.DownloadTask{
		UserID:               userID,
		SubscriptionID:       strings.TrimSpace(meta.SubscriptionID),
		Source:               "qbittorrent",
		URL:                  urlStr,
		Title:                title,
		PosterURL:            meta.PosterURL,
		BackdropURL:          meta.BackdropURL,
		Overview:             meta.Overview,
		SavePath:             savePath,
		MediaType:            meta.MediaType,
		MediaCategory:        meta.MediaCategory,
		OriginalName:         meta.OriginalName,
		OriginalLanguage:     meta.OriginalLanguage,
		Year:                 meta.Year,
		Rating:               meta.Rating,
		Genres:               meta.Genres,
		Status:               "queued",
		AllowExistingLibrary: meta.AllowExistingLibrary,
	}
	if err := d.repo.Download.Create(ctx, t); err != nil {
		return nil, err
	}
	return t, nil
}
