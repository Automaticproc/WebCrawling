<template>
  <div v-loading="loading">
    <el-alert v-if="error" :title="error" type="error" show-icon :closable="false" />
    <template v-if="job">
      <div class="page-head">
        <div>
          <h1>任务 #{{ job.id }}</h1>
          <p>{{ modeLabel[job.mode] }} · {{ triggerLabel[job.trigger_type] }} · {{ formatHKT(job.started_at || job.created_at) }}</p>
        </div>
        <el-tag :type="statusType(job.status)" size="large">{{ statusLabel[job.status] }}</el-tag>
      </div>
      <el-alert v-if="job.skip_reason" :title="job.skip_reason" type="warning" show-icon :closable="false" style="margin-bottom: 12px" />
      <el-alert v-if="job.error_message" :title="job.error_message" type="error" show-icon :closable="false" style="margin-bottom: 12px" />
      <div class="grid" style="margin-bottom: 16px">
        <article class="stat"><em>发现</em><strong>{{ job.discovered_count }}</strong></article>
        <article class="stat"><em>新增 / 更新</em><strong>{{ job.created_count }} / {{ job.updated_count }}</strong></article>
        <article class="stat"><em>跳过 / 失败</em><strong>{{ job.skipped_count }} / {{ job.failed_count }}</strong></article>
        <article class="stat"><em>缺席</em><strong>{{ job.absent_count }}</strong></article>
      </div>
      <div class="panel">
        <el-empty v-if="items.length === 0" description="这条任务没有明细。" />
        <el-table v-else :data="items">
          <el-table-column prop="item_title" label="标题" min-width="180" />
          <el-table-column prop="external_id" label="编号" width="100" />
          <el-table-column label="结果" width="90"><template #default="{ row }">{{ actionLabel[row.action] }}</template></el-table-column>
          <el-table-column prop="reason" label="原因" min-width="220" />
          <el-table-column label="官方页面" min-width="180">
            <template #default="{ row }">
              <a v-if="row.play_url" :href="row.play_url" target="_blank" rel="noreferrer noopener">打开</a>
              <span v-else>—</span>
            </template>
          </el-table-column>
        </el-table>
        <div class="pager">
          <el-pagination background layout="prev, pager, next, total" :total="total" :page-size="pageSize" :current-page="page" @current-change="changePage" />
        </div>
      </div>
    </template>
  </div>
</template>

<script setup lang="ts">
import { onMounted, onUnmounted, ref } from "vue";
import { useRoute } from "vue-router";
import { api, errorText } from "../api/client";
import type { SyncJob, SyncJobItem } from "../api/types";
import { actionLabel, formatHKT, modeLabel, statusLabel, statusType, triggerLabel } from "../utils/format";

const route = useRoute();
const job = ref<SyncJob | null>(null);
const items = ref<SyncJobItem[]>([]);
const page = ref(1);
const pageSize = 20;
const total = ref(0);
const loading = ref(true);
const error = ref("");
let timer = 0;

async function load(silent = false) {
  if (!silent) loading.value = true;
  error.value = "";
  try {
    const id = Number(route.params.id);
    job.value = await api.job(id);
    const pageData = await api.jobItems(id, page.value, pageSize);
    items.value = pageData.items;
    total.value = pageData.total;
  } catch (err) {
    error.value = errorText(err);
  } finally {
    loading.value = false;
  }
}

function changePage(n: number) {
  page.value = n;
  load();
}

onMounted(async () => {
  await load();
  timer = window.setInterval(() => {
    if (job.value?.status === "running") load(true);
  }, 2000);
});
onUnmounted(() => window.clearInterval(timer));
</script>
