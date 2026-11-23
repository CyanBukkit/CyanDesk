<script setup lang="ts">
import { onMounted, ref } from "vue";
import { api } from "../api";

type Release = { artifact_id: string; version: string; os: string; arch: string; status: string; sha256: string; signature: string };
const releases = ref<Release[]>([]);
const error = ref("");
const version = ref("0.1.5");
const channel = ref("stable");
const osName = ref("linux");
const arch = ref("amd64");
const status = ref("private");
const file = ref<File | null>(null);

async function load() {
  try {
    const data = await api<{ releases: Release[] }>("/api/v1/admin/releases");
    releases.value = data.releases || [];
  } catch (err) {
    error.value = err instanceof Error ? err.message : "无法读取版本";
  }
}

async function upload() {
  if (!file.value) {
    error.value = "先选择已经构建好的文件";
    return;
  }
  const body = new FormData();
  body.set("version", version.value);
  body.set("channel", channel.value);
  body.set("min_safe_version", "0.1.0");
  body.set("engine_version", "linux-ffmpeg");
  body.set("os", osName.value);
  body.set("arch", arch.value);
  body.set("status", status.value);
  body.set("file", file.value);
  error.value = "";
  try {
    await api("/api/v1/admin/releases", { method: "POST", body });
    await load();
  } catch (err) {
    error.value = err instanceof Error ? err.message : "没有登记文件";
  }
}

onMounted(load);
</script>

<template>
  <section>
    <h1>版本与下载</h1>
    <p class="muted">这里只登记已经生成的文件并计算校验和与签名。它不会在服务器上执行构建命令。</p>
    <p v-if="error" class="error">{{ error }}</p>
    <form class="card row" @submit.prevent="upload">
      <label>版本<input v-model="version" /></label>
      <label>渠道<input v-model="channel" /></label>
      <label>系统<input v-model="osName" /></label>
      <label>架构<input v-model="arch" /></label>
      <label>可见性
        <select v-model="status">
          <option value="private">登录后下载</option>
          <option value="public">公开下载</option>
        </select>
      </label>
      <label>文件<input type="file" @change="file = ($event.target as HTMLInputElement).files?.[0] || null" /></label>
      <button type="submit">登记</button>
    </form>
    <table>
      <thead><tr><th>版本</th><th>平台</th><th>状态</th><th>SHA256</th></tr></thead>
      <tbody>
        <tr v-for="item in releases" :key="item.artifact_id">
          <td>{{ item.version }}</td>
          <td>{{ item.os }}/{{ item.arch }}</td>
          <td>{{ item.status === "public" ? "公开" : "需登录" }}</td>
          <td><code>{{ item.sha256.slice(0, 16) }}</code></td>
        </tr>
      </tbody>
    </table>
  </section>
</template>
