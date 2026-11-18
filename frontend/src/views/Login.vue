<template>
  <div class="login-wrap">
    <el-form class="login-card" label-position="top" @submit.prevent="submit">
      <h1>目录采集台</h1>
      <p>登录后管理平台、作品目录和同步任务。时间以香港时间显示。</p>
      <el-form-item label="用户名">
        <el-input v-model="username" autocomplete="username" />
      </el-form-item>
      <el-form-item label="密码">
        <el-input v-model="password" type="password" show-password autocomplete="current-password" @keyup.enter="submit" />
      </el-form-item>
      <el-alert v-if="error" :title="error" type="error" show-icon :closable="false" style="margin-bottom: 12px" />
      <el-button type="primary" :loading="loading" style="width: 100%" @click="submit">登录</el-button>
    </el-form>
  </div>
</template>

<script setup lang="ts">
import { ref } from "vue";
import { useRoute, useRouter } from "vue-router";
import { api, errorText } from "../api/client";

const route = useRoute();
const router = useRouter();
const username = ref("");
const password = ref("");
const loading = ref(false);
const error = ref("");

async function submit() {
  error.value = "";
  loading.value = true;
  try {
    await api.login(username.value.trim(), password.value);
    const redirect = typeof route.query.redirect === "string" ? route.query.redirect : "/";
    await router.push(redirect);
  } catch (err) {
    error.value = errorText(err);
  } finally {
    loading.value = false;
  }
}
</script>
