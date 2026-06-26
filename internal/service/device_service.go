package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

// DeviceService implements device/session tracking and the two decoupled
// enforcement policies requested by the operator:
//
//	① 防共享: too many concurrent playbacks / logged-in clients disables the
//	   account immediately; fingerprint mismatch is warning-based and disables
//	   the account after the configured warning threshold.
//	② Mgo 保号规则: admins define one or more keep rules; a sweep deletes
//	   accounts only when none of the enabled keep rules match.
//
// Safeguards: admin / protected accounts are never auto disabled or deleted;
// a Telegram notification is sent before a destructive action; every policy
// defaults to OFF.
type DeviceService struct {
	log      *zap.Logger
	repo     *repository.Container
	sessions *SessionTrackerService

	// notifyUser sends a Telegram message to the local user (resolved to their
	// Telegram binding). Wired by the bot service; nil disables notifications.
	notifyUser func(ctx context.Context, userID, text string)
}

// NewDeviceService constructs a DeviceService.
func NewDeviceService(log *zap.Logger, repo *repository.Container) *DeviceService {
	return &DeviceService{log: log, repo: repo}
}

// SetNotifier wires the per-user Telegram notification callback.
func (s *DeviceService) SetNotifier(fn func(ctx context.Context, userID, text string)) {
	s.notifyUser = fn
}

func (s *DeviceService) SetSessionTracker(tracker *SessionTrackerService) {
	s.sessions = tracker
}

// fingerprint derives a stable terminal hash from the device name. Client/app
// names are deliberately ignored so one phone/TV/PC using multiple apps is
// still counted as one terminal device; Client remains a login channel label.
func fingerprint(client, deviceName string) string {
	terminal := strings.ToLower(strings.TrimSpace(deviceName))
	if terminal == "" {
		terminal = strings.ToLower(strings.TrimSpace(client))
	}
	sum := sha256.Sum256([]byte(terminal))
	return hex.EncodeToString(sum[:])[:16]
}

// isProtected reports whether a user must never be auto disabled/deleted.
// Admins are always protected; the earliest admin (default admin) too.
func (s *DeviceService) isProtected(ctx context.Context, u *model.User) bool {
	return UserIsProtectedAccount(ctx, s.repo, u)
}

// RecordLogin records (or refreshes) a login channel at authentication time and
// runs the terminal-device + fingerprint anti-share checks. It is safe to call
// on every Emby/Jellyfin AuthenticateByName request.
func (s *DeviceService) RecordLogin(ctx context.Context, userID, deviceID, deviceName, client, ip string) {
	if userID == "" {
		return
	}
	username := ""
	if u, _ := s.repo.User.FindByID(ctx, userID); u != nil {
		username = u.Username
	}
	if deviceID == "" {
		// Fall back to a fingerprint-derived id so headless clients still count.
		deviceID = "fp-" + fingerprint(client, deviceName)
	}
	if s.sessions != nil {
		s.sessions.RecordLogin(ctx, userID, username, deviceID, deviceName, client, ip)
	}
	fp := fingerprint(client, deviceName)
	now := s.now()

	existing, _ := s.findTerminalDevice(ctx, userID, deviceID, fp)
	mismatch := false
	if existing == nil {
		_ = s.repo.UserDevice.Create(ctx, &model.UserDevice{
			UserID:      userID,
			DeviceID:    deviceID,
			DeviceName:  deviceName,
			Client:      client,
			Fingerprint: fp,
			LastIP:      ip,
			FirstSeenAt: now,
			LastSeenAt:  now,
		})
	} else {
		if existing.Fingerprint != "" && existing.Fingerprint != fp {
			mismatch = true
		}
		existing.DeviceID = deviceID
		existing.DeviceName = deviceName
		existing.Client = client
		existing.Fingerprint = fp
		existing.LastIP = ip
		existing.LastSeenAt = now
		existing.Kicked = false
		_ = s.repo.UserDevice.Save(ctx, existing)
		s.deleteStaleTerminalDeviceRows(ctx, userID, fp, existing.ID)
	}

	cfg := loadBotConfig(ctx, s.repo)
	if !cfg.AntiShareEnabled {
		return
	}

	if mismatch {
		s.registerFingerprintWarning(ctx, userID, fmt.Sprintf("设备指纹变更（设备：%s）", deviceLabel(deviceName, client)), cfg)
		return
	}
	since := now.Add(-time.Duration(cfg.ClientActiveDays) * 24 * time.Hour)
	if n, err := s.repo.UserDevice.CountActiveClients(ctx, userID, since); err == nil && int(n) > cfg.MaxLoggedClients {
		s.disableForPolicy(ctx, userID, fmt.Sprintf("同时登录终端设备 %d 台，超过上限 %d 台", n, cfg.MaxLoggedClients))
	}
}

