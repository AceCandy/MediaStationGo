// Package service — Telegram command registry and dispatch.
package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

type telegramCommandHandler func(args []string) (telegramCommandReply, error)

type telegramCommandDefinition struct {
	Aliases       []string
	AdminOnly     bool
	AdminOnlyText string
	GroupAllowed  bool
	Handle        telegramCommandHandler
}

func (s *TelegramBotService) telegramCommandDefinitions(ctx context.Context, channel *model.NotifyChannel, msg *TelegramMessage) []telegramCommandDefinition {
	adminOnly := "此命令仅管理员可用。"
	return []telegramCommandDefinition{
		{Aliases: []string{"/start"}, GroupAllowed: true, Handle: func(args []string) (telegramCommandReply, error) {
			if len(args) == 0 {
				return s.mainMenu(ctx, channel, telegramPrivateMessageForUser(msg)), nil
			}
			return s.cmdStart(ctx, msg, args), nil
		}},
		{Aliases: []string{"/menu"}, GroupAllowed: true, Handle: func(args []string) (telegramCommandReply, error) {
			return s.mainMenu(ctx, channel, telegramPrivateMessageForUser(msg)), nil
		}},
		{Aliases: []string{"/cancel"}, GroupAllowed: true, Handle: func(args []string) (telegramCommandReply, error) {
			s.takePending(int64(msg.From.ID))
			return telegramCommandReply{Text: "已取消当前操作。"}, nil
		}},
		{Aliases: []string{"/help"}, GroupAllowed: true, Handle: func(args []string) (telegramCommandReply, error) {
			return telegramCommandReply{Text: s.cmdHelp(ctx, msg)}, nil
		}},
		{Aliases: []string{"/hideadult", "/hide_adult", "/adult"}, GroupAllowed: true, Handle: func(args []string) (telegramCommandReply, error) { return s.cmdHideAdult(ctx, msg, args), nil }},
		{Aliases: []string{"/account", "/me", "/myinfo"}, GroupAllowed: true, Handle: func(args []string) (telegramCommandReply, error) { return s.replyAccount(ctx, msg), nil }},
		{Aliases: []string{"/count"}, GroupAllowed: true, Handle: func(args []string) (telegramCommandReply, error) { return s.cmdStats(ctx) }},
		{Aliases: []string{"/signin", "/checkin"}, GroupAllowed: true, Handle: func(args []string) (telegramCommandReply, error) { return s.replySignIn(ctx, msg), nil }},
		{Aliases: []string{"/devices"}, GroupAllowed: true, Handle: func(args []string) (telegramCommandReply, error) { return s.replyDevices(ctx, msg), nil }},
		{Aliases: []string{"/kick"}, GroupAllowed: true, Handle: func(args []string) (telegramCommandReply, error) { return s.cmdKick(ctx, msg, args), nil }},
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
		{Aliases: []string{"/unbind"}, AdminOnly: true, AdminOnlyText: adminOnly, Handle: func(args []string) (telegramCommandReply, error) { return s.cmdUnbind(ctx, args), nil }},
		{Aliases: []string{"/unbind_duplicates"}, AdminOnly: true, AdminOnlyText: adminOnly, Handle: func(args []string) (telegramCommandReply, error) { return s.cmdUnbindDuplicates(ctx), nil }},
		{Aliases: []string{"/unbind_inactive"}, AdminOnly: true, AdminOnlyText: adminOnly, Handle: func(args []string) (telegramCommandReply, error) { return s.cmdUnbindInactive(ctx, args), nil }},
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
	if telegramIsGroupChat(msg.Chat.Type) && !def.GroupAllowed {
		if def.AdminOnly && s.telegramUserIsAdmin(ctx, channel, msg.From.ID) {
			return telegramCommandReply{Text: telegramGroupPrivateAdminHint()}, nil
		}
		return telegramCommandReply{}, nil
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
	"/account": {}, "/me": {}, "/myinfo": {}, "/count": {}, "/signin": {}, "/checkin": {}, "/devices": {}, "/kick": {}, "/setname": {}, "/rename": {}, "/setpass": {}, "/passwd": {}, "/password": {},
	"/redeem": {}, "/redeem_register": {}, "/redeem_renew": {},
	"/register": {}, "/reg": {}, "/signup": {}, "/registration": {}, "/reg_switch": {}, "/openreg": {},
	"/capacity": {}, "/users": {}, "/gencode": {}, "/renew_user": {}, "/delete_user": {}, "/unbind": {}, "/unbind_duplicates": {}, "/unbind_inactive": {},
	"/devicepolicy": {}, "/policy": {}, "/antishare": {}, "/cleanup": {}, "/cleanup_mode": {}, "/cleanup_rule": {},
	"/ban": {}, "/unban": {}, "/status": {}, "/search": {}, "/downloads": {}, "/stats": {},
}

type telegramBotCommand struct {
	Command     string `json:"command"`
	Description string `json:"description"`
}

func telegramBotCommandMenu() []telegramBotCommand {
	return telegramPrivateBotCommandMenu()
}

func telegramPrivateBotCommandMenu() []telegramBotCommand {
	return []telegramBotCommand{
		{Command: "start", Description: "绑定账号或打开主菜单"},
		{Command: "menu", Description: "打开功能菜单"},
		{Command: "help", Description: "查看命令帮助"},
		{Command: "account", Description: "查看账号状态"},
		{Command: "myinfo", Description: "查看账号状态"},
		{Command: "count", Description: "查看媒体库数量"},
		{Command: "signin", Description: "签到"},
		{Command: "devices", Description: "查看登录设备"},
		{Command: "kick", Description: "踢下线设备"},
		{Command: "hideadult", Description: "隐藏/显示成人媒体库"},
		{Command: "redeem", Description: "兑换注册码或续期码"},
		{Command: "register", Description: "注册新账号"},
	}
}

func telegramGroupBotCommandMenu() []telegramBotCommand {
	return []telegramBotCommand{
		{Command: "start", Description: "打开群组自助菜单"},
		{Command: "menu", Description: "打开群组自助菜单"},
		{Command: "help", Description: "查看群组可用命令"},
		{Command: "account", Description: "查看账号状态"},
		{Command: "signin", Description: "签到"},
		{Command: "devices", Description: "查看登录设备"},
		{Command: "kick", Description: "踢下线设备"},
		{Command: "hideadult", Description: "隐藏/显示成人媒体库"},
	}
}

func telegramAdminBotCommandMenu() []telegramBotCommand {
	commands := append([]telegramBotCommand{}, telegramPrivateBotCommandMenu()...)
	commands = append(commands,
		telegramBotCommand{Command: "status", Description: "系统运行状态(管理员)"},
		telegramBotCommand{Command: "search", Description: "搜索媒体库(管理员)"},
		telegramBotCommand{Command: "downloads", Description: "下载列表(管理员)"},
		telegramBotCommand{Command: "stats", Description: "媒体库统计(管理员)"},
		telegramBotCommand{Command: "users", Description: "用户管理(管理员)"},
		telegramBotCommand{Command: "cleanup", Description: "删号规则巡检(管理员)"},
		telegramBotCommand{Command: "cleanup_rule", Description: "保号规则管理(管理员)"},
	)
	return commands
}

func registerTelegramBotCommands(ctx context.Context, cfg map[string]string) error {
	if strings.TrimSpace(cfg["bot_token"]) == "" {
		return nil
	}
	normalizeTelegramConfig(cfg)
	if err := telegramSetBotCommands(ctx, cfg, telegramPrivateBotCommandMenu(), nil); err != nil {
		return err
	}
	if err := telegramSetBotCommands(ctx, cfg, telegramPrivateBotCommandMenu(), map[string]interface{}{"type": "all_private_chats"}); err != nil {
		return err
	}
	if err := telegramSetBotCommands(ctx, cfg, telegramGroupBotCommandMenu(), map[string]interface{}{"type": "all_group_chats"}); err != nil {
		return err
	}

	adminCommands := telegramAdminBotCommandMenu()
	for _, adminID := range telegramConfiguredUserIDs(cfg["admin_user_ids"]) {
		_ = telegramSetBotCommands(ctx, cfg, adminCommands, map[string]interface{}{"type": "chat", "chat_id": adminID})
	}
	return nil
}

func telegramSetBotCommands(ctx context.Context, cfg map[string]string, commands []telegramBotCommand, scope map[string]interface{}) error {
	payload := map[string]interface{}{"commands": commands}
	if scope != nil {
		payload["scope"] = scope
	}
	return telegramPostJSON(ctx, cfg, "setMyCommands", payload, 15*time.Second)
}
