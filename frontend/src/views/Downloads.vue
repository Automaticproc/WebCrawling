<template>
  <div>
    <div class="page-head">
      <div>
        <h1>下载任务</h1>
        <p>按作品逐集抓取播放地址并合成为 MP4。只下载你确认有权保存的内容。</p>
      </div>
      <el-button @click="load" :loading="loading">刷新</el-button>
    </div>
    <el-alert v-if="error" :title="error" type="error" show-icon :closable="false" style="margin-bottom: 12px" />
    <div class="panel" v-loading="loading">
      <el-empty v-if="!loading && rows.length === 0" description="还没有下载任务。" />
      <el-table v-else :data="rows" row-key="id">
        <el-table-column type="expand">
          <template #default="{ row }">
            <div style="padding: 8px 16px">
              <el-table :data="row.items" size="small">
                <el-table-column prop="episode_no" label="集" width="70" />
                <el-table-column prop="title" label="标题" min-width="120" />
                <el-table-column label="状态" width="100">
                  <template #default="{ row: it }"><el-tag size="small" :type="itemTag(it.status)">{{ itemLabel[it.status] }}</el-tag></template>
                </el-table-column>
                <el-table-column label="大小" width="100">
                  <template #default="{ row: it }">{{ it.size_bytes ? formatSize(it.size_bytes) : "-" }}</template>
                </el-table-column>
                <el-table-column label="时长" width="90">
                  <template #default="{ row: it }">{{ it.duration_seconds ? Math.round(it.duration_seconds) + "s" : "-" }}</template>
                </el-table-column>
                <el-table-column label="文件" min-width="120">
                  <template #default="{ row: it }">
                    <a v-if="it.file_url" :href="it.file_url">下载 MP4</a>
                    <span v-else class="note">{{ it.error_message || "-" }}</span>
                  </template>
                </el-table-column>
              </el-table>
            </div>
          </template>
        </el-table-column>
        <el-table-column prop="id" label="#" width="70" />
        <el-table-column label="作品" width="100">
          <template #default="{ row }"><router-link :to="`/works/${row.work_id}`">#{{ row.work_id }}</router-link></template>
        </el-table-column>
        <el-table-column label="状态" width="110">
          <template #default="{ row }"><el-tag :type="taskTag(row.status)">{{ taskLabel[row.status] }}</el-tag></template>
        </el-table-column>
        <el-table-column label="进度" min-width="200">
          <template #default="{ row }">
            <el-progress :percentage="percentOf(row)" :status="row.status === 'failed' ? 'exception' : undefined" />
            <span class="note">{{ row.completed_episodes }}/{{ row.total_episodes }} 集 · {{ row.quality }}</span>
          </template>
        </el-table-column>
        <el-table-column label="当前集" width="160">
          <template #default="{ row }">
            <span v-if="row.progress">第 {{ row.progress.episode_no }} 集 · {{ row.progress.done }}/{{ row.progress.total }} 片</span>
            <span v-else class="note">-</span>
          </template>
        </el-table-column>
        <el-table-column label="创建" width="150">
          <template #default="{ row }">{{ formatHKT(row.created_at) }}</template>
        </el-table-column>
        <el-table-column label="操作" width="170">
          <template #default="{ row }">
            <el-button v-if="row.status === 'running' || row.status === 'queued'" size="small" @click="cancel(row)">取消</el-button>
            <el-button v-else-if="row.status === 'failed' || row.status === 'canceled'" size="small" type="primary" @click="retry(row)">重试失败集</el-button>
          </template>
        </el-table-column>
      </el-table>
      <div class="pager">
        <el-pagination background layout="prev, pager, next, total" :total="total" :page-size="pageSize" :current-page="page" @current-change="changePage" />
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { onMounted, onUnmounted, ref } from "vue";
import { api, errorText } from "../api/client";
import type { DownloadTask } from "../api/types";
import { formatHKT } from "../utils/format";

const rows = ref<DownloadTask[]>([]);
const page = ref(1);
const pageSize = 20;
const total = ref(0);
const loading = ref(false);
const error = ref("");
let timer = 0;

const taskLabel: Record<string, string> = {
  queued: "排队中", running: "下载中", succeeded: "已完成", failed: "失败", canceled: "已取消",
};
const itemLabel: Record<string, string> = {
  queued: "排队中", running: "下载中", succeeded: "完成", failed: "失败", canceled: "取消",
};

function taskTag(s: string) {
  return s === "succeeded" ? "success" : s === "failed" ? "danger" : s === "running" ? "warning" : "info";
}
function itemTag(s: string) {
  return s === "succeeded" ? "success" : s === "failed" ? "danger" : "info";
}
function percentOf(t: DownloadTask) {
  if (!t.total_episodes) return 0;
  if (t.status === "succeeded") return 100;
  return Math.min(99, Math.round((t.completed_episodes / t.total_episodes) * 100));
}
function formatSize(n: number) {
  if (n > 1024 * 1024 * 1024) return (n / 1024 / 1024 / 1024).toFixed(1) + " GB";
  if (n > 1024 * 1024) return (n / 1024 / 1024).toFixed(1) + " MB";
  return Math.round(n / 1024) + " KB";
}

async function load() {
  loading.value = true;
  error.value = "";
  try {
    const data = await api.downloads({ page: page.value, page_size: pageSize });
    rows.value = data.items;
    total.value = data.total;
  } catch (err) {
    error.value = errorText(err);
  } finally {
    loading.value = false;
  }
}

async function cancel(row: DownloadTask) {
  try {
    await api.cancelDownload(row.id);
    await load();
  } catch (err) {
    error.value = errorText(err);
  }
}

async function retry(row: DownloadTask) {
  try {
    await api.retryDownload(row.id);
    await load();
  } catch (err) {
    error.value = errorText(err);
  }
}

function changePage(p: number) {
  page.value = p;
  load();
}

onMounted(async () => {
  await load();
  timer = window.setInterval(async () => {
    const active = rows.value.some((r) => r.status === "running" || r.status === "queued");
    if (!active) return;
    try {
      const data = await api.downloads({ page: page.value, page_size: pageSize });
      rows.value = data.items;
      total.value = data.total;
    } catch {
      /* 轮询失败时保留旧数据 */
    }
  }, 2000);
});
onUnmounted(() => window.clearInterval(timer));
</script>