// RecordPlayback marks a device as actively playing and runs the concurrent
// playback anti-share check. Call from playback-progress reporting.
func (s *DeviceService) RecordPlayback(ctx context.Context, userID, deviceID, deviceName, client string) {
	if userID == "" {
		return
	}
	username := ""
	if u, _ := s.repo.User.FindByID(ctx, userID); u != nil {
		username = u.Username
	}
	if deviceID == "" {
		deviceID = "fp-" + fingerprint(client, deviceName)
	}
	if s.sessions != nil {
		s.sessions.RecordPlayback(ctx, userID, username, deviceID, deviceName, client, "", "", 0, 0, false)
	}
	now := s.now()
	fp := fingerprint(client, deviceName)
	existing, _ := s.findTerminalDevice(ctx, userID, deviceID, fp)
	if existing == nil {
		existing = &model.UserDevice{
			UserID:      userID,
			DeviceID:    deviceID,
			DeviceName:  deviceName,
			Client:      client,
			Fingerprint: fp,
			FirstSeenAt: now,
			LastSeenAt:  now,
		}
		existing.LastPlayAt = &now
		_ = s.repo.UserDevice.Create(ctx, existing)
	} else {
		existing.DeviceID = deviceID
		existing.DeviceName = deviceName
		existing.Client = client
		existing.Fingerprint = fp
		existing.LastSeenAt = now
		existing.LastPlayAt = &now
		_ = s.repo.UserDevice.Save(ctx, existing)
		s.deleteStaleTerminalDeviceRows(ctx, userID, fp, existing.ID)
	}

	cfg := loadBotConfig(ctx, s.repo)
	if !cfg.AntiShareEnabled {
		return
	}
	since := now.Add(-time.Duration(cfg.PlayWindowSeconds) * time.Second)
	if n, err := s.repo.UserDevice.CountConcurrentPlaying(ctx, userID, since); err == nil && int(n) > cfg.MaxConcurrentPlay {
		s.disableForPolicy(ctx, userID, fmt.Sprintf("同时播放终端设备 %d 台，超过上限 %d 台", n, cfg.MaxConcurrentPlay))
	}
}

func (s *DeviceService) findTerminalDevice(ctx context.Context, userID, deviceID, fp string) (*model.UserDevice, error) {
	existing, err := s.repo.UserDevice.Find(ctx, userID, deviceID)
	if err != nil || existing != nil {
		return existing, err
	}
	if strings.TrimSpace(fp) == "" {
		return nil, nil
	}
	return s.repo.UserDevice.FindByFingerprint(ctx, userID, fp)
}

func (s *DeviceService) deleteStaleTerminalDeviceRows(ctx context.Context, userID, fp, keepID string) {
	if strings.TrimSpace(fp) == "" || strings.TrimSpace(keepID) == "" {
		return
	}
	if err := s.repo.UserDevice.DeleteByFingerprintExcept(ctx, userID, fp, keepID); err != nil && s.log != nil {
		s.log.Warn("device terminal cleanup failed",
			zap.String("user_id", userID),
			zap.String("fingerprint", fp),
			zap.Error(err))
	}
}

