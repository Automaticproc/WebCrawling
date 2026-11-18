<template>
  <div>
    <div class="page-head">
      <div>
        <h1>概览</h1>
        <p>默认定时是每天 08:00（香港时间）增量同步。同一平台已有任务进行时，新的同步会记为跳过。</p>
      </div>
    </div>
    <el-alert v-if="error" :title="error" type="error" show-icon :closable="false" style="margin-bottom: 12px" />
    <div v-loading="loading" class="grid">
      <article class="stat"><em>平台</em><strong>{{ platforms.length }}</strong></article>
      <article class="stat"><em>作品</em><strong>{{ workTotal }}</strong></article>
      <article class="stat"><em>最近任务</em><strong>{{ jobs.length }}</strong></article>
      <article class="stat"><em>下一次增量</em><strong style="font-size: 18px">{{ nextLabel }}</strong></article>
    </div>
    <div class="panel" style="margin-top: 16px">
      <h2 style="font-size: 16px">最近同步</h2>
      <el-empty v-if="!loading && jobs.length === 0" description="还没有同步任务。到平台页可以手动发起增量或全量。" />
      <el-table v-else :data="jobs" @row-click="openJob">
        <el-table-column label="平台" min-width="120">
          <template #default="{ row }">{{ platformName(row.platform_id) }}</template>
        </el-table-column>
        <el-table-column label="方式" width="90">
          <template #default="{ row }">{{ modeLabel[row.mode] }}</template>
        </el-table-column>
        <el-table-column label="状态" width="100">
          <template #default="{ row }"><el-tag :type="statusType(row.status)">{{ statusLabel[row.status] }}</el-tag></template>
        </el-table-column>
        <el-table-column label="开始" min-width="150">
          <template #default="{ row }">{{ formatHKT(row.started_at) }}</template>
        </el-table-column>
      </el-table>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { useRouter } from "vue-router";
import { api, errorText } from "../api/client";
import type { Platform, SyncJob } from "../api/types";
import { formatHKT, modeLabel, statusLabel, statusType } from "../utils/format";

const router = useRouter();
const loading = ref(true);
const error = ref("");
const platforms = ref<Platform[]>([]);
const jobs = ref<SyncJob[]>([]);
const workTotal = ref(0);

const nextLabel = computed(() => {
  const times = platforms.value.map((p) => p.next_run_at).filter((v): v is string => Boolean(v)).sort();
  return times[0] ? formatHKT(times[0]) : "—";
});

function platformName(id: number) {
  return platforms.value.find((p) => p.id === id)?.name || `#${id}`;
}

function openJob(row: SyncJob) {
  router.push(`/jobs/${row.id}`);
}

onMounted(async () => {
  try {
    const [p, j, w] = await Promise.all([
      api.platforms(),
      api.jobs({ page: 1, page_size: 8 }),
      api.works({ page: 1, page_size: 1 }),
    ]);
    platforms.value = p;
    jobs.value = j.items;
    workTotal.value = w.total;
  } catch (err) {
    error.value = errorText(err);
  } finally {
    loading.value = false;
  }
});
</script>
