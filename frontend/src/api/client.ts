import axios from "axios";
import type { Page, Platform, SkipRule, SyncJob, SyncJobItem, Work } from "./types";

export const http = axios.create({
  withCredentials: true,
  headers: { "X-Requested-With": "catalog" },
});

export function errorText(err: unknown): string {
  if (axios.isAxiosError(err)) {
    const msg = err.response?.data?.error;
    if (typeof msg === "string" && msg) return msg;
    if (err.response?.status === 401) return "请先登录";
  }
  return "请求失败，请稍后再试";
}

let cachedUser: string | null | undefined;

export function clearSessionCache() {
  cachedUser = undefined;
}

export const api = {
  async me(): Promise<string | null> {
    if (cachedUser !== undefined) return cachedUser;
    try {
      const { data } = await http.get<{ username: string }>("/api/v1/me");
      cachedUser = data.username;
      return cachedUser;
    } catch {
      cachedUser = null;
      return null;
    }
  },
  async login(username: string, password: string) {
    const { data } = await http.post<{ username: string }>("/api/v1/login", { username, password });
    cachedUser = data.username;
    return data.username;
  },
  async logout() {
    await http.post("/api/v1/logout");
    cachedUser = null;
  },
  async platforms() {
    const { data } = await http.get<{ items: Platform[] }>("/api/v1/platforms");
    return data.items;
  },
  async savePlatform(body: Partial<Platform>, id?: number) {
    if (id) {
      const { data } = await http.put<Platform>(`/api/v1/platforms/${id}`, body);
      return data;
    }
    const { data } = await http.post<Platform>("/api/v1/platforms", body);
    return data;
  },
  async deletePlatform(id: number) {
    await http.delete(`/api/v1/platforms/${id}`);
  },
  async syncPlatform(id: number, mode: "full" | "incremental") {
    const { data } = await http.post<SyncJob>(`/api/v1/platforms/${id}/sync`, { mode });
    return data;
  },
  async works(params: { page: number; page_size: number; platform_id?: number; q?: string }) {
    const { data } = await http.get<Page<Work>>("/api/v1/works", { params });
    return data;
  },
  async work(id: number) {
    const { data } = await http.get<Work>(`/api/v1/works/${id}`);
    return data;
  },
  async jobs(params: { page: number; page_size: number; platform_id?: number }) {
    const { data } = await http.get<Page<SyncJob>>("/api/v1/sync-jobs", { params });
    return data;
  },
  async job(id: number) {
    const { data } = await http.get<SyncJob>(`/api/v1/sync-jobs/${id}`);
    return data;
  },
  async jobItems(id: number, page: number, pageSize = 20) {
    const { data } = await http.get<Page<SyncJobItem>>(`/api/v1/sync-jobs/${id}/items`, {
      params: { page, page_size: pageSize },
    });
    return data;
  },
  async rules() {
    const { data } = await http.get<{ items: SkipRule[] }>("/api/v1/skip-rules");
    return data.items;
  },
  async saveRule(body: Partial<SkipRule>, id?: number) {
    if (id) {
      const { data } = await http.put<SkipRule>(`/api/v1/skip-rules/${id}`, body);
      return data;
    }
    const { data } = await http.post<SkipRule>("/api/v1/skip-rules", body);
    return data;
  },
  async deleteRule(id: number) {
    await http.delete(`/api/v1/skip-rules/${id}`);
  },
};