// registerFingerprintWarning increments the fingerprint warning counter for a
// user and disables the account once warnings exceed the threshold. Violations
// are debounced so a single burst counts at most once per minute.
func (s *DeviceService) registerFingerprintWarning(ctx context.Context, userID, reason string, cfg botConfig) {
	u, err := s.repo.User.FindByID(ctx, userID)
	if err != nil || u == nil {
		return
	}
	if s.isProtected(ctx, u) {
		s.log.Info("anti-share: skipping protected account", zap.String("user", u.Username), zap.String("reason", reason))
		return
	}
	now := s.now()
	if u.LastShareWarnAt != nil && now.Sub(*u.LastShareWarnAt) < time.Minute {
		return // debounce burst
	}

	warnings := u.ShareWarnings + 1
	if warnings > cfg.WarnThreshold {
		s.disableForPolicy(ctx, userID, fmt.Sprintf("多次设备指纹异常：%s", reason))
		s.log.Warn("anti-share: disabling account after fingerprint warnings",
			zap.String("user", u.Username), zap.Int("warnings", u.ShareWarnings), zap.String("reason", reason))
		return
	}
	_ = s.repo.User.UpdateFields(ctx, userID, map[string]any{
		"share_warnings":     warnings,
		"last_share_warn_at": &now,
	})
	left := cfg.WarnThreshold + 1 - warnings
	s.notify(ctx, userID, fmt.Sprintf("⚠️ 账号 <b>%s</b> 触发设备指纹警告：%s\n这是第 <b>%d</b> 次警告，再异常 <b>%d</b> 次将禁用账号。请使用 Bot 的「我的设备」踢下线异常设备。", u.Username, reason, warnings, left))
	s.log.Info("anti-share: warning issued", zap.String("user", u.Username), zap.Int("warnings", warnings), zap.String("reason", reason))
}

func (s *DeviceService) disableForPolicy(ctx context.Context, userID, reason string) {
	u, err := s.repo.User.FindByID(ctx, userID)
	if err != nil || u == nil || !u.IsActive {
		return
	}
	if s.isProtected(ctx, u) {
		s.log.Info("device policy: skipping protected account", zap.String("user", u.Username), zap.String("reason", reason))
		return
	}
	now := s.now()
	_ = s.repo.User.UpdateFields(ctx, userID, map[string]any{
		"is_active":          false,
		"last_share_warn_at": &now,
	})
	_ = s.repo.UserDevice.SetKickedByUser(ctx, userID, true)
	s.notify(ctx, userID, fmt.Sprintf("⛔️ 账号 <b>%s</b> 因触发设备规则已被禁用：%s\n请联系管理员解除禁用，或通过「我的设备」踢下线多余设备后再申请恢复。", u.Username, reason))
	s.log.Warn("device policy: disabled account", zap.String("user", u.Username), zap.String("reason", reason))
}

// SweepInactiveUsers is kept for compatibility with the existing scheduler; it
// now delegates to the custom account-cleanup policy.
func (s *DeviceService) SweepInactiveUsers(ctx context.Context) (int, error) {
	return s.SweepAccountCleanup(ctx)
}

// SweepAccountCleanup runs the admin-defined account cleanup policy once.
// Users are kept when they satisfy any enabled keep rule. Users that do not
// meet any enabled rule are deleted.
func (s *DeviceService) SweepAccountCleanup(ctx context.Context) (int, error) {
	cfg := loadBotConfig(ctx, s.repo)
	if !cfg.AccountCleanupEnabled {
		return 0, nil
	}
	candidates, err := s.accountCleanupCandidates(ctx, cfg)
	if err != nil {
		return 0, err
	}
	removed := 0
	for _, candidate := range candidates {
		s.notify(ctx, candidate.UserID, fmt.Sprintf("⛔️ 账号 <b>%s</b> 未满足保号规则，已被清理。\n规则结果：%s\n如需恢复请联系管理员。", candidate.Username, candidate.Details))
		s.log.Warn("account cleanup: deleting account", zap.String("user", candidate.Username), zap.String("details", candidate.Details))
		_ = s.repo.UserDevice.DeleteByUser(ctx, candidate.UserID)
		if err := s.repo.User.Delete(ctx, candidate.UserID); err == nil {
			removed++
		}
	}
	return removed, nil
}

