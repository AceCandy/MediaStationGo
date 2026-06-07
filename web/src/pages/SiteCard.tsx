import {
  CheckCircle,
  Edit3,
  HelpCircle,
  RefreshCw,
  Trash2,
  Wifi,
  XCircle,
} from "lucide-react";

import type { Site } from "../types";
import {
  AUTH_TYPE_LABELS,
  SITE_TYPE_ABBR,
  SITE_TYPE_COLORS,
  SITE_TYPE_LABELS,
} from "./sitesPageModel";

type SiteCardProps = {
  site: Site;
  testing: boolean;
  onTest: () => void;
  onEdit: () => void;
  onDelete: () => void;
};

export function SiteCard({
  site,
  testing,
  onTest,
  onEdit,
  onDelete,
}: SiteCardProps) {
  return (
    <div className="glass-panel p-4 space-y-3 transition-all hover:border-primary-400/30">
      {/* 头部 */}
      <div className="flex items-start justify-between">
        <div className="flex items-center gap-2 min-w-0">
          <div
            className={`w-8 h-8 rounded-xl flex items-center justify-center text-xs font-bold shrink-0 ${SITE_TYPE_COLORS[site.type] || "bg-sand-500/15 text-ink-50"}`}
          >
            {SITE_TYPE_ABBR[site.type] || "?"}
          </div>
          <div className="min-w-0">
            <div className="font-medium text-ink-600 truncate">{site.name}</div>
            <div className="text-xs text-ink-50 truncate max-w-[160px]">
              {site.url}
            </div>
          </div>
        </div>
        {/* 状态指示 */}
        <div className="flex items-center gap-1 shrink-0 ml-2">
          {site.last_check_at ? (
            site.last_error ? (
              <XCircle size={14} className="text-red-400" />
            ) : (
              <CheckCircle size={14} className="text-green-400" />
            )
          ) : (
            <HelpCircle size={14} className="text-sand-500" />
          )}
          {!site.enabled && (
            <span className="text-xs text-sand-500 ml-1">已停用</span>
          )}
        </div>
      </div>

      {/* 标签 */}
      <div className="flex flex-wrap gap-1.5">
        <span className="text-xs px-1.5 py-0.5 rounded-lg bg-gray-50 text-ink-50">
          {SITE_TYPE_LABELS[site.type] || site.type}
        </span>
        <span className="text-xs px-1.5 py-0.5 rounded-lg bg-gray-50 text-ink-50">
          {AUTH_TYPE_LABELS[site.auth_type] || site.auth_type}
        </span>
        {site.is_default && (
          <span className="text-xs px-1.5 py-0.5 rounded-lg bg-primary-400/15 text-brand-500">
            默认
          </span>
        )}
        {site.use_proxy && (
          <span className="text-xs px-1.5 py-0.5 rounded-lg bg-blue-500/15 text-blue-400">
            代理
          </span>
        )}
        {site.rate_limit && (
          <span className="text-xs px-1.5 py-0.5 rounded-lg bg-yellow-500/15 text-yellow-400">
            限流
          </span>
        )}
        {site.browser_emulation && (
          <span className="text-xs px-1.5 py-0.5 rounded-lg bg-purple-500/15 text-purple-400">
            浏览器
          </span>
        )}
      </div>

      {/* 状态与统计（只读） */}
      <div className="text-xs text-sand-500 space-y-0.5">
        <div>
          状态：
          <span
            className={
              site.login_status === "ok"
                ? "text-green-400"
                : site.login_status === "failed"
                  ? "text-red-400"
                  : "text-ink-50"
            }
          >
            {site.login_status || "unknown"}
          </span>
        </div>
        {(site.upload_bytes || 0) > 0 && (
          <div>
            ↑ {Math.round(((site.upload_bytes ?? 0) / 1073741824) * 100) / 100}{" "}
            GB / ↓{" "}
            {Math.round(((site.download_bytes ?? 0) / 1073741824) * 100) / 100}{" "}
            GB
          </div>
        )}
        {site.priority !== 50 && <div>优先级：{site.priority}</div>}
      </div>

      {/* 操作按钮 */}
      <div className="flex items-center gap-2 pt-1">
        <button
          onClick={() => onTest()}
          disabled={testing}
          className="flex-1 rounded-lg border border-gray-200 px-2 py-1.5 text-xs text-ink-100 hover:bg-gray-50 disabled:opacity-50 flex items-center justify-center gap-1 transition"
        >
          {testing ? (
            <>
              <RefreshCw size={12} className="animate-spin" />
              测试中...
            </>
          ) : (
            <>
              <Wifi size={12} />
              测试连接
            </>
          )}
        </button>
        <button
          onClick={() => onEdit()}
          className="rounded-lg border border-gray-200 p-1.5 text-ink-50 hover:text-white hover:bg-gray-50 transition"
          title="编辑"
        >
          <Edit3 size={14} />
        </button>
        <button
          onClick={() => onDelete()}
          className="rounded-lg border border-gray-200 p-1.5 text-ink-50 hover:text-red-400 hover:bg-red-400/10 transition"
          title="删除"
        >
          <Trash2 size={14} />
        </button>
      </div>
    </div>
  );
}
