<template>
  <div>
    <div class="page-head">
      <div>
        <h1>平台</h1>
        <p>每个平台独立调度。Cron 按香港时间解释，留空则为每天 08:00 增量。</p>
      </div>
      <el-button type="primary" @click="openCreate">新建平台</el-button>
    </div>
    <el-alert v-if="error" :title="error" type="error" show-icon :closable="false" style="margin-bottom: 12px" />
    <div class="panel" v-loading="loading">
      <el-empty v-if="!loading && rows.length === 0" description="还没有平台。" />
      <el-table v-else :data="rows">
        <el-table-column prop="name" label="名称" min-width="120" />
        <el-table-column prop="code" label="代码" width="130" />
        <el-table-column label="适配器" width="110">
          <template #default="{ row }">
            <el-tag v-if="row.supported" type="success">已内置</el-tag>
            <el-tag v-else type="info">未内置</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="定时" min-width="150">
          <template #default="{ row }">{{ row.cron_expr }} · {{ row.enabled ? "启用" : "停用" }}</template>
        </el-table-column>
        <el-table-column label="下次" min-width="150">
          <template #default="{ row }">{{ formatHKT(row.next_run_at) }}</template>
        </el-table-column>
        <el-table-column label="上次成功推进" min-width="150">
          <template #default="{ row }">{{ formatHKT(row.last_synced_at) }}</template>
        </el-table-column>
        <el-table-column label="操作" min-width="280" fixed="right">
          <template #default="{ row }">
            <el-button size="small" @click="sync(row, 'incremental')">增量</el-button>
            <el-button size="small" @click="sync(row, 'full')">全量</el-button>
            <el-button size="small" @click="openEdit(row)">编辑</el-button>
            <el-button size="small" type="danger" plain @click="remove(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
    </div>

    <el-dialog v-model="dialog" :title="editing ? '编辑平台' : '新建平台'" width="640px">
      <el-form label-position="top">
        <el-form-item label="名称"><el-input v-model="form.name" /></el-form-item>
        <el-form-item label="代码"><el-input v-model="form.code" placeholder="例如 huangguoai" /></el-form-item>
        <el-form-item label="源站地址"><el-input v-model="form.base_url" placeholder="https://example.com" /></el-form-item>
        <el-form-item label="站点地图路径"><el-input v-model="form.sitemap_path" placeholder="/sitemap.xml" /></el-form-item>
        <p class="note" style="margin: -8px 0 12px">
          一般是 <code>/sitemap.xml</code>。不确定就先打开 <code>https://站点/robots.txt</code>，看里面的
          <code>Sitemap:</code> 行；也可以直接在浏览器访问 <code>https://站点/sitemap.xml</code>。
        </p>
        <el-form-item label="Cron（香港时间）"><el-input v-model="form.cron_expr" placeholder="0 8 * * *" /></el-form-item>
        <el-form-item label="参与定时同步"><el-switch v-model="form.enabled" /></el-form-item>

        <el-divider content-position="left">登录配置（下载时使用，可选）</el-divider>
        <el-alert
          type="info"
          :closable="false"
          show-icon
          title="需要 VIP/登录的站点才填。支持自动填表登录，或直接贴 Cookie；密码加密保存，接口不回传。"
          style="margin-bottom: 12px"
        />
        <el-form-item label="登录页地址">
          <el-input v-model="form.login_url" placeholder="https://example.com/login" />
        </el-form-item>
        <el-form-item label="登录账号">
          <el-input v-model="form.login_username" placeholder="手机号 / 邮箱 / 用户名" />
        </el-form-item>
        <el-form-item label="登录密码">
          <el-input
            v-model="form.login_password"
            type="password"
            show-password
            :placeholder="form.has_login_password ? '已保存，留空表示不修改' : '留空表示不使用自动登录'"
          />
        </el-form-item>
        <el-form-item v-if="form.has_login_password">
          <el-checkbox v-model="form.clear_login_password">清除已保存的登录密码</el-checkbox>
        </el-form-item>
        <el-form-item label="用户名选择器（可选）">
          <el-input v-model="form.login_user_selector" placeholder="input[name=username]" />
        </el-form-item>
        <el-form-item label="密码选择器（可选）">
          <el-input v-model="form.login_pass_selector" placeholder="input[type=password]" />
        </el-form-item>
        <el-form-item label="提交按钮选择器（可选）">
          <el-input v-model="form.login_submit_selector" placeholder="button[type=submit]" />
        </el-form-item>
        <el-form-item label="Cookie（可选，优先于自动登录）">
          <el-input
            v-model="form.cookie"
            type="textarea"
            :rows="2"
            :placeholder="form.has_cookie ? '已保存，留空表示不修改' : 'vip=1; token=abc123'"
          />
        </el-form-item>
        <el-form-item v-if="form.has_cookie">
          <el-checkbox v-model="form.clear_cookie">清除已保存的 Cookie</el-checkbox>
        </el-form-item>
        <el-form-item label="浏览器 profile 目录（可选）">
          <el-input v-model="form.browser_profile" placeholder="留空用默认：<下载目录>/.browser-profile/<平台代码>" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialog = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="save">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from "vue";