type accountCleanupCandidate struct {
	UserID   string
	Username string
	Details  string
}

func (s *DeviceService) PreviewAccountCleanup(ctx context.Context) ([]accountCleanupCandidate, error) {
	cfg := loadBotConfig(ctx, s.repo)
	if !cfg.AccountCleanupEnabled {
		return nil, nil
	}
	return s.accountCleanupCandidates(ctx, cfg)
}

func (s *DeviceService) accountCleanupCandidates(ctx context.Context, cfg botConfig) ([]accountCleanupCandidate, error) {
	users, err := s.repo.User.List(ctx)
	if err != nil {
		return nil, err
	}
	candidates := make([]accountCleanupCandidate, 0)
	for i := range users {
		u := &users[i]
		if s.isProtected(ctx, u) || !u.IsActive {
			continue
		}
		keep, details := s.userMatchesCleanupPolicy(ctx, u, cfg)
		if keep {
			continue
		}
		candidates = append(candidates, accountCleanupCandidate{
			UserID:   u.ID,
			Username: u.Username,
			Details:  details,
		})
	}
	return candidates, nil
}

// KickDevice marks a device as kicked so the next request from it is rejected
// (the client must log in again). Returns the affected device for messaging.
func (s *DeviceService) KickDevice(ctx context.Context, userID, deviceID string) error {
	d, err := s.repo.UserDevice.Find(ctx, userID, deviceID)
	if err != nil {
		return err
	}
	if d == nil {
		return fmt.Errorf("device not found")
	}
	if fp := strings.TrimSpace(d.Fingerprint); fp != "" {
		return s.repo.UserDevice.SetKickedByFingerprint(ctx, userID, fp, true)
	}
	return s.repo.UserDevice.SetKicked(ctx, d.ID, true)
}

// KickAllDevices marks all devices for a user as kicked.
func (s *DeviceService) KickAllDevices(ctx context.Context, userID string) error {
	return s.repo.UserDevice.SetKickedByUser(ctx, userID, true)
}

// ListDevices returns the device sessions for a user.
func (s *DeviceService) ListDevices(ctx context.Context, userID string) ([]model.UserDevice, error) {
	rows, err := s.repo.UserDevice.ListByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	rows = collapseUserDeviceRows(rows)
	if s.sessions == nil {
		return rows, nil
	}
	now := s.sessions.now()
	byDevice := make(map[string]int, len(rows))
	byTerminal := make(map[string]int, len(rows))
	for i := range rows {
		byDevice[rows[i].DeviceID] = i
		byTerminal[userDeviceTerminalKey(rows[i])] = i
	}
	for _, sess := range s.sessions.ListByUser(ctx, userID) {
		online := sess.LastActivityAt.After(now.Add(-realtimeSessionOnlineTTL))
		idx, ok := byDevice[sess.DeviceID]
		if !ok {
			idx, ok = byTerminal[sessionDeviceKey(sess)]
		}
		if ok {
			if sess.LastActivityAt.After(rows[idx].LastSeenAt) {
				rows[idx].LastSeenAt = sess.LastActivityAt
			}
			if sess.DeviceName != "" {
				rows[idx].DeviceName = sess.DeviceName
			}
			if sess.Client != "" {
				rows[idx].Client = sess.Client
			}
			if sess.RemoteEndPoint != "" {
				rows[idx].LastIP = sess.RemoteEndPoint
			}
			if sess.LastPlaybackAt != nil {
				rows[idx].LastPlayAt = sess.LastPlaybackAt
			}
			rows[idx].Realtime = true
			rows[idx].Online = online
			rows[idx].Playing = sess.IsPlaying && online
			continue
		}
		row := model.UserDevice{
			UserID:      userID,
			DeviceID:    sess.DeviceID,
			DeviceName:  sess.DeviceName,
			Client:      sess.Client,
			Fingerprint: fingerprint(sess.Client, sess.DeviceName),
			LastIP:      sess.RemoteEndPoint,
			FirstSeenAt: sess.LastActivityAt,
			LastSeenAt:  sess.LastActivityAt,
			LastPlayAt:  sess.LastPlaybackAt,
			Realtime:    true,
			Online:      online,
			Playing:     sess.IsPlaying && online,
		}
		row.ID = "rt:" + sess.ID
		rows = append(rows, row)
		byDevice[row.DeviceID] = len(rows) - 1
		byTerminal[userDeviceTerminalKey(row)] = len(rows) - 1
	}
	sort.SliceStable(rows, func(i, j int) bool {
		return rows[i].LastSeenAt.After(rows[j].LastSeenAt)
	})
	return rows, nil
}

