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

    <el-dialog v-model="dialog" :title="editing ? '编辑平台' : '新建平台'" width="520px">
      <el-form label-position="top">
        <el-form-item label="名称"><el-input v-model="form.name" /></el-form-item>
        <el-form-item label="代码"><el-input v-model="form.code" placeholder="例如 huangguoai" /></el-form-item>
        <el-form-item label="源站地址"><el-input v-model="form.base_url" placeholder="https://example.com" /></el-form-item>
        <el-form-item label="站点地图路径"><el-input v-model="form.sitemap_path" placeholder="/sitemap.xml" /></el-form-item>
        <el-form-item label="Cron（香港时间）"><el-input v-model="form.cron_expr" placeholder="0 8 * * *" /></el-form-item>
        <el-form-item label="参与定时同步"><el-switch v-model="form.enabled" /></el-form-item>
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
  Object.assign(form, { name: "", code: "", base_url: "", sitemap_path: "/sitemap.xml", cron_expr: "0 8 * * *", enabled: true });
  dialog.value = true;
}

function openEdit(row: Platform) {
  editing.value = row.id;
  Object.assign(form, row);
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