import { useRouter } from "vue-router";
import { ElMessage, ElMessageBox } from "element-plus";
import { api, errorText } from "../api/client";
import type { Platform } from "../api/types";
import { formatHKT } from "../utils/format";

const router = useRouter();
const rows = ref<Platform[]>([]);
const loading = ref(true);
const error = ref("");
const dialog = ref(false);
const saving = ref(false);
const editing = ref<number | null>(null);
const form = reactive({
  name: "",
  code: "",
  base_url: "",
  sitemap_path: "/sitemap.xml",
  cron_expr: "0 8 * * *",
  enabled: true,
  login_url: "",
  login_username: "",
  login_password: "",
  login_user_selector: "",
  login_pass_selector: "",
  login_submit_selector: "",
  browser_profile: "",
  cookie: "",
  clear_login_password: false,
  clear_cookie: false,
  has_login_password: false,
  has_cookie: false,
});

async function load() {
  loading.value = true;
  error.value = "";
  try {
    rows.value = await api.platforms();
  } catch (err) {
    error.value = errorText(err);
  } finally {
    loading.value = false;
  }
}

function openCreate() {
  editing.value = null;
  Object.assign(form, {
    name: "", code: "", base_url: "", sitemap_path: "/sitemap.xml", cron_expr: "0 8 * * *", enabled: true,
    login_url: "", login_username: "", login_password: "", login_user_selector: "", login_pass_selector: "",
    login_submit_selector: "", browser_profile: "", cookie: "",
    clear_login_password: false, clear_cookie: false, has_login_password: false, has_cookie: false,
  });
  dialog.value = true;
}

function openEdit(row: Platform) {
  editing.value = row.id;
  Object.assign(form, {
    name: row.name, code: row.code, base_url: row.base_url, sitemap_path: row.sitemap_path,
    cron_expr: row.cron_expr, enabled: row.enabled,
    login_url: row.login_url || "", login_username: row.login_username || "",
    login_user_selector: row.login_user_selector || "", login_pass_selector: row.login_pass_selector || "",
    login_submit_selector: row.login_submit_selector || "", browser_profile: row.browser_profile || "",
    login_password: "", cookie: "", clear_login_password: false, clear_cookie: false,
    has_login_password: row.has_login_password, has_cookie: row.has_cookie,
  });
  dialog.value = true;
}

async function save() {
  saving.value = true;
  try {
    await api.savePlatform({
      name: form.name,
      code: form.code,
      base_url: form.base_url,
      sitemap_path: form.sitemap_path,
      cron_expr: form.cron_expr,
      enabled: form.enabled,
      login_url: form.login_url,
      login_username: form.login_username,
      login_password: form.login_password,
      login_user_selector: form.login_user_selector,
      login_pass_selector: form.login_pass_selector,
      login_submit_selector: form.login_submit_selector,
      browser_profile: form.browser_profile,
      cookie: form.cookie,
      clear_login_password: form.clear_login_password,
      clear_cookie: form.clear_cookie,
    }, editing.value || undefined);
    dialog.value = false;
    ElMessage.success("已保存");
    await load();
  } catch (err) {
    ElMessage.error(errorText(err));
  } finally {
    saving.value = false;
  }
}

async function sync(row: Platform, mode: "full" | "incremental") {
  if (mode === "full") {
    try {
      await ElMessageBox.confirm(`对「${row.name}」执行全量同步？已有进行中的任务会被记为跳过。`, "全量同步", { type: "warning" });
    } catch {
      return;
    }
  }
  try {
    const job = await api.syncPlatform(row.id, mode);
    ElMessage.success(job.status === "skipped" ? job.skip_reason : "已开始同步");
    await router.push(`/jobs/${job.id}`);
  } catch (err) {
    ElMessage.error(errorText(err));
  }
}

async function remove(row: Platform) {
  try {
    await ElMessageBox.confirm(`删除平台「${row.name}」及其作品、任务记录？`, "删除平台", { type: "warning" });
  } catch {
    return;
  }
  try {
    await api.deletePlatform(row.id);
    ElMessage.success("已删除");
    await load();
  } catch (err) {
    ElMessage.error(errorText(err));
  }
}

onMounted(load);
</script>