func collapseUserDeviceRows(rows []model.UserDevice) []model.UserDevice {
	if len(rows) < 2 {
		return rows
	}
	out := make([]model.UserDevice, 0, len(rows))
	byTerminal := map[string]int{}
	for _, row := range rows {
		key := userDeviceTerminalKey(row)
		if idx, ok := byTerminal[key]; ok {
			out[idx] = mergeUserDeviceRows(out[idx], row)
			continue
		}
		byTerminal[key] = len(out)
		out = append(out, row)
	}
	return out
}

func mergeUserDeviceRows(a, b model.UserDevice) model.UserDevice {
	if b.LastSeenAt.After(a.LastSeenAt) {
		a.ID = b.ID
		a.DeviceID = b.DeviceID
		a.DeviceName = b.DeviceName
		a.Client = b.Client
		a.LastIP = b.LastIP
		a.FirstSeenAt = earlierTime(a.FirstSeenAt, b.FirstSeenAt)
		a.LastSeenAt = b.LastSeenAt
		a.LastPlayAt = latestOptionalTime(a.LastPlayAt, b.LastPlayAt)
		a.Fingerprint = firstNonEmptyString(b.Fingerprint, a.Fingerprint)
		a.Kicked = b.Kicked
		return a
	}
	a.FirstSeenAt = earlierTime(a.FirstSeenAt, b.FirstSeenAt)
	a.LastPlayAt = latestOptionalTime(a.LastPlayAt, b.LastPlayAt)
	a.Fingerprint = firstNonEmptyString(a.Fingerprint, b.Fingerprint)
	return a
}

func userDeviceTerminalKey(row model.UserDevice) string {
	if key := strings.TrimSpace(row.Fingerprint); key != "" {
		return key
	}
	if strings.TrimSpace(row.DeviceName) != "" {
		return "fp-" + fingerprint(row.Client, row.DeviceName)
	}
	if id := strings.TrimSpace(row.DeviceID); id != "" {
		return id
	}
	return "unknown"
}

func earlierTime(a, b time.Time) time.Time {
	if a.IsZero() || (!b.IsZero() && b.Before(a)) {
		return b
	}
	return a
}

func latestOptionalTime(a, b *time.Time) *time.Time {
	if a == nil {
		return b
	}
	if b != nil && b.After(*a) {
		return b
	}
	return a
}

// IsDeviceKicked reports whether a (user, device) pair was kicked and should be
// forced to re-authenticate.
func (s *DeviceService) IsDeviceKicked(ctx context.Context, userID, deviceID string) bool {
	return s.IsTerminalKicked(ctx, userID, deviceID, "", "")
}

// IsTerminalKicked reports whether a request belongs to a kicked terminal.
func (s *DeviceService) IsTerminalKicked(ctx context.Context, userID, deviceID, deviceName, client string) bool {
	if userID == "" {
		return false
	}
	if strings.TrimSpace(deviceID) != "" {
		d, err := s.repo.UserDevice.Find(ctx, userID, deviceID)
		if err == nil && d != nil {
			return d.Kicked
		}
	}
	fp := ""
	if strings.TrimSpace(deviceName) != "" {
		fp = fingerprint(client, deviceName)
	}
	if fp == "" {
		return false
	}
	d, err := s.repo.UserDevice.FindByFingerprint(ctx, userID, fp)
	return err == nil && d != nil && d.Kicked
}

