// Package service — Telegram command registry and dispatch.
package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

type telegramCommandHandler func(args []string) (telegramCommandReply, error)

type telegramCommandDefinition struct {
	Aliases       []string
	AdminOnly     bool
	AdminOnlyText string
	Handle        telegramCommandHandler
}

func (s *TelegramBotService) telegramCommandDefinitions(ctx context.Context, channel *model.NotifyChannel, msg *TelegramMessage) []telegramCommandDefinition {
	adminOnly := "此命令仅管理员可用。"
	return []telegramCommandDefinition{
		{Aliases: []string{"/start"}, Handle: func(args []string) (telegramCommandReply, error) {
			if len(args) == 0 {
				return s.mainMenu(ctx, channel, msg), nil
			}
			return s.cmdStart(ctx, msg, args), nil
		}},
		{Aliases: []string{"/menu"}, Handle: func(args []string) (telegramCommandReply, error) { return s.mainMenu(ctx, channel, msg), nil }},
		{Aliases: []string{"/cancel"}, Handle: func(args []string) (telegramCommandReply, error) {
			s.takePending(int64(msg.From.ID))
			return telegramCommandReply{Text: "已取消当前操作。"}, nil
		}},
		{Aliases: []string{"/help"}, Handle: func(args []string) (telegramCommandReply, error) {
			return telegramCommandReply{Text: s.cmdHelp(ctx, msg)}, nil
		}},
		{Aliases: []string{"/hideadult", "/hide_adult", "/adult"}, Handle: func(args []string) (telegramCommandReply, error) { return s.cmdHideAdult(ctx, msg, args), nil }},
		{Aliases: []string{"/account", "/me"}, Handle: func(args []string) (telegramCommandReply, error) { return s.replyAccount(ctx, msg), nil }},
		{Aliases: []string{"/signin", "/checkin"}, Handle: func(args []string) (telegramCommandReply, error) { return s.replySignIn(ctx, msg), nil }},
		{Aliases: []string{"/devices"}, Handle: func(args []string) (telegramCommandReply, error) { return s.replyDevices(ctx, msg), nil }},
		{Aliases: []string{"/kick"}, Handle: func(args []string) (telegramCommandReply, error) { return s.cmdKick(ctx, msg, args), nil }},
		{Aliases: []string{"/setname", "/rename"}, Handle: func(args []string) (telegramCommandReply, error) { return s.cmdSetName(ctx, msg, args), nil }},
		{Aliases: []string{"/setpass", "/passwd", "/password"}, Handle: func(args []string) (telegramCommandReply, error) { return s.cmdSetPass(ctx, msg, args), nil }},
		{Aliases: []string{"/redeem"}, Handle: func(args []string) (telegramCommandReply, error) { return s.cmdRedeem(ctx, channel, msg, args), nil }},
		{Aliases: []string{"/redeem_register"}, Handle: func(args []string) (telegramCommandReply, error) {
			return s.cmdRedeemRegister(ctx, channel, msg, args), nil
		}},
		{Aliases: []string{"/redeem_renew"}, Handle: func(args []string) (telegramCommandReply, error) { return s.cmdRedeemRenew(ctx, msg, args), nil }},
		{Aliases: []string{"/register", "/reg", "/signup"}, Handle: func(args []string) (telegramCommandReply, error) { return s.cmdRegister(ctx, channel, msg, args), nil }},

		{Aliases: []string{"/registration", "/reg_switch", "/openreg"}, AdminOnly: true, AdminOnlyText: adminOnly, Handle: func(args []string) (telegramCommandReply, error) { return s.cmdRegistrationToggle(ctx, args), nil }},
		{Aliases: []string{"/capacity"}, AdminOnly: true, AdminOnlyText: adminOnly, Handle: func(args []string) (telegramCommandReply, error) { return s.replyCapacity(ctx), nil }},
		{Aliases: []string{"/users"}, AdminOnly: true, AdminOnlyText: adminOnly, Handle: func(args []string) (telegramCommandReply, error) { return s.replyUserList(ctx), nil }},
		{Aliases: []string{"/gencode"}, AdminOnly: true, AdminOnlyText: adminOnly, Handle: func(args []string) (telegramCommandReply, error) { return s.cmdGenCode(ctx, msg, args), nil }},
		{Aliases: []string{"/renew_user"}, AdminOnly: true, AdminOnlyText: adminOnly, Handle: func(args []string) (telegramCommandReply, error) { return s.cmdUserRenew(ctx, args), nil }},
		{Aliases: []string{"/delete_user"}, AdminOnly: true, AdminOnlyText: adminOnly, Handle: func(args []string) (telegramCommandReply, error) { return s.cmdUserDelete(ctx, args), nil }},
		{Aliases: []string{"/devicepolicy", "/policy"}, AdminOnly: true, AdminOnlyText: adminOnly, Handle: func(args []string) (telegramCommandReply, error) { return s.cmdDevicePolicy(ctx, args), nil }},
		{Aliases: []string{"/antishare"}, AdminOnly: true, AdminOnlyText: adminOnly, Handle: func(args []string) (telegramCommandReply, error) { return s.cmdAntiShare(ctx, args), nil }},
		{Aliases: []string{"/cleanup"}, AdminOnly: true, AdminOnlyText: adminOnly, Handle: func(args []string) (telegramCommandReply, error) { return s.cmdCleanup(ctx, args), nil }},
		{Aliases: []string{"/cleanup_mode"}, AdminOnly: true, AdminOnlyText: adminOnly, Handle: func(args []string) (telegramCommandReply, error) { return s.cmdCleanupMode(ctx, args), nil }},
		{Aliases: []string{"/cleanup_rule"}, AdminOnly: true, AdminOnlyText: adminOnly, Handle: func(args []string) (telegramCommandReply, error) { return s.cmdCleanupRule(ctx, args), nil }},
		{Aliases: []string{"/ban"}, AdminOnly: true, AdminOnlyText: adminOnly, Handle: func(args []string) (telegramCommandReply, error) { return s.cmdUserBan(ctx, args, false), nil }},
		{Aliases: []string{"/unban"}, AdminOnly: true, AdminOnlyText: adminOnly, Handle: func(args []string) (telegramCommandReply, error) { return s.cmdUserBan(ctx, args, true), nil }},
		{Aliases: []string{"/status"}, AdminOnly: true, AdminOnlyText: "此命令仅管理员可用。普通用户只能使用 /start 绑定账号，并通过按钮隐藏成人目录。", Handle: func(args []string) (telegramCommandReply, error) { return s.cmdStatus(ctx) }},
		{Aliases: []string{"/search"}, AdminOnly: true, AdminOnlyText: adminOnly, Handle: func(args []string) (telegramCommandReply, error) { return s.cmdSearch(ctx, args) }},
		{Aliases: []string{"/downloads"}, AdminOnly: true, AdminOnlyText: adminOnly, Handle: func(args []string) (telegramCommandReply, error) { return s.cmdDownloads(ctx) }},
		{Aliases: []string{"/stats"}, AdminOnly: true, AdminOnlyText: adminOnly, Handle: func(args []string) (telegramCommandReply, error) { return s.cmdStats(ctx) }},
	}
}

