<template>
  <div v-loading="loading">
    <el-alert v-if="error" :title="error" type="error" show-icon :closable="false" />
    <template v-if="work">
      <div class="page-head">
        <div>
          <h1>{{ work.title }}</h1>
          <p>{{ work.category || "未分类" }} · {{ work.author || "未知作者" }} · 上线 {{ formatHKT(work.published_at) }}</p>
        </div>
        <div style="display: flex; gap: 8px; align-items: center">
          <el-select v-model="quality" style="width: 120px">
            <el-option label="最高画质" value="best" />
            <el-option label="1080p" value="1080p" />
            <el-option label="720p" value="720p" />
            <el-option label="最低画质" value="worst" />
          </el-select>
          <el-button type="primary" :loading="starting" @click="startDownload">下载全部集</el-button>
          <el-button tag="a" :href="work.play_url" target="_blank" rel="noreferrer noopener">打开作品页</el-button>
        </div>
      </div>
      <div class="grid" style="margin-bottom: 16px">
        <article class="stat"><em>播放</em><strong>{{ formatCount(work.view_count) }}</strong></article>
        <article class="stat"><em>点赞</em><strong>{{ formatCount(work.like_count) }}</strong></article>
        <article class="stat"><em>收藏</em><strong>{{ formatCount(work.favorite_count) }}</strong></article>
        <article class="stat"><em>评论</em><strong>{{ formatCount(work.comment_count) }}</strong></article>
      </div>
      <div class="panel" style="margin-bottom: 16px">
        <p v-if="work.tags.length">标签：{{ work.tags.join("、") }}</p>
        <p>{{ work.description || "这条作品没有已保存的简介。" }}</p>
        <p class="note">缺席次数 {{ work.absent_count }}。缺席只在全量同步时累加，再次见到会清零。播放链接是官方页面，不是视频文件。</p>
      </div>

      <div class="panel" style="margin-bottom: 16px" v-if="task">
        <h2 style="font-size: 16px">下载任务 #{{ task.id }}</h2>
        <p>
          <el-tag :type="taskTag(task.status)">{{ taskLabel[task.status] }}</el-tag>
          <span style="margin-left: 10px">{{ task.completed_episodes }}/{{ task.total_episodes }} 集 · {{ task.quality }}</span>
          <span v-if="task.error_message" class="note" style="margin-left: 10px">{{ task.error_message }}</span>
        </p>
        <el-progress :percentage="percentOf(task)" :status="task.status === 'failed' ? 'exception' : undefined" />
        <p v-if="task.progress" class="note">
          正在下载第 {{ task.progress.episode_no }} 集：{{ task.progress.done }}/{{ task.progress.total }} 个分片
        </p>
        <p class="note">输出目录：{{ task.output_dir }}</p>
        <p v-if="task.status === 'failed' || task.status === 'canceled'">
          <el-button size="small" type="primary" :loading="starting" @click="retryTask">重试失败集</el-button>
          <span class="note" style="margin-left: 8px">只重下失败或缺少文件的集，已完成的不会重复下载。</span>
        </p>
        <el-table :data="task.items" size="small" style="margin-top: 8px">
          <el-table-column prop="episode_no" label="集" width="70" />
          <el-table-column label="状态" width="100">
            <template #default="{ row }"><el-tag size="small" :type="row.status === 'succeeded' ? 'success' : row.status === 'failed' ? 'danger' : 'info'">{{ itemLabel[row.status] }}</el-tag></template>
          </el-table-column>
          <el-table-column label="大小" width="100">
            <template #default="{ row }">{{ row.size_bytes ? formatSize(row.size_bytes) : "-" }}</template>
          </el-table-column>
          <el-table-column label="文件" min-width="120">
            <template #default="{ row }">
              <a v-if="row.file_url" :href="row.file_url">下载</a>
              <span v-else class="note">{{ row.error_message || "-" }}</span>
            </template>
          </el-table-column>
        </el-table>
      </div>

      <div class="panel">
        <h2 style="font-size: 16px">分集</h2>
        <el-empty v-if="!(work.episodes || []).length" description="没有分集页面。" />
        <el-table v-else :data="work.episodes">
          <el-table-column prop="episode_no" label="集" width="80" />
          <el-table-column prop="title" label="标题" min-width="140" />
          <el-table-column label="官方页面" min-width="240">
            <template #default="{ row }">
              <a :href="row.play_url" target="_blank" rel="noreferrer noopener">{{ row.play_url }}</a>
            </template>
          </el-table-column>
        </el-table>
      </div>
    </template>
  </div>
</template>

<script setup lang="ts">
import { onMounted, onUnmounted, ref } from "vue";
import { useRoute } from "vue-router";
import { api, errorText } from "../api/client";
import type { DownloadTask, Work } from "../api/types";
import { formatCount, formatHKT } from "../utils/format";

const route = useRoute();
const work = ref<Work | null>(null);
const loading = ref(true);
const error = ref("");
const quality = ref("best");
const starting = ref(false);
const task = ref<DownloadTask | null>(null);
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

async function loadTask() {
  const data = await api.downloads({ page: 1, page_size: 1, work_id: Number(route.params.id) });
  const first = data.items[0];
  if (!first) return;
  task.value = first.status === "running" || first.status === "queued" ? await api.download(first.id) : first;
}

async function startDownload() {
  starting.value = true;
  error.value = "";
  try {
    task.value = await api.startDownload(Number(route.params.id), quality.value);
    poll();
  } catch (err) {
    error.value = errorText(err);
  } finally {
    starting.value = false;
  }
}

async function retryTask() {
  if (!task.value) return;
  starting.value = true;
  error.value = "";
  try {
    task.value = await api.retryDownload(task.value.id);
    poll();
  } catch (err) {
    error.value = errorText(err);
  } finally {
    starting.value = false;
  }
}

function poll() {
  window.clearInterval(timer);
  timer = window.setInterval(async () => {
    if (!task.value) return;
    try {
      task.value = await api.download(task.value.id);
    } catch {
      return;
    }
    if (task.value.status !== "running" && task.value.status !== "queued") {
      window.clearInterval(timer);
    }
  }, 1500);
}

onMounted(async () => {
  try {
    work.value = await api.work(Number(route.params.id));
    await loadTask();
    if (task.value && (task.value.status === "running" || task.value.status === "queued")) poll();
  } catch (err) {
    error.value = errorText(err);
  } finally {
    loading.value = false;
  }
});

onUnmounted(() => window.clearInterval(timer));
</script>
