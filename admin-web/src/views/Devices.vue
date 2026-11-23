<script setup lang="ts">
import { onMounted, ref } from "vue";
import { api } from "../api";

type Device = { id: string; name: string; os: string; arch: string; trust_state: string; online: boolean; fingerprint: string };
const devices = ref<Device[]>([]);
const error = ref("");

async function load() {
  try {
    const data = await api<{ devices: Device[] }>("/api/v1/admin/devices");
    devices.value = data.devices || [];
  } catch (err) {
    error.value = err instanceof Error ? err.message : "无法读取设备";
  }
}

async function revoke(id: string) {
  if (!confirm("撤销后这台设备不能再建立新会话。")) return;
  await api(`/api/v1/admin/devices/${id}/revoke`, { method: "POST" });
  await load();
}

onMounted(load);
</script>

<template>
  <section>
    <h1>设备</h1>
    <p class="muted">列表只显示登记信息。批准设备要在已信任的客户端上核对指纹，管理页不能代替这个动作。</p>
    <p v-if="error" class="error">{{ error }}</p>
    <table>
      <thead><tr><th>名称</th><th>系统</th><th>状态</th><th>指纹</th><th></th></tr></thead>
      <tbody>
        <tr v-for="device in devices" :key="device.id">
          <td>{{ device.name }}</td>
          <td>{{ device.os }} / {{ device.arch }}</td>
          <td>{{ device.trust_state }}<span v-if="device.online"> · 在线</span></td>
          <td><code>{{ device.fingerprint.slice(0, 16) }}</code></td>
          <td><button class="quiet" type="button" @click="revoke(device.id)" :disabled="device.trust_state === 'revoked'">撤销</button></td>
        </tr>
      </tbody>
    </table>
  </section>
</template>
