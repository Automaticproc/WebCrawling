<template>
  <div v-loading="loading">
    <el-alert v-if="error" :title="error" type="error" show-icon :closable="false" />
    <template v-if="work">
      <div class="page-head">
        <div>
          <h1>{{ work.title }}</h1>
          <p>{{ work.category || "未分类" }} · {{ work.author || "未知作者" }} · 上线 {{ formatHKT(work.published_at) }}</p>
        </div>
        <el-button tag="a" :href="work.play_url" target="_blank" rel="noreferrer noopener">打开作品页</el-button>
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
import { onMounted, ref } from "vue";
import { useRoute } from "vue-router";
import { api, errorText } from "../api/client";
import type { Work } from "../api/types";
import { formatCount, formatHKT } from "../utils/format";

const route = useRoute();
const work = ref<Work | null>(null);
const loading = ref(true);
const error = ref("");

onMounted(async () => {
  try {
    work.value = await api.work(Number(route.params.id));
  } catch (err) {
    error.value = errorText(err);
  } finally {
    loading.value = false;
  }
});
</script>
