import type { Dispatch, FormEvent, SetStateAction } from "react";
import { RefreshCw, X } from "lucide-react";

import type { SiteForm } from "./sitesPageModel";

type SiteFormModalProps = {
  editingId: string | null;
  form: SiteForm;
  setForm: Dispatch<SetStateAction<SiteForm>>;
  saving: boolean;
  advancedOpen: boolean;
  setAdvancedOpen: Dispatch<SetStateAction<boolean>>;
  onClose: () => void;
  onSubmit: (event: FormEvent<HTMLFormElement>) => void | Promise<void>;
  onTypeChange: (type: string) => void;
};

export function SiteFormModal({
  editingId,
  form,
  setForm,
  saving,
  advancedOpen,
  setAdvancedOpen,
  onClose,
  onSubmit,
  onTypeChange,
}: SiteFormModalProps) {
  return (
    <div
      className="fixed inset-0 z-50 flex items-start justify-center pt-[10vh] bg-black/60 backdrop-blur-sm"
      onClick={onClose}
    >
      <div
        className="glass-panel w-full max-w-xl max-h-[75vh] overflow-y-auto mx-4 space-y-5"
        onClick={(event) => event.stopPropagation()}
      >
        <div className="flex items-center justify-between">
          <h2 className="text-lg font-bold text-ink-600">
            {editingId ? "编辑站点" : "添加站点"}
          </h2>
          <button
            onClick={onClose}
            className="text-ink-50 hover:text-white transition"
          >
            <X size={20} />
          </button>
        </div>

        <form onSubmit={onSubmit} className="space-y-4">
          <div>
            <label className="block text-sm text-ink-50 mb-1.5">
              站点名称 *
            </label>
            <input
              required
              className="input-base w-full"
              placeholder="例如: 馒头、观众、家园"
              value={form.name}
              onChange={(event) =>
                setForm((current) => ({ ...current, name: event.target.value }))
              }
            />
          </div>

          <div>
            <label className="block text-sm text-ink-50 mb-1.5">
              站点地址 *
            </label>
            <input
              required
              className="input-base w-full"
              placeholder="https://www.example.com/"
              value={form.url}
              onChange={(event) =>
                setForm((current) => ({ ...current, url: event.target.value }))
              }
            />
            <p className="text-xs text-sand-500 mt-1">
              格式: https://www.example.com/
            </p>
          </div>

          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className="block text-sm text-ink-50 mb-1.5">
                站点类型
              </label>
              <select
                className="input-base w-full"
                value={form.type}
                onChange={(event) => onTypeChange(event.target.value)}
              >
                <option value="nexusphp">NexusPHP（国内主流PT）</option>
                <option value="gazelle">Gazelle（HDBits等）</option>
                <option value="unit3d">UNIT3D（BeyondHD等）</option>
                <option value="mteam">馒头 M-Team（专用API）</option>
                <option value="discuz">Discuz 论坛型</option>
                <option value="custom_rss">自定义 RSS</option>
              </select>
            </div>
            <div>
              <label className="block text-sm text-ink-50 mb-1.5">状态</label>
              <div className="flex items-center gap-3 h-10">
                <button
                  type="button"
                  onClick={() =>
                    setForm((current) => ({
                      ...current,
                      enabled: !current.enabled,
                    }))
                  }
                  className={`relative inline-flex h-5 w-9 shrink-0 rounded-full transition-colors cursor-pointer ${form.enabled ? "bg-primary-500" : "bg-gray-200"}`}
                >
                  <span
                    className={`pointer-events-none inline-block h-4 w-4 rounded-full bg-white shadow transform transition-transform mt-0.5 ${form.enabled ? "translate-x-4" : "translate-x-0.5"}`}
                  />
                </button>
                <span
                  className={`text-sm ${form.enabled ? "text-white" : "text-sand-500"}`}
                >
                  {form.enabled ? "启用" : "停用"}
                </span>
              </div>
            </div>
          </div>

          {form.type === "mteam" && (
            <div className="p-3 rounded-xl border border-green-500/30 bg-green-500/5">
              <div className="text-sm font-medium text-green-400 mb-1">
                馒头站点配置指南
              </div>
              <div className="text-xs text-ink-50 space-y-1">
                <div>
                  <b>正式站地址：</b>
                  <code className="text-green-300">https://api.m-team.cc</code>
                </div>
                <div>
                  <b>测试站地址：</b>
                  <code className="text-green-300">
                    https://test2.m-team.cc
                  </code>
                  （开发测试环境）
                </div>
                <div>
                  <b>认证方式：</b>必须使用「API Access Token」，不要使用 Cookie
                </div>
                <div className="pl-3 text-sand-500">
                  1. 登录馒头站 → 控制台 → 实验室 → 存取令牌
                  <br />
                  2. 点击「创建令牌」，复制生成的 Token
                  <br />
                  3. 将 Token 填入下方「令牌」输入框
                </div>
              </div>
            </div>
          )}

          <div>
            <label className="block text-sm text-ink-50 mb-2">认证方式</label>
            <div className="flex gap-2 mb-3">
              {[
                { value: "cookie", label: "Cookie" },
                { value: "api_key", label: "API Key" },
                { value: "auth_header", label: "Auth Header" },
              ].map((option) => (
                <button
                  key={option.value}
                  type="button"
                  onClick={() =>
                    setForm((current) => ({
                      ...current,
                      auth_type: option.value,
                    }))
                  }
                  className={`px-3 py-1.5 rounded-xl text-xs font-medium border transition ${
                    form.auth_type === option.value
                      ? "bg-primary-500 text-ink-600 border-primary-500"
                      : "border-gray-200 text-ink-50 hover:border-primary-500/50"
                  }`}
                >
                  {option.label}
                </button>
              ))}
            </div>

            {form.auth_type === "cookie" && (
              <div>
                <label className="block text-xs text-ink-50 mb-1">Cookie</label>
                <textarea
                  rows={3}
                  className="input-base w-full resize-none text-xs font-mono"
                  placeholder="uid=xxx; pass=xxx; ..."
                  value={form.cookie}
                  onChange={(event) =>
                    setForm((current) => ({
                      ...current,
                      cookie: event.target.value,
                    }))
                  }
                />
                <p className="text-xs text-sand-500 mt-1">
                  从浏览器开发者工具的请求头中获取 Cookie 值
                </p>
              </div>
            )}

            {form.auth_type === "api_key" && (
              <div>
                <label className="block text-xs text-ink-50 mb-1">
                  令牌（API Key / Passkey）
                </label>
                <input
                  type="password"
                  className="input-base w-full font-mono text-sm"
                  placeholder={
                    editingId
                      ? "留空则保留原令牌，输入新值则替换"
                      : "输入 API Access Token"
                  }
                  value={form.api_key}
                  onChange={(event) =>
                    setForm((current) => ({
                      ...current,
                      api_key: event.target.value,
                    }))
                  }
                />
                <p className="text-xs text-sand-500 mt-1">
                  {form.type === "mteam"
                    ? "馒头：控制台 → 实验室 → 存取令牌；第三方工具通过 x-api-key 请求头访问"
                    : "站点的访问 API Key"}
                </p>
              </div>
            )}

            {form.auth_type === "auth_header" && (
              <div>
                <label className="block text-xs text-ink-50 mb-1">
                  请求头（Authorization）
                </label>
                <input
                  className="input-base w-full font-mono text-xs"
                  placeholder="Bearer eyJhbGciOiJIUzI1NiIs..."
                  value={form.auth_header}
                  onChange={(event) =>
                    setForm((current) => ({
                      ...current,
                      auth_header: event.target.value,
                    }))
                  }
                />
              </div>
            )}

            <div className="mt-4">
              <label className="block text-xs text-ink-50 mb-1">RSS 地址</label>
              <input
                className="input-base w-full text-xs font-mono"
                placeholder="https://.../torrents/rss?..."
                value={form.rss_url}
                onChange={(event) =>
                  setForm((current) => ({
                    ...current,
                    rss_url: event.target.value,
                  }))
                }
              />
              <p className="text-xs text-sand-500 mt-1">
                站点 RSS 订阅地址，用于获取最新资源
              </p>
            </div>
          </div>

          <div>
            <button
              type="button"
              onClick={() => setAdvancedOpen(!advancedOpen)}
              className="flex items-center gap-1 text-xs text-ink-50 hover:text-white transition"
            >
              {advancedOpen ? "▾" : "▸"} 高级选项
            </button>
            {advancedOpen && (
              <div className="mt-3 pl-4 space-y-3 border-l border-gray-200">
                <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
                  <div>
                    <label className="block text-xs text-ink-50 mb-1">
                      User-Agent
                    </label>
                    <input
                      className="input-base w-full text-xs"
                      placeholder="自定义 UA，留空使用默认"
                      value={form.user_agent}
                      onChange={(event) =>
                        setForm((current) => ({
                          ...current,
                          user_agent: event.target.value,
                        }))
                      }
                    />
                  </div>
                  <div>
                    <label className="block text-xs text-ink-50 mb-1">
                      请求超时 (秒)
                    </label>
                    <input
                      type="number"
                      className="input-base w-full"
                      min={1}
                      max={300}
                      value={form.timeout}
                      onChange={(event) =>
                        setForm((current) => ({
                          ...current,
                          timeout: Number(event.target.value),
                        }))
                      }
                    />
                  </div>
                  <div>
                    <label className="block text-xs text-ink-50 mb-1">
                      优先级 (数字越大越优先)
                    </label>
                    <input
                      type="number"
                      className="input-base w-full"
                      min={1}
                      max={100}
                      value={form.priority}
                      onChange={(event) =>
                        setForm((current) => ({
                          ...current,
                          priority: Number(event.target.value),
                        }))
                      }
                    />
                  </div>
                  <div>
                    <label className="block text-xs text-ink-50 mb-1">
                      关联下载器
                    </label>
                    <input
                      className="input-base w-full text-xs"
                      placeholder="下载器 ID 或名称"
                      value={form.downloader}
                      onChange={(event) =>
                        setForm((current) => ({
                          ...current,
                          downloader: event.target.value,
                        }))
                      }
                    />
                  </div>
                </div>

                <div className="flex flex-wrap gap-4">
                  <label className="flex items-center gap-2 cursor-pointer">
                    <input
                      type="checkbox"
                      className="h-4 w-4 accent-primary-400"
                      checked={form.use_proxy}
                      onChange={(event) =>
                        setForm((current) => ({
                          ...current,
                          use_proxy: event.target.checked,
                        }))
                      }
                    />
                    <span className="text-xs text-ink-100">使用代理</span>
                  </label>
                  <label className="flex items-center gap-2 cursor-pointer">
                    <input
                      type="checkbox"
                      className="h-4 w-4 accent-primary-400"
                      checked={form.rate_limit}
                      onChange={(event) =>
                        setForm((current) => ({
                          ...current,
                          rate_limit: event.target.checked,
                        }))
                      }
                    />
                    <span className="text-xs text-ink-100">启用限流</span>
                  </label>
                  <label className="flex items-center gap-2 cursor-pointer">
                    <input
                      type="checkbox"
                      className="h-4 w-4 accent-primary-400"
                      checked={form.browser_emulation}
                      onChange={(event) =>
                        setForm((current) => ({
                          ...current,
                          browser_emulation: event.target.checked,
                        }))
                      }
                    />
                    <span className="text-xs text-ink-100">浏览器模拟</span>
                  </label>
                </div>

                <div>
                  <label className="block text-xs text-ink-50 mb-1">
                    Extra 扩展配置 (JSON)
                  </label>
                  <textarea
                    rows={3}
                    className="input-base w-full resize-none text-xs font-mono"
                    placeholder='{"key":"value"}'
                    value={form.extra}
                    onChange={(event) =>
                      setForm((current) => ({
                        ...current,
                        extra: event.target.value,
                      }))
                    }
                  />
                </div>
                <div className="flex items-center gap-3">
                  <button
                    type="button"
                    onClick={() =>
                      setForm((current) => ({
                        ...current,
                        is_default: !current.is_default,
                      }))
                    }
                    className={`relative inline-flex h-5 w-9 shrink-0 rounded-full transition-colors cursor-pointer ${form.is_default ? "bg-primary-500" : "bg-gray-200"}`}
                  >
                    <span
                      className={`pointer-events-none inline-block h-4 w-4 rounded-full bg-white shadow transform transition-transform mt-0.5 ${form.is_default ? "translate-x-4" : "translate-x-0.5"}`}
                    />
                  </button>
                  <span className="text-sm text-ink-50">设为默认站点</span>
                </div>
              </div>
            )}
          </div>

          <div className="flex justify-end gap-2 pt-2">
            <button
              type="button"
              onClick={onClose}
              className="rounded-lg border border-gray-200 px-4 py-2 text-sm text-ink-100 hover:bg-gray-50 transition"
            >
              取消
            </button>
            <button
              type="submit"
              disabled={saving || !form.name.trim() || !form.url.trim()}
              className="neon-button text-sm disabled:opacity-50 flex items-center gap-1.5"
            >
              {saving ? (
                <>
                  <RefreshCw size={14} className="animate-spin" />
                  保存中...
                </>
              ) : (
                "保存"
              )}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}
