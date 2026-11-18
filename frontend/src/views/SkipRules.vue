<template>
  <div>
    <div class="page-head">
      <div>
        <h1>跳过规则</h1>
        <p>内置的未成年与未满 21 岁规则始终生效，不能关闭或删除。命中后只保留标题和原因，不保存简介。</p>
      </div>
      <el-button type="primary" @click="openCreate">新建规则</el-button>
    </div>
    <el-alert v-if="error" :title="error" type="error" show-icon :closable="false" style="margin-bottom: 12px" />
    <div class="panel" v-loading="loading">
      <el-empty v-if="!loading && rows.length === 0" description="还没有规则。" />
      <el-table v-else :data="rows">
        <el-table-column prop="name" label="名称" min-width="160" />
        <el-table-column label="类型" width="90"><template #default="{ row }">{{ ruleTypeLabel[row.rule_type] }}</template></el-table-column>
        <el-table-column label="范围" width="100"><template #default="{ row }">{{ targetLabel[row.target] }}</template></el-table-column>
        <el-table-column prop="pattern" label="模式" min-width="180" />
        <el-table-column prop="reason" label="原因" min-width="180" />
        <el-table-column label="启用" width="90">
          <template #default="{ row }">
            <span v-if="row.locked">生效中</span>
            <el-switch v-else :model-value="row.enabled" @change="(v) => toggle(row, Boolean(v))" />
          </template>
        </el-table-column>
        <el-table-column label="操作" width="160">
          <template #default="{ row }">
            <el-tag v-if="row.locked" type="warning">不可关闭</el-tag>
            <template v-else>
              <el-button size="small" @click="openEdit(row)">编辑</el-button>
              <el-button size="small" type="danger" plain @click="remove(row)">删除</el-button>
            </template>
          </template>
        </el-table-column>
      </el-table>
    </div>
    <el-dialog v-model="dialog" :title="editing ? '编辑规则' : '新建规则'" width="520px">
      <el-form label-position="top">
        <el-form-item label="名称"><el-input v-model="form.name" /></el-form-item>
        <el-form-item label="类型">
          <el-select v-model="form.rule_type" style="width: 100%">
            <el-option label="关键词" value="keyword" />
            <el-option label="正则" value="regex" />
          </el-select>
        </el-form-item>
        <el-form-item label="范围">
          <el-select v-model="form.target" style="width: 100%">
            <el-option label="标题" value="title" />
            <el-option label="标签" value="tags" />
            <el-option label="简介" value="description" />
            <el-option label="全部字段" value="all" />
          </el-select>
        </el-form-item>
        <el-form-item label="模式"><el-input v-model="form.pattern" /></el-form-item>
        <el-form-item label="原因"><el-input v-model="form.reason" /></el-form-item>
        <el-form-item label="优先级"><el-input-number v-model="form.priority" :min="1" :max="1000" /></el-form-item>
        <el-form-item label="启用"><el-switch v-model="form.enabled" /></el-form-item>
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
import { ElMessage, ElMessageBox } from "element-plus";
import { api, errorText } from "../api/client";
import type { SkipRule } from "../api/types";
import { ruleTypeLabel, targetLabel } from "../utils/format";

const rows = ref<SkipRule[]>([]);
const loading = ref(true);
const error = ref("");
const dialog = ref(false);
const saving = ref(false);
const editing = ref<number | null>(null);
const form = reactive({
  name: "",
  rule_type: "keyword" as "keyword" | "regex",
  target: "all" as SkipRule["target"],
  pattern: "",
  reason: "",
  priority: 100,
  enabled: true,
});

async function load() {
  loading.value = true;
  error.value = "";
  try {
    rows.value = await api.rules();
  } catch (err) {
    error.value = errorText(err);
  } finally {
    loading.value = false;
  }
}

function openCreate() {
  editing.value = null;
  Object.assign(form, { name: "", rule_type: "keyword", target: "all", pattern: "", reason: "", priority: 100, enabled: true });
  dialog.value = true;
}

function openEdit(row: SkipRule) {
  editing.value = row.id;
  Object.assign(form, row);
  dialog.value = true;
}

async function save() {
  saving.value = true;
  try {
    await api.saveRule({
      name: form.name,
      rule_type: form.rule_type,
      target: form.target,
      pattern: form.pattern,
      reason: form.reason,
      priority: form.priority,
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

async function toggle(row: SkipRule, enabled: boolean) {
  if (row.locked) return;
  try {
    await api.saveRule({
      name: row.name,
      rule_type: row.rule_type,
      target: row.target,
      pattern: row.pattern,
      reason: row.reason,
      priority: row.priority,
      enabled,
    }, row.id);
    await load();
  } catch (err) {
    ElMessage.error(errorText(err));
  }
}

async function remove(row: SkipRule) {
  try {
    await ElMessageBox.confirm(`删除规则「${row.name}」？`, "删除规则", { type: "warning" });
  } catch {
    return;
  }
  try {
    await api.deleteRule(row.id);
    ElMessage.success("已删除");
    await load();
  } catch (err) {
    ElMessage.error(errorText(err));
  }
}

onMounted(load);
</script>
