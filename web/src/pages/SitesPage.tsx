import { type FormEvent, useEffect, useState } from "react";
import toast from "react-hot-toast";
import { Globe, Plus, RefreshCw } from "lucide-react";

import { sitesAPI } from "../api/sites";
import type { Site } from "../types";
import { ManagementShortcuts } from "../components/ManagementShortcuts";
import { confirmAction } from "../components/ConfirmDialog";

import {
  defaultSiteForm,
  siteFormToPayload,
  siteToForm,
} from "./sitesPageModel";
import { SiteCard } from "./SiteCard";
import { SiteFormModal } from "./SiteFormModal";

export function SitesPage() {
  const [sites, setSites] = useState<Site[]>([]);
  const [loading, setLoading] = useState(true);
  const [showModal, setShowModal] = useState(false);
  const [editingId, setEditingId] = useState<string | null>(null);
  const [form, setForm] = useState(defaultSiteForm());
  const [saving, setSaving] = useState(false);
  const [testingId, setTestingId] = useState<string | null>(null);
  const [advancedOpen, setAdvancedOpen] = useState(false);

  const loadSites = async () => {
    setLoading(true);
    try {
      const response = await sitesAPI.list();
      setSites(Array.isArray(response.data) ? response.data : []);
    } catch {
      toast.error("加载站点列表失败");
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    loadSites();
  }, []);

  const openCreate = () => {
    setEditingId(null);
    setForm(defaultSiteForm());
    setAdvancedOpen(false);
    setShowModal(true);
  };

  const openEdit = async (id: string) => {
    try {
      const response = await sitesAPI.get(id);
      setEditingId(id);
      setForm(siteToForm(response.data as Site));
      setAdvancedOpen(false);
      setShowModal(true);
    } catch {
      toast.error("获取站点详情失败");
    }
  };

  const closeModal = () => {
    setShowModal(false);
    setEditingId(null);
  };

  const silentSave = async (): Promise<boolean> => {
    if (!form.name.trim() || !form.url.trim()) return false;
    const payload = siteFormToPayload(form, !editingId);
    try {
      if (editingId) {
        await sitesAPI.update(editingId, payload);
      } else {
        const response = await sitesAPI.create(payload);
        setEditingId((response.data as Site)?.id ?? null);
      }
      return true;
    } catch {
      return false;
    }
  };

  const handleSave = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    setSaving(true);
    const ok = await silentSave();
    if (ok) {
      toast.success(editingId ? "站点已更新" : "站点已添加");
      closeModal();
      await loadSites();
    } else {
      toast.error("保存失败");
    }
    setSaving(false);
  };

  const handleTest = async (id: string) => {
    if (editingId === id) {
      setSaving(true);
      const ok = await silentSave();
      setSaving(false);
      if (!ok) {
        toast.error("保存失败，无法测试");
        return;
      }
    }

    setTestingId(id);
    try {
      const response = await sitesAPI.test(id);
      toast.success(response?.message || "连接测试成功");
    } catch (err: unknown) {
      const message =
        (err as { response?: { data?: { message?: string } } })?.response?.data
          ?.message ?? "连接测试失败";
      toast.error(message);
    } finally {
      setTestingId(null);
    }
  };

  const handleDelete = async (site: Site) => {
    const confirmed = await confirmAction({
      title: "删除站点",
      message: `确定要删除站点「${site.name}」吗？此操作不可撤销。`,
      confirmText: "删除",
    });
    if (!confirmed) return;

    try {
      await sitesAPI.remove(site.id);
      toast.success("站点已删除");
      await loadSites();
    } catch {
      toast.error("删除站点失败");
    }
  };

  const handleTypeChange = (type: string) => {
    setForm((current) => ({
      ...current,
      type,
      auth_type: type === "mteam" ? "api_key" : current.auth_type,
    }));
  };

  return (
    <div className="space-y-6">
      <ManagementShortcuts
        title="站点与下载链路"
        description="把站点、搜索、订阅和下载器放在同一工作流里，避免功能入口被隐藏。"
        items={[
          {
            to: "/download-clients",
            title: "下载器管理",
            description: "添加、测试和维护下载器连接",
            badge: "必需",
          },
          {
            to: "/site-search",
            title: "站点检索",
            description: "跨 PT 站点搜索资源并创建下载任务",
          },
          {
            to: "/subscriptions",
            title: "订阅管理",
            description: "管理追剧追番和自动下载规则",
          },
          {
            to: "/downloads",
            title: "下载中心",
            description: "查看下载任务状态和历史记录",
          },
        ]}
      />

      <div className="flex items-center justify-between">
        <h1 className="font-display text-3xl font-bold text-ink-600">
          站点管理
        </h1>
        <button
          onClick={openCreate}
          className="neon-button flex items-center gap-2"
        >
          <Plus size={16} />
          添加站点
        </button>
      </div>

      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
        {sites.map((site) => (
          <SiteCard
            key={site.id}
            site={site}
            testing={testingId === site.id}
            onTest={() => handleTest(site.id)}
            onEdit={() => openEdit(site.id)}
            onDelete={() => handleDelete(site)}
          />
        ))}

        {!loading && sites.length === 0 && (
          <div className="col-span-full py-12 text-center text-ink-50">
            <Globe size={40} className="mx-auto mb-3 text-gray-500" />
            <p>暂无站点</p>
            <p className="text-sm mt-1 text-sand-500">
              点击「添加站点」添加 PT/BT 站点
            </p>
          </div>
        )}

        {loading && (
          <div className="col-span-full py-12 text-center text-ink-50">
            <RefreshCw size={24} className="mx-auto mb-3 animate-spin" />
            <p>加载中...</p>
          </div>
        )}
      </div>

      {showModal && (
        <SiteFormModal
          editingId={editingId}
          form={form}
          setForm={setForm}
          saving={saving}
          advancedOpen={advancedOpen}
          setAdvancedOpen={setAdvancedOpen}
          onClose={closeModal}
          onSubmit={handleSave}
          onTypeChange={handleTypeChange}
        />
      )}
    </div>
  );
}
