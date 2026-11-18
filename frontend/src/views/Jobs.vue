<template>
  <div>
    <div class="page-head">
      <div>
        <h1>同步任务</h1>
        <p>跳过的条目会留下标题和原因。命中未成年规则时不会保存简介。</p>
      </div>
    </div>
    <div class="filters">
      <el-select v-model="platformId" clearable placeholder="全部平台" style="width: 180px" @change="reload">
        <el-option v-for="p in platforms" :key="p.id" :label="p.name" :value="p.id" />
      </el-select>
    </div>
    <el-alert v-if="error" :title="error" type="error" show-icon :closable="false" style="margin-bottom: 12px" />
    <div class="panel" v-loading="loading">
      <el-empty v-if="!loading && rows.length === 0" description="还没有任务。" />
      <el-table v-else :data="rows" @row-click="open">
        <el-table-column label="平台" width="120"><template #default="{ row }">{{ nameOf(row.platform_id) }}</template></el-table-column>
        <el-table-column label="模式" width="80"><template #default="{ row }">{{ modeLabel[row.mode] }}</template></el-table-column>
        <el-table-column label="触发" width="80"><template #default="{ row }">{{ triggerLabel[row.trigger_type] }}</template></el-table-column>
        <el-table-column label="状态" width="100"><template #default="{ row }"><el-tag :type="statusType(row.status)">{{ statusLabel[row.status] }}</el-tag></template></el-table-column>
        <el-table-column label="发现/新增/更新/跳过/失败/缺席" min-width="220">
          <template #default="{ row }">{{ row.discovered_count }}/{{ row.created_count }}/{{ row.updated_count }}/{{ row.skipped_count }}/{{ row.failed_count }}/{{ row.absent_count }}</template>
        </el-table-column>
        <el-table-column label="开始" min-width="150"><template #default="{ row }">{{ formatHKT(row.started_at || row.created_at) }}</template></el-table-column>
      </el-table>
      <div class="pager">
        <el-pagination background layout="prev, pager, next, total" :total="total" :page-size="pageSize" :current-page="page" @current-change="changePage" />
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from "vue";
import { useRouter } from "vue-router";
import { api, errorText } from "../api/client";
import type { Platform, SyncJob } from "../api/types";
import { formatHKT, modeLabel, statusLabel, statusType, triggerLabel } from "../utils/format";

const router = useRouter();
const platforms = ref<Platform[]>([]);
const rows = ref<SyncJob[]>([]);
const platformId = ref<number | undefined>();
const page = ref(1);
const pageSize = 20;
const total = ref(0);
const loading = ref(true);
const error = ref("");

function nameOf(id: number) {
  return platforms.value.find((p) => p.id === id)?.name || `#${id}`;
}
async function load() {
  loading.value = true;
  error.value = "";
  try {
    const data = await api.jobs({ page: page.value, page_size: pageSize, platform_id: platformId.value });
    rows.value = data.items;
    total.value = data.total;
  } catch (err) {
    error.value = errorText(err);
  } finally {
    loading.value = false;
  }
}
function reload() { page.value = 1; load(); }
function changePage(n: number) { page.value = n; load(); }
function open(row: SyncJob) { router.push(`/jobs/${row.id}`); }
onMounted(async () => {
  try { platforms.value = await api.platforms(); } catch (err) { error.value = errorText(err); }
  await load();
});
</script>
