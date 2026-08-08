import { Globe, Plus, RefreshCw } from "lucide-react";

import { ManagementShortcuts } from "../components/ManagementShortcuts";
import type { Site } from "../types";
import { SiteCard } from "./SiteCard";

export function SitesManagementShortcuts() {
  return (
    <ManagementShortcuts
      title="站点与搜索"
      description="管理站点连接，并在启用的站点中搜索资源。"
      items={[
        {
          to: "/site-search",
          title: "站点检索",
          description: "跨 PT 站点搜索并查看资源详情",
          group: "资源搜索",
        },
      ]}
    />
  );
}

export function SitesPageHeader({ onCreate }: { onCreate: () => void }) {
  return (
    <div className="flex items-center justify-between">
      <h1 className="font-display text-3xl font-bold text-ink-600">
        站点管理
      </h1>
      <button
        onClick={onCreate}
        className="neon-button flex items-center gap-2"
      >
        <Plus size={16} />
        添加站点
      </button>
    </div>
  );
}

export function SitesGrid({
  sites,
  loading,
  testingId,
  onTest,
  onEdit,
  onDelete,
}: {
  sites: Site[];
  loading: boolean;
  testingId: string | null;
  onTest: (id: string) => void;
  onEdit: (id: string) => void;
  onDelete: (site: Site) => void;
}) {
  return (
    <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
      {sites.map((site) => (
        <SiteCard
          key={site.id}
          site={site}
          testing={testingId === site.id}
          onTest={() => onTest(site.id)}
          onEdit={() => onEdit(site.id)}
          onDelete={() => onDelete(site)}
        />
      ))}

      {!loading && sites.length === 0 && <SitesEmptyState />}
      {loading && <SitesLoadingState />}
    </div>
  );
}

function SitesEmptyState() {
  return (
    <div className="col-span-full py-12 text-center text-ink-50">
      <Globe size={40} className="mx-auto mb-3 text-gray-500" />
      <p>暂无站点</p>
      <p className="text-sm mt-1 text-sand-500">
        点击「添加站点」添加 PT/BT 站点
      </p>
    </div>
  );
}

function SitesLoadingState() {
  return (
    <div className="col-span-full py-12 text-center text-ink-50">
      <RefreshCw size={24} className="mx-auto mb-3 animate-spin" />
      <p>加载中...</p>
    </div>
  );
}
