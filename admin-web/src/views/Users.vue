<script setup lang="ts">
import { onMounted, ref } from "vue";
import { api } from "../api";

type User = { id: string; login_name: string; role: string; status: string; created_at: string };
const users = ref<User[]>([]);
const error = ref("");
const loginName = ref("");
const password = ref("");
const role = ref("user");

async function load() {
  error.value = "";
  try {
    const data = await api<{ users: User[] }>("/api/v1/admin/users");
    users.value = data.users || [];
  } catch (err) {
    error.value = err instanceof Error ? err.message : "无法读取账户";
  }
}

async function createUser() {
  error.value = "";
  try {
    await api("/api/v1/admin/users", {
      method: "POST",
      body: JSON.stringify({ login_name: loginName.value, password: password.value, role: role.value }),
    });
    password.value = "";
    loginName.value = "";
    await load();
  } catch (err) {
    error.value = err instanceof Error ? err.message : "没有创建账户";
  }
}

async function setStatus(id: string, status: string) {
  error.value = "";
  try {
    await api(`/api/v1/admin/users/${id}/status`, { method: "POST", body: JSON.stringify({ status }) });
    await load();
  } catch (err) {
    error.value = err instanceof Error ? err.message : "没有更新状态";
  }
}

onMounted(load);
</script>

<template>
  <section>
    <h1>账户</h1>
    <p class="muted">管理员可以创建和停用账户，但不能因此获得远控权限。</p>
    <p v-if="error" class="error">{{ error }}</p>
    <form class="card row" @submit.prevent="createUser">
      <label>账号<input v-model="loginName" /></label>
      <label>初始密码<input v-model="password" type="password" /></label>
      <label>角色
        <select v-model="role">
          <option value="user">用户</option>
          <option value="admin">管理员</option>
        </select>
      </label>
      <button type="submit">创建</button>
    </form>
    <table>
      <thead><tr><th>账号</th><th>角色</th><th>状态</th><th></th></tr></thead>
      <tbody>
        <tr v-for="user in users" :key="user.id">
          <td>{{ user.login_name }}</td>
          <td>{{ user.role === "admin" ? "管理员" : "用户" }}</td>
          <td>{{ user.status === "active" ? "可用" : "已停用" }}</td>
          <td>
            <button class="quiet" type="button" @click="setStatus(user.id, user.status === 'active' ? 'disabled' : 'active')">
              {{ user.status === "active" ? "停用" : "恢复" }}
            </button>
          </td>
        </tr>
      </tbody>
    </table>
  </section>
</template>
