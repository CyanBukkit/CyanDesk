<script setup lang="ts">
import { onMounted, ref } from "vue";
import { api } from "../api";

type Event = { id: string; action: string; result: string; timestamp: string; actor_id: string };
const events = ref<Event[]>([]);
const error = ref("");

onMounted(async () => {
  try {
    const data = await api<{ events: Event[] }>("/api/v1/admin/audit?limit=100");
    events.value = data.events || [];
  } catch (err) {
    error.value = err instanceof Error ? err.message : "无法读取审计";
  }
});
</script>

<template>
  <section>
    <h1>审计</h1>
    <p class="muted">记录登录、授权和发布结果。密码、令牌和私钥不会出现在这里。</p>
    <p v-if="error" class="error">{{ error }}</p>
    <table>
      <thead><tr><th>时间</th><th>动作</th><th>结果</th></tr></thead>
      <tbody>
        <tr v-for="event in events" :key="event.id">
          <td>{{ event.timestamp }}</td>
          <td>{{ event.action }}</td>
          <td>{{ event.result }}</td>
        </tr>
      </tbody>
    </table>
  </section>
</template>
