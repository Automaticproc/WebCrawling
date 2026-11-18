<template>
  <div class="shell">
    <aside class="sidebar" :class="{ open: open }">
      <div class="brand">
        <strong>目录采集台</strong>
        <span>本地作品目录 · 香港时间</span>
      </div>
      <el-menu
        :default-active="active"
        router
        background-color="transparent"
        text-color="#f3e7dc"
        active-text-color="#ffffff"
        @select="open = false"
      >
        <el-menu-item index="/">概览</el-menu-item>
        <el-menu-item index="/platforms">平台</el-menu-item>
        <el-menu-item index="/works">作品</el-menu-item>
        <el-menu-item index="/jobs">同步任务</el-menu-item>
        <el-menu-item index="/rules">跳过规则</el-menu-item>
      </el-menu>
      <p class="note" style="margin-top: auto; color: #c8b4a6">
        播放链接只保存官方作品页。系统不采集视频文件。
      </p>
    </aside>
    <section class="main">
      <header class="topbar">
        <el-button class="menu-btn" text @click="open = !open">菜单</el-button>
        <div class="clock">香港时间 {{ clock }}</div>
        <div>
          <span style="margin-right: 10px">{{ username }}</span>
          <el-button size="small" @click="logout">退出</el-button>
        </div>
      </header>
      <div class="content">
        <router-view />
      </div>
    </section>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from "vue";
import { useRoute, useRouter } from "vue-router";
import { api } from "../api/client";
import { nowHKT } from "../utils/format";

const route = useRoute();
const router = useRouter();
const active = computed(() => {
  if (route.path.startsWith("/works")) return "/works";
  if (route.path.startsWith("/jobs")) return "/jobs";
  return route.path || "/";
});
const open = ref(false);
const username = ref("");
const clock = ref(nowHKT());
let timer = 0;

onMounted(async () => {
  username.value = (await api.me()) || "";
  timer = window.setInterval(() => {
    clock.value = nowHKT();
  }, 1000);
});

onUnmounted(() => window.clearInterval(timer));

async function logout() {
  await api.logout();
  await router.push("/login");
}
</script>
