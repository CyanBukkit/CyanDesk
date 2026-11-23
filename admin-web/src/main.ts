import { createApp } from "vue";
import { createRouter, createWebHashHistory } from "vue-router";
import App from "./App.vue";
import "./style.css";
import Audit from "./views/Audit.vue";
import Devices from "./views/Devices.vue";
import Login from "./views/Login.vue";
import Releases from "./views/Releases.vue";
import Sessions from "./views/Sessions.vue";
import Users from "./views/Users.vue";

const router = createRouter({
  history: createWebHashHistory(),
  routes: [
    { path: "/", redirect: "/users" },
    { path: "/login", component: Login },
    { path: "/users", component: Users },
    { path: "/devices", component: Devices },
    { path: "/sessions", component: Sessions },
    { path: "/releases", component: Releases },
    { path: "/audit", component: Audit },
  ],
});

createApp(App).use(router).mount("#app");
