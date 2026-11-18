<template>
  <div>
    <div class="page-head">
      <div>
        <h1>作品</h1>
        <p>互动数据只保留最近一次快照。简介只在通过跳过规则后才会出现在这里。</p>
      </div>
    </div>
    <div class="filters">
      <el-select v-model="platformId" clearable placeholder="全部平台" style="width: 180px" @change="reload">
        <el-option v-for="p in platforms" :key="p.id" :label="p.name" :value="p.id" />
      </el-select>
      <el-input v-model="q" placeholder="搜索标题或外部编号" style="width: 240px" clearable @keyup.enter="reload" />
      <el-button @click="reload">搜索</el-button>
    </div>
    <el-alert v-if="error" :title="error" type="error" show-icon :closable="false" style="margin-bottom: 12px" />
    <div class="panel" v-loading="loading">
      <el-empty v-if="!loading && rows.length === 0" description="目录里还没有作品。完成一次同步后会出现在这里。" />
      <el-table v-else :data="rows" @row-click="open">
        <el-table-column prop="title" label="标题" min-width="180" />
        <el-table-column label="平台" width="120">
          <template #default="{ row }">{{ nameOf(row.platform_id) }}</template>
        </el-table-column>
        <el-table-column label="播放" width="100"><template #default="{ row }">{{ formatCount(row.view_count) }}</template></el-table-column>
        <el-table-column label="点赞" width="90"><template #default="{ row }">{{ formatCount(row.like_count) }}</template></el-table-column>
        <el-table-column label="收藏" width="90"><template #default="{ row }">{{ formatCount(row.favorite_count) }}</template></el-table-column>
        <el-table-column label="评论" width="80"><template #default="{ row }">{{ formatCount(row.comment_count) }}</template></el-table-column>
        <el-table-column prop="episode_count" label="集数" width="70" />
        <el-table-column prop="absent_count" label="缺席" width="70" />
        <el-table-column label="最近见到" min-width="140"><template #default="{ row }">{{ formatHKT(row.last_seen_at) }}</template></el-table-column>
      </el-table>
      <div class="pager">
        <el-pagination
          background
          layout="prev, pager, next, total"
          :total="total"
          :page-size="pageSize"
          :current-page="page"
          @current-change="changePage"
        />
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from "vue";
import { useRouter } from "vue-router";
import { api, errorText } from "../api/client";
import type { Platform, Work } from "../api/types";
import { formatCount, formatHKT } from "../utils/format";

const router = useRouter();
const platforms = ref<Platform[]>([]);
const rows = ref<Work[]>([]);
const platformId = ref<number | undefined>();
const q = ref("");
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
    const data = await api.works({
      page: page.value,
      page_size: pageSize,
      platform_id: platformId.value,
      q: q.value.trim() || undefined,
    });
    rows.value = data.items;
    total.value = data.total;
  } catch (err) {
    error.value = errorText(err);
  } finally {
    loading.value = false;
  }
}

function reload() {
  page.value = 1;
  load();
}

function changePage(next: number) {
  page.value = next;
  load();
}

function open(row: Work) {
  router.push(`/works/${row.id}`);
}

onMounted(async () => {
  try {
    platforms.value = await api.platforms();
  } catch (err) {
    error.value = errorText(err);
  }
  await load();
});
</script>
