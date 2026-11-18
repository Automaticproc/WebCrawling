import dayjs from "dayjs";
import utc from "dayjs/plugin/utc";
import timezone from "dayjs/plugin/timezone";

dayjs.extend(utc);
dayjs.extend(timezone);

const zone = "Asia/Hong_Kong";

export function formatHKT(value?: string | null) {
  if (!value) return "—";
  const parsed = dayjs(value);
  if (!parsed.isValid()) return "—";
  return parsed.tz(zone).format("YYYY-MM-DD HH:mm");
}

export function nowHKT() {
  return dayjs().tz(zone).format("YYYY-MM-DD HH:mm:ss");
}

export function formatCount(value?: number) {
  return new Intl.NumberFormat("zh-Hans").format(value || 0);
}

export const modeLabel: Record<string, string> = {
  full: "全量",
  incremental: "增量",
};

export const triggerLabel: Record<string, string> = {
  manual: "手动",
  schedule: "定时",
};

export const statusLabel: Record<string, string> = {
  running: "进行中",
  succeeded: "成功",
  failed: "失败",
  skipped: "已跳过",
};

export const actionLabel: Record<string, string> = {
  created: "新增",
  updated: "更新",
  skipped: "跳过",
  failed: "失败",
  absent: "缺席",
};

export const targetLabel: Record<string, string> = {
  title: "标题",
  tags: "标签",
  description: "简介",
  all: "全部字段",
};

export const ruleTypeLabel: Record<string, string> = {
  builtin: "内置",
  keyword: "关键词",
  regex: "正则",
};

export function statusType(status: string) {
  if (status === "succeeded") return "success";
  if (status === "failed") return "danger";
  if (status === "skipped") return "warning";
  return "info";
}
