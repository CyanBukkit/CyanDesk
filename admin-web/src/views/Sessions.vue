<script setup lang="ts">
import { onMounted, ref } from "vue";
import { api } from "../api";

type Session = { id: string; host_id: string; controller_id: string; state: string; close_reason: string; ticket?: string };
const sessions = ref<Session[]>([]);
const error = ref("");

async function load() {
  try {
    const data = await api<{ sessions: Session[] }>("/api/v1/admin/sessions");
    sessions.value = data.sessions || [];
  } catch (err) {
    error.value = err instanceof Error ? err.message : "无法读取会话";
  }
}

async function closeSession(id: string) {
  await api(`/api/v1/admin/sessions/${id}`, { method: "DELETE" });
  await load();
}

onMounted(load);
</script>

<template>
  <section>
    <h1>会话</h1>
    <p class="muted">这里只有会话元数据。结束会话不会把管理员变成主控。</p>
    <p v-if="error" class="error">{{ error }}</p>
    <table>
      <thead><tr><th>状态</th><th>主机</th><th>主控</th><th></th></tr></thead>
      <tbody>
        <tr v-for="session in sessions" :key="session.id">
          <td>{{ session.state }}</td>
          <td>{{ session.host_id }}</td>
          <td>{{ session.controller_id }}</td>
          <td><button class="quiet" type="button" @click="closeSession(session.id)" :disabled="session.state === 'closed'">结束</button></td>
        </tr>
      </tbody>
    </table>
  </section>
</template>