func (s *DeviceService) UserRecentlyActive(ctx context.Context, userID string, within time.Duration) bool {
	return s.sessions != nil && s.sessions.UserRecentlyActive(ctx, userID, within)
}

func (s *DeviceService) now() time.Time {
	if s != nil && s.sessions != nil && s.sessions.now != nil {
		return s.sessions.now()
	}
	return time.Now()
}

func (s *DeviceService) notify(ctx context.Context, userID, text string) {
	if s.notifyUser != nil {
		s.notifyUser(ctx, userID, text)
	}
}

// randomWindowDays returns a random integer in [min,max]. The window is
// intentionally non-fixed per the operator's requirement ("随机触发").
func randomWindowDays(min, max int) int {
	if min < 1 {
		min = 1
	}
	if max < min {
		max = min
	}
	if max == min {
		return min
	}
	return min + secureRandomIntn(max-min+1)
}

func (s *DeviceService) userMatchesCleanupPolicy(ctx context.Context, u *model.User, cfg botConfig) (bool, string) {
	rules := make([]accountCleanupRule, 0, len(cfg.AccountCleanupRules))
	for _, r := range cfg.AccountCleanupRules {
		if r.Enabled {
			rules = append(rules, r)
		}
	}
	if len(rules) == 0 {
		return true, "无启用规则，跳过"
	}
	matches := 0
	parts := make([]string, 0, len(rules))
	for _, r := range rules {
		ok, detail := s.userMatchesCleanupRule(ctx, u, r)
		if ok {
			matches++
			parts = append(parts, "✅ "+detail)
		} else {
			parts = append(parts, "❌ "+detail)
		}
	}
	required := 1
	return matches >= required, fmt.Sprintf("满足 %d/%d 条，需要 %d 条；%s", matches, len(rules), required, strings.Join(parts, "；"))
}

func (s *DeviceService) userMatchesCleanupRule(ctx context.Context, u *model.User, r accountCleanupRule) (bool, string) {
	switch r.Type {
	case "watch_hours":
		windowDays := randomWindowDays(r.WindowDaysMin, r.WindowDaysMax)
		since := time.Now().Add(-time.Duration(windowDays) * 24 * time.Hour)
		watched, _ := s.repo.UserDevice.WatchedMillisSince(ctx, u.ID, since)
		hours := float64(watched) / 3600000
		return hours >= r.MinHours, fmt.Sprintf("%s：近 %d 天观看 %.1f/%.1f 小时", r.Name, windowDays, hours, r.MinHours)
	case "recent_login":
		days := r.WindowDaysMax
		if days < 1 {
			days = r.WindowDaysMin
		}
		cutoff := time.Now().Add(-time.Duration(days) * 24 * time.Hour)
		ok := u.LastLoginAt != nil && u.LastLoginAt.After(cutoff)
		if !ok && s.sessions != nil {
			ok = s.sessions.UserRecentlyActive(ctx, u.ID, time.Duration(days)*24*time.Hour)
		}
		return ok, fmt.Sprintf("%s：%d 天内登录", r.Name, days)
	case "signin_streak":
		rec, _ := s.repo.SignIn.Get(ctx, u.ID)
		streak := 0
		if rec != nil {
			streak = rec.StreakDays
		}
		return streak >= r.MinCount, fmt.Sprintf("%s：连续签到 %d/%d 天", r.Name, streak, r.MinCount)
	case "account_age_grace":
		days := r.MinCount
		if days < 1 {
			days = r.WindowDaysMax
		}
		ok := u.CreatedAt.After(time.Now().Add(-time.Duration(days) * 24 * time.Hour))
		return ok, fmt.Sprintf("%s：新账号 %d 天宽限", r.Name, days)
	default:
		return false, r.Name + "：未知规则"
	}
}

func deviceLabel(name, client string) string {
	name = strings.TrimSpace(name)
	client = strings.TrimSpace(client)
	switch {
	case name != "" && client != "":
		return name + " / " + client
	case name != "":
		return name
	case client != "":
		return client
	default:
		return "未知设备"
	}
}
