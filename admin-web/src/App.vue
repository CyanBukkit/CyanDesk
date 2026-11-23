<template>
  <div class="shell">
    <nav>
      <strong>CyanDesk</strong>
      <p class="muted">管理员入口。这里不能观看任何人的屏幕。</p>
      <RouterLink to="/users">账户</RouterLink>
      <RouterLink to="/devices">设备</RouterLink>
      <RouterLink to="/sessions">会话</RouterLink>
      <RouterLink to="/releases">版本与下载</RouterLink>
      <RouterLink to="/audit">审计</RouterLink>
      <RouterLink to="/login">登录</RouterLink>
    </nav>
    <main>
      <RouterView />
    </main>
  </div>
</template>
