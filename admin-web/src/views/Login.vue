<script setup lang="ts">
import { onMounted, ref } from "vue";
import { useRouter } from "vue-router";
import { api } from "../api";

const router = useRouter();
const bootstrapped = ref(true);
const error = ref("");
const loginName = ref("");
const password = ref("");
const token = ref("");

onMounted(async () => {
  const status = await api<{ bootstrapped: boolean }>("/api/v1/setup-status");
  bootstrapped.value = status.bootstrapped;
});

async function submit() {
  error.value = "";
  try {
    if (!bootstrapped.value) {
      await api("/api/v1/admin/bootstrap", {
        method: "POST",
        body: JSON.stringify({ token: token.value, login_name: loginName.value, password: password.value }),
      });
      bootstrapped.value = true;
      token.value = "";
    }
    await api("/api/v1/auth/login", {
      method: "POST",
      body: JSON.stringify({ login_name: loginName.value, password: password.value }),
    });
    password.value = "";
    await router.push("/users");
  } catch (err) {
    error.value = err instanceof Error ? err.message : "登录没有完成";
  }
}
</script>

<template>
  <section class="card" style="max-width: 440px">
    <h1>{{ bootstrapped ? "管理员登录" : "初始化管理员" }}</h1>
    <p class="muted">没有默认密码。引导凭据在服务器的 bootstrap.token 文件里，不会写进日志。</p>
    <form @submit.prevent="submit">
      <label v-if="!bootstrapped">引导凭据<input v-model="token" autocomplete="off" /></label>
      <label>账号<input v-model="loginName" autocomplete="username" /></label>
      <label>密码<input v-model="password" type="password" autocomplete="current-password" /></label>
      <p v-if="error" class="error">{{ error }}</p>
      <button type="submit">{{ bootstrapped ? "登录" : "创建并登录" }}</button>
    </form>
  </section>
</template>

<style scoped>
form { display: grid; gap: 0.7rem; }
</style>