func (s *TelegramBotService) telegramCommandRegistry(ctx context.Context, channel *model.NotifyChannel, msg *TelegramMessage) map[string]telegramCommandDefinition {
	defs := s.telegramCommandDefinitions(ctx, channel, msg)
	registry := make(map[string]telegramCommandDefinition, len(defs)*2)
	for _, def := range defs {
		for _, alias := range def.Aliases {
			registry[alias] = def
		}
	}
	return registry
}

// executeCommand parses and dispatches Telegram commands through a registry so
// adding a command does not grow a monolithic switch.
func (s *TelegramBotService) executeCommand(ctx context.Context, channel *model.NotifyChannel, msg *TelegramMessage, text string) (telegramCommandReply, error) {
	parts := strings.Fields(text)
	if len(parts) == 0 {
		return telegramCommandReply{}, nil
	}

	cmd := telegramCommandName(parts[0])
	args := parts[1:]
	if msg.Chat.Type != "" && msg.Chat.Type != "private" && !s.telegramChatAllowed(channel, msg.Chat.ID) {
		return telegramCommandReply{Text: "此群组/频道未绑定到 Bot 管理入口，请在通知渠道里填写「绑定群组 ID」或「绑定频道 ID」。"}, nil
	}

	def, ok := s.telegramCommandRegistry(ctx, channel, msg)[cmd]
	if !ok {
		return telegramCommandReply{Text: fmt.Sprintf("未知命令: %s\n\n输入 /help 查看可用命令列表。", cmd)}, nil
	}
	if def.AdminOnly && !s.telegramUserIsAdmin(ctx, channel, msg.From.ID) {
		return telegramCommandReply{Text: def.AdminOnlyText}, nil
	}
	return def.Handle(args)
}

func telegramSupportedCommand(cmd string) bool {
	cmd = telegramCommandName(cmd)
	if cmd == "" {
		return false
	}
	_, ok := telegramSupportedCommandSet[cmd]
	return ok
}

var telegramSupportedCommandSet = map[string]struct{}{
	"/start": {}, "/menu": {}, "/cancel": {}, "/help": {}, "/hideadult": {}, "/hide_adult": {}, "/adult": {},
	"/account": {}, "/me": {}, "/signin": {}, "/checkin": {}, "/devices": {}, "/kick": {}, "/setname": {}, "/rename": {}, "/setpass": {}, "/passwd": {}, "/password": {},
	"/redeem": {}, "/redeem_register": {}, "/redeem_renew": {},
	"/register": {}, "/reg": {}, "/signup": {}, "/registration": {}, "/reg_switch": {}, "/openreg": {},
	"/capacity": {}, "/users": {}, "/gencode": {}, "/renew_user": {}, "/delete_user": {},
	"/devicepolicy": {}, "/policy": {}, "/antishare": {}, "/cleanup": {}, "/cleanup_mode": {}, "/cleanup_rule": {},
	"/ban": {}, "/unban": {}, "/status": {}, "/search": {}, "/downloads": {}, "/stats": {},
}
