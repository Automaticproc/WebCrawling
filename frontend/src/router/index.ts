import { createRouter, createWebHistory } from "vue-router";
import { api } from "../api/client";

export const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: "/login", component: () => import("../views/Login.vue"), meta: { public: true } },
    {
      path: "/",
      component: () => import("../layouts/AdminLayout.vue"),
      children: [
        { path: "", component: () => import("../views/Dashboard.vue") },
        { path: "platforms", component: () => import("../views/Platforms.vue") },
        { path: "works", component: () => import("../views/Works.vue") },
        { path: "works/:id", component: () => import("../views/WorkDetail.vue") },
        { path: "jobs", component: () => import("../views/Jobs.vue") },
        { path: "jobs/:id", component: () => import("../views/JobDetail.vue") },
        { path: "downloads", component: () => import("../views/Downloads.vue") },
        { path: "rules", component: () => import("../views/SkipRules.vue") },
      ],
    },
  ],
});

router.beforeEach(async (to) => {
  if (to.meta.public) {
    if (to.path === "/login" && (await api.me())) return { path: "/" };
    return true;
  }
  const username = await api.me();
  if (!username) return { path: "/login", query: { redirect: to.fullPath } };
  return true;
});
