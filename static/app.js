const titles = {
  dashboard: ["概览", "后台数据与快捷操作"],
  users: ["用户", "查看与搜索用户"],
  dynamics: ["动态", "发布和管理动态"],
  friends: ["好友关系", "查看并删除好友关系"],
  applies: ["好友申请", "查看并更新申请状态"],
  star: ["公告", "管理 StarNotice 公告"],
  notices: ["通知投递", "创建供客户端弹窗的后台通知"],
  ai: ["AI 助手", "您的AI聊天助手"],
};

let currentView = localStorage.getItem("mhkh_admin_view") || "dashboard";
let pendingRequests = 0;
let lastAnalyticsData = null;
const PAGE_SIZE = 20;
const pageState = { users: 1, dynamics: 1, friends: 1, applies: 1, star: 1, notices: 1, logs: 1 };
const selectedIds = { users: new Set() };
const roleLabels = { 0: "普通用户", 1: "管理员", 2: "超级管理员" };
const actionLabels = {
  create: "新增",
  update: "编辑",
  delete: "删除",
  reset_password: "重置密码",
  update_status: "修改状态",
  appoint_role: "任命角色",
  approve: "同意",
  reject: "拒绝",
  cancel: "取消处理",
  delivered: "标记已处理",
  login: "登录",
  login_failed: "登录失败",
  login_denied: "登录拒绝",
};
let refreshInFlight = null;
let loginPromptTimer = null;
let noticeTargetOptionsHtml = "";

const $ = (selector) => document.querySelector(selector);

function debounce(fn, delay = 300) {
  let timer = null;
  return (...args) => {
    clearTimeout(timer);
    timer = setTimeout(() => fn(...args), delay);
  };
}

function formatDateTime(value) {
  if (value === null || value === undefined || value === "") return "";
  const raw = String(value).trim();
  if (/T.*(?:Z|(?:\+|-)\d{2}:?\d{2})$/.test(raw)) {
    const date = new Date(raw);
    if (!Number.isNaN(date.getTime())) {
      const parts = Object.fromEntries(new Intl.DateTimeFormat("zh-CN", {
        timeZone: "Asia/Shanghai",
        year: "numeric",
        month: "2-digit",
        day: "2-digit",
        hour: "2-digit",
        minute: "2-digit",
        second: "2-digit",
        hourCycle: "h23",
      }).formatToParts(date).map((p) => [p.type, p.value]));
      return `${parts.year}-${parts.month}-${parts.day} ${parts.hour}:${parts.minute}:${parts.second}`;
    }
  }
  return raw
    .replace("T", " ")
    .replace(/(?:\+|-)\d{2}:?\d{2}$/, "")
    .replace(/\.\d+$/, "")
    .replace(/Z$/, "")
    .slice(0, 19);
}

async function api(path, options = {}) {
  const { skipLoading, headers = {}, ...rest } = options;
  if (!skipLoading) {
    pendingRequests += 1;
    document.body.classList.add("loading");
  }
  try {
    const res = await fetch(path, {
      ...rest,
      headers: { "Content-Type": "application/json", ...headers },
    });
    const data = await res.json().catch(() => ({}));
    if (!res.ok) {
      if (res.status === 401 && path !== "/api/login") scheduleLoginPrompt();
      throw new Error(data.detail || `请求失败: ${res.status}`);
    }
    if (path !== "/api/login") cancelLoginPrompt();
    return data;
  } finally {
    if (!skipLoading) {
      pendingRequests -= 1;
      if (pendingRequests <= 0) document.body.classList.remove("loading");
    }
  }
}

function scheduleLoginPrompt() {
  clearTimeout(loginPromptTimer);
  loginPromptTimer = setTimeout(() => {
    loginPromptTimer = null;
    showLogin();
  }, 350);
}

function cancelLoginPrompt() {
  if (!loginPromptTimer) return;
  clearTimeout(loginPromptTimer);
  loginPromptTimer = null;
}

function toast(message, type = "success") {
  const el = $("#toast");
  const msg = el.querySelector(".toast-msg");
  msg.textContent = type === "loading" ? message : "处理中...";
  el.className = "toast-loading";
  el.style.display = "flex";
  clearTimeout(window.__toastTimer);
  clearTimeout(window.__toastStateTimer);
  if (type === "loading") {
    window.__toastTimer = setTimeout(() => { el.style.display = "none"; }, 8000);
    return;
  }
  window.__toastStateTimer = setTimeout(() => {
    el.className = type === "error" ? "toast-error" : "toast-success";
    msg.textContent = type === "error" ? message : "已完成";
  }, 450);
  window.__toastTimer = setTimeout(() => { el.style.display = "none"; }, 2400);
}

function customConfirm(message, options = {}) {
  return new Promise((resolve) => {
    const overlay = $("#modalOverlay");
    const passwordInput = $("#modalPassword");
    $("#modalText").textContent = message;
    passwordInput.value = "";
    passwordInput.classList.toggle("show", !!options.password);
    passwordInput.required = !!options.password;
    overlay.classList.add("show");
    if (options.password) setTimeout(() => passwordInput.focus(), 80);
    const cleanup = (result) => {
      overlay.classList.remove("show");
      passwordInput.classList.remove("show");
      passwordInput.required = false;
      resolve(result);
    };
    $("#modalConfirm").onclick = () => {
      if (options.password) {
        const code = passwordInput.value.trim();
        if (!code) {
          toast("请输入二级验证密码", "error");
          passwordInput.focus();
          return;
        }
        cleanup(code);
        return;
      }
      cleanup(true);
    };
    $("#modalCancel").onclick = () => cleanup(false);
    overlay.onclick = (e) => { if (e.target === overlay) cleanup(false); };
    passwordInput.onkeydown = (e) => {
      if (e.key === "Enter") {
        e.preventDefault();
        $("#modalConfirm").click();
      }
    };
  });
}

// ---- Modal Form System ----
async function loadNoticeTargetOptions() {
  const rows = await api("/api/users?limit=200");
  noticeTargetOptionsHtml = rows.map((u) => {
    const uid = u.uid ?? "";
    const displayName = u.name || u.nick || "";
    return `<option value="${escapeHtml(uid)}">${escapeHtml(uid)}（${escapeHtml(displayName || "-")}）</option>`;
  }).join("");
}

function noticeTargetSelectHtml() {
  return `<label>目标 UID<select name="target_uid"><option value="">广播（全部用户）</option>${noticeTargetOptionsHtml}</select></label>`;
}

const formTemplates = {
  createUser: {
    title: "创建账号",
    html: `<label>用户名<input name="name" required></label><label>邮箱<input name="email" type="email" required></label><label>初始密码<input name="password" type="password" minlength="6" required></label>`,
  },
  editUser: {
    title: "编辑账号",
    html: `<input name="uid" type="hidden"><label>用户名<input name="name" required></label><label>邮箱<input name="email" type="email" required></label><label>昵称<input name="nick"></label><label>性别<input name="sex"></label><label class="wide">头像<input name="icon"></label><label class="wide">签名<input name="desc"></label>`,
  },
  resetPassword: {
    title: "重置密码",
    html: `<input name="uid" type="hidden"><label class="wide">新密码<input name="password" type="password" minlength="6" required></label>`,
  },
  appointRole: {
    title: "任命角色",
    html: `<input name="uid" type="hidden"><label class="wide">角色<select name="role" required><option value="1">管理员</option></select></label>`,
  },
  createDynamic: {
    title: "发布动态",
    html: `<label>UID<input name="uid" type="number" min="1" required></label><label class="wide">内容<textarea name="content" rows="3" maxlength="2000" required></textarea></label>`,
  },
  editDynamic: {
    title: "编辑动态",
    html: `<input name="id" type="hidden"><label>点赞数<input name="like_count" type="number" min="0" value="0" required></label><label class="wide">内容<textarea name="content" rows="3" maxlength="2000" required></textarea></label>`,
  },
  createStar: {
    title: "新增公告",
    html: `<label>标题<input name="title" maxlength="60" required></label><label>作者<input name="author" maxlength="50" required readonly style="opacity:0.7;cursor:not-allowed"></label><label class="wide">内容<textarea name="content" rows="4"></textarea></label>`,
  },
  editStar: {
    title: "编辑公告",
    html: `<input name="original_title" type="hidden"><input name="original_author" type="hidden"><label>标题<input name="title" maxlength="60" required></label><label>作者<input name="author" maxlength="50" required readonly style="opacity:0.7;cursor:not-allowed"></label><label class="wide">内容<textarea name="content" rows="4"></textarea></label>`,
  },
  createNotice: {
    title: "创建通知",
    html: `__NOTICE_TARGET__<div class="radio-group"><span class="radio-label">等级</span><div class="radio-options"><label class="radio-item"><input type="radio" name="level" value="info" checked><span>INFO</span></label><label class="radio-item"><input type="radio" name="level" value="success"><span>SUCCESS</span></label><label class="radio-item"><input type="radio" name="level" value="warning"><span>WARNING</span></label><label class="radio-item"><input type="radio" name="level" value="error"><span>ERROR</span></label></div></div><label class="wide">标题<input name="title" maxlength="80" required></label><label class="wide">内容<textarea name="content" rows="4" required></textarea></label>`,
  },
  editNotice: {
    title: "编辑通知",
    html: `<input name="id" type="hidden">__NOTICE_TARGET__<div class="radio-group"><span class="radio-label">等级</span><div class="radio-options"><label class="radio-item"><input type="radio" name="level" value="info" checked><span>INFO</span></label><label class="radio-item"><input type="radio" name="level" value="success"><span>SUCCESS</span></label><label class="radio-item"><input type="radio" name="level" value="warning"><span>WARNING</span></label><label class="radio-item"><input type="radio" name="level" value="error"><span>ERROR</span></label></div></div><div class="radio-group"><span class="radio-label">状态</span><div class="radio-options"><label class="radio-item"><input type="radio" name="delivered" value="0" checked><span>未处理</span></label><label class="radio-item"><input type="radio" name="delivered" value="1"><span>已处理</span></label></div></div><label class="wide">标题<input name="title" maxlength="80" required></label><label class="wide">内容<textarea name="content" rows="4" required></textarea></label>`,
  },
};
function openFormModal(type, data = {}) {
  const tpl = formTemplates[type];
  if (!tpl) return;
  $("#formModalTitle").textContent = tpl.title;
  const form = $("#modalForm");
  form.innerHTML = tpl.html.replace("__NOTICE_TARGET__", noticeTargetSelectHtml());
  form.dataset.formType = type;
  for (const [k, v] of Object.entries(data)) {
    const el = form.querySelector(`[name="${k}"]`);
    if (!el) continue;
    if (el.type === "radio") {
      const radio = form.querySelector(`input[name="${k}"][value="${v}"]`);
      if (radio) radio.checked = true;
    } else {
      el.value = v ?? "";
    }
  }
  $("#formModalOverlay").classList.add("show");
  const firstInput = form.querySelector("input:not([type=hidden]):not([readonly]), textarea, select");
  if (firstInput) setTimeout(() => firstInput.focus(), 100);
}

function closeFormModal() {
  $("#formModalOverlay").classList.remove("show");
}

function getFormModalJson() {
  const form = $("#modalForm");
  const data = Object.fromEntries(new FormData(form).entries());
  for (const key of Object.keys(data)) {
    if (data[key] === "") data[key] = null;
  }
  if (data.uid) data.uid = Number(data.uid);
  if (data.id) data.id = Number(data.id);
  if (data.target_uid) data.target_uid = Number(data.target_uid);
  if (data.like_count) data.like_count = Number(data.like_count);
  if (data.role !== undefined && data.role !== null) data.role = Number(data.role);
  return data;
}

$("#formModalCancel").addEventListener("click", closeFormModal);
$("#formModalOverlay").addEventListener("click", (e) => {
  if (e.target === $("#formModalOverlay")) closeFormModal();
});

// Enter key to submit modal form
$("#formModalOverlay").addEventListener("keydown", (e) => {
  if (e.key === "Enter" && e.target.tagName !== "TEXTAREA") {
    e.preventDefault();
    $("#formModalSubmit").click();
  }
});

$("#formModalSubmit").addEventListener("click", async () => {
  const form = $("#modalForm");
  if (!validateForm(form)) return;
  const type = form.dataset.formType;
  const data = getFormModalJson();
  try {
    switch (type) {
      case "createUser":
        await api("/api/users", { method: "POST", body: JSON.stringify(data) });
        toast("账号已创建");
        break;
      case "editUser": {
        const uid = data.uid; delete data.uid;
        await api(`/api/users/${uid}`, { method: "PATCH", body: JSON.stringify(data) });
        toast("账号已保存");
        break;
      }
      case "resetPassword":
        await api(`/api/users/${data.uid}/password`, { method: "PATCH", body: JSON.stringify({ password: data.password }) });
        toast("密码已重置");
        break;
      case "appointRole":
        await api(`/api/users/${data.uid}/role`, { method: "PATCH", body: JSON.stringify({ role: data.role }) });
        toast("角色已更新");
        break;
      case "createDynamic":
        await api("/api/dynamics", { method: "POST", body: JSON.stringify(data) });
        toast("动态已发布");
        break;
      case "editDynamic": {
        const id = data.id; delete data.id;
        await api(`/api/dynamics/${id}`, { method: "PATCH", body: JSON.stringify(data) });
        toast("动态已更新");
        break;
      }
      case "createStar":
        await api("/api/star-notices", { method: "POST", body: JSON.stringify(data) });
        toast("公告已发布");
        break;
      case "editStar": {
        const t = encodeURIComponent(data.original_title);
        const a = encodeURIComponent(data.original_author);
        delete data.original_title; delete data.original_author;
        await api(`/api/star-notices?title=${t}&author=${a}`, { method: "PATCH", body: JSON.stringify(data) });
        toast("公告已更新");
        break;
      }
      case "createNotice":
        await api("/api/admin-notices", { method: "POST", body: JSON.stringify(data) });
        toast("通知已创建");
        break;
      case "editNotice": {
        const nid = data.id; delete data.id;
        data.delivered = Number(data.delivered || 0);
        await api(`/api/admin-notices/${nid}`, { method: "PATCH", body: JSON.stringify(data) });
        toast("通知已更新");
        break;
      }
    }
    closeFormModal();
    await refreshCurrent();
  } catch (err) { toast(err.message, "error"); }
});
// ---- Create buttons ----
$("#createUserBtn")?.addEventListener("click", () => openFormModal("createUser"));
$("#createDynamicBtn")?.addEventListener("click", () => openFormModal("createDynamic"));
$("#createStarBtn")?.addEventListener("click", () => {
  const author = localStorage.getItem("mhkh_user_name") || "";
  openFormModal("createStar", { author });
});
$("#createNoticeBtn")?.addEventListener("click", async () => {
  try {
    await loadNoticeTargetOptions();
    openFormModal("createNotice");
  } catch (err) {
    toast(err.message, "error");
  }
});

function validateForm(form) {
  for (const el of form.querySelectorAll("[required]")) {
    if (!el.value.trim()) {
      const label = el.closest("label")?.textContent?.trim() || el.name || "字段";
      toast(`${label} 不能为空`, "error");
      el.focus();
      return false;
    }
    if (el.minLength > 0 && el.value.length < el.minLength) {
      toast(`最少输入 ${el.minLength} 个字符`, "error");
      el.focus();
      return false;
    }
    if (el.type === "email" && !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(el.value)) {
      toast("请输入正确的邮箱格式", "error");
      el.focus();
      return false;
    }
  }
  return true;
}
function escapeHtml(value) {
  return String(value ?? "")
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;");
}

function formatAction(action) {
  return actionLabels[action] || action || "-";
}

function renderPager(containerId, viewKey, rows) {
  const el = document.getElementById(containerId);
  if (!el) return;
  const p = pageState[viewKey];
  const hasMore = rows.length >= PAGE_SIZE;
  el.innerHTML =
    `<span class="pager-total">第 ${p} 页</span>` +
    (p > 1 ? `<button data-page="${p - 1}" data-pager="${viewKey}">&laquo; 上一页</button>` : "") +
    (hasMore ? `<button data-page="${p + 1}" data-pager="${viewKey}">下一页 &raquo;</button>` : "");
}
const sortState = {};

function renderTable(table, columns, rows, options = {}) {
  const { selectable = false, viewKey = "" } = options;

  // Apply sort if active
  const sort = sortState[viewKey];
  if (sort && sort.key && sort.dir) {
    const col = columns.find((c) => c.key === sort.key);
    if (col) {
      rows = [...rows].sort((a, b) => {
        let va = a[sort.key] ?? "";
        let vb = b[sort.key] ?? "";
        if (typeof va === "number" && typeof vb === "number") return sort.dir === "asc" ? va - vb : vb - va;
        va = String(va); vb = String(vb);
        return sort.dir === "asc" ? va.localeCompare(vb) : vb.localeCompare(va);
      });
    }
  }

  const checkboxCol = selectable ? '<th style="width:40px"><input type="checkbox" class="table-checkbox" data-select-all></th>' : "";
  const head = `<thead><tr>${checkboxCol}${columns.map((c) => {
    const className = c.className || "";
    const width = c.width ? ` style="width:${c.width}"` : "";
    if (c.sortable && c.key) {
      const s = sortState[viewKey];
      const sortCls = s && s.key === c.key && s.dir ? `sortable sort-${s.dir}` : "sortable";
      const cls = [className, sortCls].filter(Boolean).join(" ");
      return `<th class="${cls}"${width} data-sort-key="${c.key}" data-sort-view="${viewKey}">${c.label}</th>`;
    }
    return `<th${className ? ` class="${className}"` : ""}${width}>${c.label}</th>`;
  }).join("")}</tr></thead>`;
  const body = rows.length
    ? rows.map((row) => {
        const cb = selectable ? `<td><input type="checkbox" class="table-checkbox" data-select-id="${row.uid || row.id}"></td>` : "";
        return `<tr>${cb}${columns.map((c) => {
          const value = c.key === "create_time" ? formatDateTime(row[c.key]) : row[c.key];
          return `<td${c.className ? ` class="${c.className}"` : ""}>${c.render ? c.render(row) : escapeHtml(value)}</td>`;
        }).join("")}</tr>`;
      }).join("")
    : `<tr><td colspan="${columns.length + (selectable ? 1 : 0)}"><div class="empty-state">暂无数据</div></td></tr>`;
  table.innerHTML = `${head}<tbody>${body}</tbody>`;

  // Bind sort click
  table.querySelectorAll("th[data-sort-key]").forEach((th) => {
    th.addEventListener("click", () => {
      const key = th.dataset.sortKey;
      const view = th.dataset.sortView;
      const cur = sortState[view];
      if (cur && cur.key === key) {
        sortState[view] = cur.dir === "asc" ? { key, dir: "desc" } : cur.dir === "desc" ? { key: null, dir: null } : { key, dir: "asc" };
      } else {
        sortState[view] = { key, dir: "asc" };
      }
      refreshCurrent().catch((err) => toast(err.message, "error"));
    });
  });
}

function renderMiniList(selector, rows, emptyText, renderRow) {
  const el = $(selector);
  if (!el) return;
  if (!rows || rows.length === 0) {
    el.innerHTML = `<div class="empty-state">${emptyText}</div>`;
    return;
  }
  el.innerHTML = rows.map(renderRow).join("");
}

async function loadSummary() {
  const data = await api("/api/summary");
  $("#mUsers").textContent = data.users;
  $("#mDynamics").textContent = data.dynamics;
  $("#mApplies").textContent = data.pending_applies;
  $("#mNotices").textContent = data.notices;
  $("#mTotalOps").textContent = data.total_operations ?? 0;
  $("#mTodayOps").textContent = data.today_operations ?? 0;
  $("#mTodayUsers").textContent = data.today_users ?? 0;
  $("#mTodayDynamics").textContent = data.today_dynamics ?? 0;
  $("#mPendingDynamics").textContent = data.pending_dynamics ?? 0;
  $("#mTodayAIChats").textContent = data.today_ai_chats ?? 0;
  renderMiniList("#recentLoginList", data.recent_logins, "暂无登录记录", (r) => `
    <div class="mini-item">
      <strong>${escapeHtml(r.user || "-")}</strong>
      <span>${escapeHtml(r.ip || "")}</span>
      <time>${escapeHtml(formatDateTime(r.create_time))}</time>
    </div>
  `);
  renderMiniList("#recentErrorList", data.recent_errors, "暂无异常操作", (r) => `
    <div class="mini-item danger-text">
      <strong>${escapeHtml(formatAction(r.action))}</strong>
      <span>${escapeHtml(r.summary || "")}</span>
      <time>${escapeHtml(formatDateTime(r.create_time))}</time>
    </div>
  `);
}

async function loadLogOperators() {
  const sel = $("#logOperatorFilter");
  if (!sel) return;
  try {
    const rows = await api("/api/log-operators", { skipLoading: true });
    const current = sel.value;
    const opts = ['<option value="">操作人</option>'];
    for (const r of rows || []) {
      const name = String(r.operator || "");
      if (!name) continue;
      opts.push(`<option value="${escapeHtml(name)}">${escapeHtml(name)} (${r.cnt})</option>`);
    }
    sel.innerHTML = opts.join("");
    if (current) sel.value = current;
  } catch (err) { /* ignore */ }
}

async function loadLogs() {
  const q = encodeURIComponent($("#logSearch")?.value?.trim() || "");
  const module = $("#logModuleFilter")?.value || "";
  const action = $("#logActionFilter")?.value || "";
  const operator = $("#logOperatorFilter")?.value || "";
  const startDate = $("#logStartDate")?.value || "";
  const endDate = $("#logEndDate")?.value || "";
  let url = `/api/logs?page=${pageState.logs}&limit=${PAGE_SIZE}`;
  if (q) url += `&q=${q}`;
  if (module) url += `&module=${module}`;
  if (action) url += `&action=${action}`;
  if (operator) url += `&operator=${encodeURIComponent(operator)}`;
  if (startDate) url += `&start_date=${startDate}`;
  if (endDate) url += `&end_date=${endDate}`;
  const rows = await api(url);
  renderTable($("#logTable"), [
    { key: "id", label: "ID", sortable: true },
    { key: "module", label: "模块", sortable: true },
    { key: "action", label: "操作", sortable: true, render: (r) => formatAction(r.action) },
    { key: "summary", label: "说明" },
    { key: "user", label: "操作人", sortable: true },
    { key: "create_time", label: "时间", sortable: true },
  ], rows, { viewKey: "logs" });
  renderPager("logPager", "logs", rows);
}
function renderAnalytics(data) {
  renderDynamicLineChart(data.dynamic_trend || []);

  const stats = Object.fromEntries((data.apply_stats || []).map((r) => [Number(r.status), Number(r.count)]));
  const total = Object.values(stats).reduce((a, b) => a + b, 0);
  const accepted = stats[1] || 0;
  const pending = stats[0] || 0;
  const rejected = stats[2] || 0;
  const noticeTotal = Number(data.notice_stats?.total || 0);
  const delivered = Number(data.notice_stats?.delivered || 0);
  const applyRate = total ? Math.round(accepted / total * 100) : 0;
  const noticeRate = noticeTotal ? Math.round(delivered / noticeTotal * 100) : 0;

  $("#mApplyRate").textContent = `${applyRate}%`;
  $("#mNoticeRate").textContent = `${noticeRate}%`;

  const rows = [
    ["待处理", pending, total],
    ["已通过", accepted, total],
    ["已拒绝", rejected, total],
    ["通知已送达", delivered, noticeTotal],
  ];
  $("#applyStats").innerHTML = rows.map(([label, count, base]) => {
    const pct = base ? Math.round(count / base * 100) : 0;
    return `<div class="stat-row">
      <div class="stat-label"><span>${label}</span><span>${count} / ${base || 0}</span></div>
      <div class="stat-track"><div class="stat-fill" style="width:${pct}%"></div></div>
    </div>`;
  }).join("");
}
function lastNDays(n) {
  const days = [];
  const today = new Date();
  for (let i = n - 1; i >= 0; --i) {
    const d = new Date(today);
    d.setDate(today.getDate() - i);
    const key = d.toISOString().slice(0, 10);
    days.push(key);
  }
  return days;
}

function cssVar(name) {
  return getComputedStyle(document.body).getPropertyValue(name).trim();
}

function renderDynamicLineChart(rows) {
  const canvas = $("#dynamicTrend");
  const ctx = canvas.getContext("2d");
  const rect = canvas.getBoundingClientRect();
  const dpr = window.devicePixelRatio || 1;
  canvas.width = Math.max(1, Math.floor(rect.width * dpr));
  canvas.height = Math.max(1, Math.floor(rect.height * dpr));
  ctx.setTransform(dpr, 0, 0, dpr, 0, 0);

  const width = rect.width;
  const height = rect.height;
  const padding = { left: 42, right: 18, top: 22, bottom: 38 };
  const text = cssVar("--text") || "#1f2937";
  const muted = cssVar("--muted") || "#667085";
  const line = cssVar("--line") || "#dfe3ea";
  const primary = cssVar("--primary") || "#2563eb";

  ctx.clearRect(0, 0, width, height);
  const map = Object.fromEntries(rows.map((r) => [String(r.day).slice(0, 10), Number(r.count || 0)]));
  const days = lastNDays(14);
  const values = days.map((d) => map[d] || 0);
  const maxValue = Math.max(1, ...values);
  const plotW = width - padding.left - padding.right;
  const plotH = height - padding.top - padding.bottom;
  const x = (i) => padding.left + (plotW * i) / Math.max(1, days.length - 1);
  const y = (v) => padding.top + plotH - (plotH * v) / maxValue;

  ctx.font = "12px Microsoft YaHei, Segoe UI, Arial";
  ctx.strokeStyle = line;
  ctx.fillStyle = muted;
  ctx.lineWidth = 1;
  for (let i = 0; i <= 4; i++) {
    const value = Math.round((maxValue * i) / 4);
    const yy = y(value);
    ctx.beginPath();
    ctx.moveTo(padding.left, yy);
    ctx.lineTo(width - padding.right, yy);
    ctx.stroke();
    ctx.fillText(String(value), 10, yy + 4);
  }

  ctx.fillStyle = muted;
  days.forEach((day, i) => {
    if (i % 2 === 0 || i === days.length - 1) {
      ctx.fillText(day.slice(5), x(i) - 15, height - 12);
    }
  });

  ctx.strokeStyle = primary;
  ctx.lineWidth = 3;
  ctx.beginPath();
  values.forEach((value, i) => {
    const px = x(i);
    const py = y(value);
    if (i === 0) ctx.moveTo(px, py);
    else ctx.lineTo(px, py);
  });
  ctx.stroke();

  values.forEach((value, i) => {
    const px = x(i);
    const py = y(value);
    ctx.beginPath();
    ctx.fillStyle = primary;
    ctx.arc(px, py, 4, 0, Math.PI * 2);
    ctx.fill();
    if (value > 0) {
      ctx.fillStyle = text;
      ctx.fillText(String(value), px - 4, py - 10);
    }
  });
}

async function loadAnalytics() {
  const data = await api("/api/analytics");
  lastAnalyticsData = data;
  renderAnalytics(data);
}

async function loadUsers() {
  const q = encodeURIComponent(($("#userSearch")?.value || "").trim());
  const rows = await api(`/api/users?q=${q}&page=${pageState.users}&limit=${PAGE_SIZE}`);
  renderTable($("#usersTable"), [
    { key: "uid", label: "UID", sortable: true },
    { key: "name", label: "用户名", sortable: true },
    { key: "email", label: "邮箱", sortable: true },
    { key: "nick", label: "昵称" },
    { key: "sex", label: "性别" },
    { key: "icon", label: "头像" },
    { key: "role", label: "角色", sortable: true, render: (r) => `<span class="role-pill role-${Number(r.role || 0)}">${roleLabels[Number(r.role || 0)] || "普通用户"}</span>` },
    { key: "desc", label: "签名" },
    {
      label: "操作",
      className: "action-cell user-actions-cell",
      width: "360px",
      render: (r) => {
        const role = Number(r.role || 0);
        const selfRole = Number(localStorage.getItem("mhkh_user_role") || 0);
        const selfUid = localStorage.getItem("mhkh_user_uid");
        const selfName = localStorage.getItem("mhkh_user_name");
        const isSelf = (selfUid && String(r.uid) === selfUid) || (selfName && r.name === selfName);
        const canEdit = isSelf || role < selfRole;
        const canReset = !isSelf && role < selfRole;
        const canDelete = !isSelf && role < selfRole;
        const canManageRole = selfRole === 2 && !isSelf && role !== 2;
        const roleBtn = !canManageRole
          ? ""
          : role === 1
            ? `<button class="role-cancel-btn" data-unrole-user="${r.uid}">取消任命</button>`
            : `<button class="role-appoint-btn" data-role-user="${r.uid}" data-current-role="${role}">任命管理员</button>`;
        return `
          <div class="table-actions">
            ${canEdit ? `<button data-edit-user="${r.uid}">编辑</button>` : ""}
            ${roleBtn}
            ${canReset ? `<button class="warn" data-reset-user="${r.uid}">重置密码</button>` : ""}
            ${canDelete ? `<button class="danger" data-delete-user="${r.uid}">删除</button>` : ""}
          </div>
        `;
      },
    },
  ], rows, { selectable: true, viewKey: "users" });
  renderPager("usersPager", "users", rows);
  updateBatchBar();
}

async function loadDynamics() {
  const q = encodeURIComponent(($("#dynamicSearch")?.value || "").trim());
  const status = $("#dynamicStatusFilter")?.value || "";
  const statusParam = status !== "" ? `&status=${status}` : "";
  const rows = await api(`/api/dynamics?q=${q}${statusParam}&page=${pageState.dynamics}&limit=${PAGE_SIZE}`);
  const statusText = { 0: "正常", 1: "审核中", 2: "违规隐藏" };
  const statusColor = { 0: "var(--ok)", 1: "#f59e0b", 2: "var(--danger)" };
  renderTable($("#dynamicTable"), [
    { key: "id", label: "ID", sortable: true },
    { key: "uid", label: "UID", sortable: true },
    { key: "name", label: "用户" },
    { key: "content", label: "内容", render: (r) => `<div class="content">${escapeHtml(r.content)}</div>` },
    { key: "like_count", label: "点赞", sortable: true },
    { label: "状态", render: (r) => `<span style="color:${statusColor[r.status] || "var(--muted)"}">${statusText[r.status] || "未知"}</span>` },
    { key: "create_time", label: "时间", sortable: true },
    { label: "操作", className: "action-cell dynamic-actions-cell", width: "270px", render: (r) => {
      const auditBtn = r.status !== 1 ? `<button class="audit-btn" data-audit-dynamic="${r.id}">审核</button>` : "";
      const passBtn = r.status === 1 ? `<button class="pass-btn" data-pass-dynamic="${r.id}">通过</button>` : "";
      const hideBtn = r.status !== 2 ? `<button class="warn" data-hide-dynamic="${r.id}">隐藏</button>` : "";
      const restoreBtn = r.status === 2 ? `<button class="ok" data-restore-dynamic="${r.id}">恢复</button>` : "";
      return `<div class="table-actions"><button class="edit-btn" data-edit-dynamic="${r.id}">编辑</button>${auditBtn}${passBtn}${hideBtn}${restoreBtn}<button class="danger" data-delete-dynamic="${r.id}">删除</button></div>`;
    }},
  ], rows, { viewKey: "dynamics" });
  renderPager("dynamicPager", "dynamics", rows);
}

async function loadApplies() {
  const rows = await api(`/api/friend-applies?page=${pageState.applies}&limit=${PAGE_SIZE}`);
  const statusText = { 0: "待处理", 1: "同意", 2: "拒绝" };
  renderTable($("#applyTable"), [
    { key: "id", label: "ID", sortable: true },
    { label: "申请人", render: (r) => `${escapeHtml(r.from_name)} (${r.from_uid})` },
    { label: "接收人", render: (r) => `${escapeHtml(r.to_name)} (${r.to_uid})` },
    { key: "status", label: "状态", sortable: true, render: (r) => statusText[r.status] || r.status },
    { key: "descs", label: "备注" },
    { label: "操作", className: "compact-actions-cell apply-actions-cell", width: "230px", render: (r) => `<div class="table-actions"><button class="ok" data-apply="${r.id}" data-status="1">同意</button><button class="danger" data-apply="${r.id}" data-status="2">拒绝</button>${r.status !== 0 ? `<button data-apply="${r.id}" data-status="0">取消处理</button>` : ""}</div>` },
  ], rows, { viewKey: "applies" });
  renderPager("applyPager", "applies", rows);
}

async function loadFriends() {
  const q = encodeURIComponent(($("#friendSearch")?.value || "").trim());
  const rows = await api(`/api/friends?q=${q}&page=${pageState.friends}&limit=${PAGE_SIZE}`);
  renderTable($("#friendTable"), [
    { key: "self_id", label: "用户 UID", sortable: true },
    { label: "用户", render: (r) => `${escapeHtml(r.self_name || "")}${r.self_nick ? ` / ${escapeHtml(r.self_nick)}` : ""}` },
    { key: "friend_id", label: "好友 UID", sortable: true },
    { label: "好友", render: (r) => `${escapeHtml(r.friend_name || "")}${r.friend_nick ? ` / ${escapeHtml(r.friend_nick)}` : ""}` },
    { key: "back", label: "备注" },
    { label: "操作", className: "compact-actions-cell friend-actions-cell", width: "150px", render: (r) => `<div class="table-actions"><button class="danger" data-delete-friend="${r.self_id}" data-friend-id="${r.friend_id}">删除关系</button></div>` },
  ], rows, { viewKey: "friends" });
  renderPager("friendPager", "friends", rows);
}

async function loadStarNotices() {
  const q = encodeURIComponent(($("#starSearch")?.value || "").trim());
  const rows = await api(`/api/star-notices?q=${q}&page=${pageState.star}&limit=${PAGE_SIZE}`);
  renderTable($("#starTable"), [
    { key: "title", label: "标题", sortable: true },
    { key: "author", label: "作者", sortable: true },
    { key: "content", label: "内容", render: (r) => `<div class="content">${escapeHtml(r.content)}</div>` },
    { label: "操作", className: "compact-actions-cell", width: "150px", render: (r) => `<div class="table-actions"><button data-edit-star="${escapeHtml(r.title)}" data-star-author="${escapeHtml(r.author)}">编辑</button><button class="danger" data-star-title="${escapeHtml(r.title)}" data-star-author="${escapeHtml(r.author)}">删除</button></div>` },
  ], rows, { viewKey: "star" });
  renderPager("starPager", "star", rows);
}

async function loadAdminNotices() {
  const q = encodeURIComponent(($("#noticeSearch")?.value || "").trim());
  const rows = await api(`/api/admin-notices?q=${q}&page=${pageState.notices}&limit=${PAGE_SIZE}`);
  renderTable($("#noticeTable"), [
    { key: "id", label: "ID", sortable: true },
    { label: "目标", render: (r) => r.target_uid ? r.target_uid : "广播" },
    { key: "level", label: "等级", sortable: true, render: (r) => r.level.toUpperCase() },
    { key: "title", label: "标题", sortable: true },
    { key: "content", label: "内容", render: (r) => `<div class="content">${escapeHtml(r.content)}</div>` },
    { label: "状态", render: (r) => r.delivered ? "已处理" : "未处理" },
    { key: "create_time", label: "时间", sortable: true },
    { label: "操作", className: "compact-actions-cell", width: "150px", render: (r) => `<div class="table-actions"><button data-edit-notice="${r.id}">编辑</button><button class="danger" data-delete-notice="${r.id}">删除</button></div>` },
  ], rows, { viewKey: "notices" });
  renderPager("noticePager", "notices", rows);
}
async function refreshCurrent() {
  if (refreshInFlight) return refreshInFlight;
  refreshInFlight = doRefreshCurrent().finally(() => {
    refreshInFlight = null;
  });
  return refreshInFlight;
}

async function doRefreshCurrent() {
  if (currentView === "dashboard") {
    await loadSummary();
    await loadLogs();
    await loadAnalytics();
    loadLogOperators().catch(() => {});
  }
  if (currentView === "users") await loadUsers();
  if (currentView === "dynamics") await loadDynamics();
  if (currentView === "friends") await loadFriends();
  if (currentView === "applies") await loadApplies();
  if (currentView === "star") await loadStarNotices();
  if (currentView === "notices") await loadAdminNotices();
  if (currentView === "ai") await loadAISessions();
}

function activateView(view) {
  if (!titles[view]) view = "dashboard";
  currentView = view;
  localStorage.setItem("mhkh_admin_view", view);
  document.querySelectorAll(".nav").forEach((n) => n.classList.toggle("active", n.dataset.view === view));
  document.querySelectorAll(".view").forEach((v) => v.classList.toggle("active", v.id === view));
  $("#view-title").textContent = titles[view][0];
  $("#view-subtitle").textContent = titles[view][1];
  if (view === "dashboard") startAutoRefresh();
  else { stopAutoRefresh(); updateTimerDisplay(); }
}

function setTheme(mode) {
  document.body.classList.toggle("dark", mode === "dark");
  localStorage.setItem("mhkh_admin_theme", mode);
  $("#themeBtn").textContent = mode === "dark" ? "亮色" : "暗黑";
  if (lastAnalyticsData && currentView === "dashboard") renderAnalytics(lastAnalyticsData);
}

document.querySelectorAll(".nav").forEach((btn) => {
  btn.addEventListener("click", async () => {
    activateView(btn.dataset.view);
    if (currentView === "dashboard") startAutoRefresh();
    else stopAutoRefresh();
    try { await refreshCurrent(); } catch (err) { toast(err.message, "error"); }
  });
});

$("#refreshBtn").addEventListener("click", () => {
  resetAutoRefresh();
  const el = $("#lastRefreshTime");
  if (el) el.textContent = `更新于 ${new Date().toLocaleTimeString("zh-CN")}`;
  refreshCurrent().catch((err) => toast(err.message, "error"));
});

// 壁纸
async function refreshBg() {
  const btn = $("#bgBtn");
  btn.disabled = true;
  btn.textContent = "加载中..";
  try {
    const res = await fetch("https://www.loliapi.com/acg/pc/?t=" + Date.now());
    const html = await res.text();
    const match = html.match(/href="([^"]+)"/);
    const actualUrl = match ? match[1] : res.url;
    document.body.style.backgroundImage = `url("${actualUrl}")`;
    localStorage.setItem("mhkh_bg_url", actualUrl);
  } catch {
    const fallback = `https://www.loliapi.com/acg/pc/?t=${Date.now()}`;
    document.body.style.backgroundImage = `url("${fallback}")`;
    localStorage.setItem("mhkh_bg_url", fallback);
  } finally {
    btn.disabled = false;
    btn.textContent = "切换壁纸";
  }
}

function loadSavedBg() {
  const saved = localStorage.getItem("mhkh_bg_url");
  if (saved) {
    document.body.style.backgroundImage = `url("${saved}")`;
  }
}

// 自定义背景上传
$("#customBgBtn").addEventListener("click", () => $("#bgUploadInput").click());
$("#bgUploadInput").addEventListener("change", (e) => {
  const file = e.target.files[0];
  if (!file) return;
  const reader = new FileReader();
  reader.onload = (ev) => {
    const dataUrl = ev.target.result;
    document.body.style.backgroundImage = `url("${dataUrl}")`;
    localStorage.setItem("mhkh_bg_url", dataUrl);
    toast("自定义背景已应用", "success");
  };
  reader.onerror = () => toast("读取图片失败", "error");
  reader.readAsDataURL(file);
  e.target.value = ""; // 允许重复选择同一文件
});

$("#bgBtn").addEventListener("click", refreshBg);

// 预览壁纸
let bgViewMode = false;
$("#bgViewBtn").addEventListener("click", () => {
  bgViewMode = !bgViewMode;
  document.body.classList.toggle("view-bg", bgViewMode);
  $("#bgViewBtn").textContent = bgViewMode ? "退出预览" : "预览壁纸";
});

$("#bgSaveBtn").addEventListener("click", async () => {
  const url = localStorage.getItem("mhkh_bg_url");
  if (!url) { toast("没有可保存的壁纸", "error"); return; }
  try {
    toast("正在获取图片...", "loading");
    const res = await api("/api/get-bg-url", { method: "POST", body: JSON.stringify({ url }) });
    const a = document.createElement("a");
    a.href = res.data_url;
    const ext = res.final_url.split(".").pop().split("?")[0] || "jpg";
    a.download = `wallpaper_${Date.now()}.${ext}`;
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
    toast("壁纸下载已开始", "success");
  } catch (err) { toast(err.message, "error"); }
});
$("#themeBtn").addEventListener("click", () => {
  setTheme(document.body.classList.contains("dark") ? "light" : "dark");
});

// ---- Search buttons (direct binding) ----
function bindClick(id, fn, key) {
  const el = document.getElementById(id);
  if (!el) return;
  el.addEventListener("click", (e) => { e.preventDefault(); pageState[key] = 1; fn().catch((err) => toast(err.message, "error")); });
}
bindClick("searchUsers", loadUsers, "users");
bindClick("searchDynamics", loadDynamics, "dynamics");
bindClick("searchFriends", loadFriends, "friends");
bindClick("searchStar", loadStarNotices, "star");
bindClick("searchNotices", loadAdminNotices, "notices");
bindClick("searchLogs", loadLogs, "logs");

// Clear buttons
function bindClear(clearId, inputId, fn, key) {
  const el = document.getElementById(clearId);
  if (!el) return;
  el.addEventListener("click", () => { const i = document.getElementById(inputId); if (i) { i.value = ""; } pageState[key] = 1; fn().catch(() => {}); });
}
bindClear("userSearchClear", "userSearch", loadUsers, "users");
bindClear("dynamicSearchClear", "dynamicSearch", loadDynamics, "dynamics");
bindClear("friendSearchClear", "friendSearch", loadFriends, "friends");
bindClear("starSearchClear", "starSearch", loadStarNotices, "star");
bindClear("noticeSearchClear", "noticeSearch", loadAdminNotices, "notices");
bindClear("logSearchClear", "logSearch", loadLogs, "logs");

// Debounced search inputs
function bindSearchInput(inputId, loadFn, stateKey) {
  const input = document.getElementById(inputId);
  if (!input) return;
  const fn = debounce(() => { pageState[stateKey] = 1; loadFn().catch((err) => toast(err.message, "error")); }, 300);
  input.addEventListener("input", fn);
}
bindSearchInput("userSearch", loadUsers, "users");
bindSearchInput("dynamicSearch", loadDynamics, "dynamics");
bindSearchInput("friendSearch", loadFriends, "friends");
bindSearchInput("starSearch", loadStarNotices, "star");
bindSearchInput("noticeSearch", loadAdminNotices, "notices");
bindSearchInput("logSearch", loadLogs, "logs");

// Filters
function bindChange(id, fn, key) {
  const el = document.getElementById(id);
  if (!el) return;
  el.addEventListener("change", () => { pageState[key] = 1; fn().catch((err) => toast(err.message, "error")); });
}
bindChange("dynamicStatusFilter", loadDynamics, "dynamics");
bindChange("logModuleFilter", loadLogs, "logs");
bindChange("logActionFilter", loadLogs, "logs");
bindChange("logOperatorFilter", loadLogs, "logs");
bindChange("logStartDate", loadLogs, "logs");
bindChange("logEndDate", loadLogs, "logs");

// ---- Auto-refresh timer ----
let autoRefreshCountdown = 30;
let autoRefreshTimer = null;

function updateTimerDisplay() {
  const el = $("#autoRefreshTimer");
  if (!el) return;
  if (currentView !== "dashboard") {
    el.textContent = "";
    return;
  }
  el.textContent = `${autoRefreshCountdown}s`;
}

function startAutoRefresh() {
  stopAutoRefresh();
  autoRefreshCountdown = 30;
  updateTimerDisplay();
  autoRefreshTimer = setInterval(() => {
    autoRefreshCountdown--;
    if (autoRefreshCountdown <= 0) {
      autoRefreshCountdown = 30;
      refreshCurrent().catch(() => {});
      const el = $("#lastRefreshTime");
      if (el) el.textContent = `更新于 ${new Date().toLocaleTimeString("zh-CN")}`;
    }
    updateTimerDisplay();
  }, 1000);
}

function stopAutoRefresh() {
  if (autoRefreshTimer) {
    clearInterval(autoRefreshTimer);
    autoRefreshTimer = null;
  }
}

function resetAutoRefresh() {
  autoRefreshCountdown = 30;
  updateTimerDisplay();
}

// ---- CSV Export ----
function exportXlsx(filename, headers, rows) {
  const data = [headers.map((h) => h.label)];
  rows.forEach((r) => {
    data.push(headers.map((h) => r[h.key] ?? ""));
  });
  const ws = XLSX.utils.aoa_to_sheet(data);
  const wb = XLSX.utils.book_new();
  XLSX.utils.book_append_sheet(wb, ws, "Sheet1");
  XLSX.writeFile(wb, filename);
  toast("导出成功", "success");
}

async function fetchAll(basePath) {
  const all = [];
  let page = 1;
  while (true) {
    const sep = basePath.includes("?") ? "&" : "?";
    const rows = await api(`${basePath}${sep}page=${page}&limit=200`);
    all.push(...rows);
    if (rows.length < 200) break;
    page++;
  }
  return all;
}

$("#exportUsers")?.addEventListener("click", async () => {
  try {
    const q = encodeURIComponent(($("#userSearch")?.value || "").trim());
    const rows = await fetchAll(`/api/users?q=${q}`);
    exportXlsx("用户列表.xlsx", [
      { key: "uid", label: "UID" }, { key: "name", label: "用户名" },
      { key: "email", label: "邮箱" }, { key: "nick", label: "昵称" },
      { key: "sex", label: "性别" }, { key: "desc", label: "签名" },
    ], rows);
  } catch (err) { toast(err.message || "导出失败", "error"); }
});

$("#exportDynamics")?.addEventListener("click", async () => {
  try {
    const q = encodeURIComponent(($("#dynamicSearch")?.value || "").trim());
    const status = $("#dynamicStatusFilter")?.value || "";
    const statusParam = status !== "" ? `&status=${status}` : "";
    const rows = await fetchAll(`/api/dynamics?q=${q}${statusParam}`);
    const statusMap = { 0: "正常", 1: "审核中", 2: "违规隐藏" };
    rows.forEach((r) => {
      r.statusText = statusMap[r.status] || "未知";
      r.create_time = formatDateTime(r.create_time);
    });
    exportXlsx("动态列表.xlsx", [
      { key: "id", label: "ID" }, { key: "uid", label: "UID" },
      { key: "name", label: "用户" }, { key: "content", label: "内容" },
      { key: "like_count", label: "点赞" }, { key: "statusText", label: "状态" },
      { key: "create_time", label: "时间" },
    ], rows);
  } catch (err) { toast(err.message || "导出失败", "error"); }
});

$("#exportStar")?.addEventListener("click", async () => {
  try {
    const q = encodeURIComponent(($("#starSearch")?.value || "").trim());
    const rows = await fetchAll(`/api/star-notices?q=${q}`);
    exportXlsx("公告列表.xlsx", [
      { key: "title", label: "标题" }, { key: "author", label: "作者" },
      { key: "content", label: "内容" },
    ], rows);
  } catch (err) { toast(err.message || "导出失败", "error"); }
});

$("#exportLogs")?.addEventListener("click", async () => {
  try {
    const q = encodeURIComponent(($("#logSearch")?.value || "").trim());
    const module = $("#logModuleFilter")?.value || "";
    const action = $("#logActionFilter")?.value || "";
    const operator = $("#logOperatorFilter")?.value || "";
    const startDate = $("#logStartDate")?.value || "";
    const endDate = $("#logEndDate")?.value || "";
    let basePath = `/api/logs?q=${encodeURIComponent("")}`;
    if (q) basePath = `/api/logs?q=${q}`;
    if (module) basePath += `&module=${encodeURIComponent(module)}`;
    if (action) basePath += `&action=${encodeURIComponent(action)}`;
    if (operator) basePath += `&operator=${encodeURIComponent(operator)}`;
    if (startDate) basePath += `&start_date=${encodeURIComponent(startDate)}`;
    if (endDate) basePath += `&end_date=${encodeURIComponent(endDate)}`;
    const rows = await fetchAll(basePath);
    rows.forEach((r) => {
      r.action = formatAction(r.action);
      r.create_time = formatDateTime(r.create_time);
    });
    exportXlsx("操作日志.xlsx", [
      { key: "id", label: "ID" }, { key: "module", label: "模块" },
      { key: "action", label: "操作" }, { key: "summary", label: "说明" },
      { key: "user", label: "操作人" }, { key: "create_time", label: "时间" },
    ], rows);
  } catch (err) { toast(err.message || "导出失败", "error"); }
});

$("#exportNotices")?.addEventListener("click", async () => {
  try {
    const q = encodeURIComponent(($("#noticeSearch")?.value || "").trim());
    const rows = await fetchAll(`/api/admin-notices?q=${q}`);
    rows.forEach((r) => {
      r.deliveredText = r.delivered ? "已处理" : "未处理";
      r.create_time = formatDateTime(r.create_time);
    });
    exportXlsx("通知列表.xlsx", [
      { key: "id", label: "ID" }, { key: "target_uid", label: "目标UID" },
      { key: "title", label: "标题" }, { key: "content", label: "内容" },
      { key: "level", label: "等级" }, { key: "deliveredText", label: "状态" },
      { key: "create_time", label: "时间" },
    ], rows);
  } catch (err) { toast(err.message || "导出失败", "error"); }
});
// ---- Batch operations ----
function updateBatchBar() {
  const bar = $("#userBatchBar");
  const count = selectedIds.users.size;
  $("#userBatchCount").textContent = `已选 ${count} 项`;
  bar?.classList.toggle("show", count > 0);
}

document.addEventListener("change", (e) => {
  if (e.target.dataset.selectAll !== undefined) {
    const checked = e.target.checked;
    document.querySelectorAll("#usersTable [data-select-id]").forEach((cb) => {
      cb.checked = checked;
      const id = cb.dataset.selectId;
      if (checked) selectedIds.users.add(id);
      else selectedIds.users.delete(id);
    });
    updateBatchBar();
  }
  if (e.target.dataset.selectId !== undefined) {
    const id = e.target.dataset.selectId;
    if (e.target.checked) selectedIds.users.add(id);
    else selectedIds.users.delete(id);
    updateBatchBar();
  }
});

$("#batchDeselectUsers")?.addEventListener("click", () => {
  selectedIds.users.clear();
  document.querySelectorAll("#usersTable .table-checkbox").forEach((cb) => { cb.checked = false; });
  updateBatchBar();
});

$("#batchDeleteUsers")?.addEventListener("click", async () => {
  const count = selectedIds.users.size;
  if (!count) return;
  const deletePassword = await customConfirm(`确定删除选中的 ${count} 个账号吗？相关动态和好友关系也会删除。`, { password: true });
  if (!deletePassword) return;
  let success = 0, fail = 0;
  for (const uid of selectedIds.users) {
    try {
      await api(`/api/users/${uid}`, { method: "DELETE", headers: { "X-Delete-Password": deletePassword } });
      success++;
    } catch { fail++; }
  }
  selectedIds.users.clear();
  toast(`批量删除完成：成功 ${success}${fail ? `，失败 ${fail}` : ""}`, fail ? "error" : "success");
  await refreshCurrent();
});

// ---- Table click actions ----
document.body.addEventListener("click", async (event) => {
  const target = event.target;
  try {
    if (target.dataset.pager) {
      pageState[target.dataset.pager] = Number(target.dataset.page);
      await refreshCurrent();
      return;
    }
    if (target.dataset.editUser) {
      const cells = target.closest("tr").querySelectorAll("td");
      openFormModal("editUser", {
        uid: target.dataset.editUser,
        name: cells[2].textContent.trim(),
        email: cells[3].textContent.trim(),
        nick: cells[4].textContent.trim(),
        sex: cells[5].textContent.trim(),
        icon: cells[6].textContent.trim(),
        desc: cells[8].textContent.trim(),
      });
    }
    if (target.dataset.roleUser) {
      if (!await customConfirm(`确定任命 UID ${target.dataset.roleUser} 为管理员吗？`)) return;
      await api(`/api/users/${target.dataset.roleUser}/role`, { method: "PATCH", body: JSON.stringify({ role: 1 }) });
      toast("管理员已任命");
      await refreshCurrent();
    }
    if (target.dataset.unroleUser) {
      if (!await customConfirm(`确定取消 UID ${target.dataset.unroleUser} 的管理员身份吗？`)) return;
      await api(`/api/users/${target.dataset.unroleUser}/role`, { method: "PATCH", body: JSON.stringify({ role: 0 }) });
      toast("管理员身份已取消");
      await refreshCurrent();
    }
    if (target.dataset.resetUser) {
      openFormModal("resetPassword", { uid: target.dataset.resetUser });
    }
    if (target.dataset.deleteUser) {
      const deletePassword = await customConfirm(`确定删除 UID ${target.dataset.deleteUser}？相关动态、好友关系和请求将一并删除。`, { password: true });
      if (!deletePassword) return;
      await api(`/api/users/${target.dataset.deleteUser}`, { method: "DELETE", headers: { "X-Delete-Password": deletePassword } });
      toast("用户已删除");
      await refreshCurrent();
    }
    if (target.dataset.deleteDynamic) {
      if (!await customConfirm(`确定删除动态 ${target.dataset.deleteDynamic} 吗？`)) return;
      await api(`/api/dynamics/${target.dataset.deleteDynamic}`, { method: "DELETE" });
      toast("动态已删除");
      await refreshCurrent();
    }
    if (target.dataset.hideDynamic) {
      await api(`/api/dynamics/${target.dataset.hideDynamic}/status`, { method: "PATCH", body: JSON.stringify({ status: 2 }) });
      toast("动态已隐藏");
      await refreshCurrent();
    }
    if (target.dataset.auditDynamic) {
      await api(`/api/dynamics/${target.dataset.auditDynamic}/status`, { method: "PATCH", body: JSON.stringify({ status: 1 }) });
      toast("动态已设为待审核");
      await refreshCurrent();
    }
    if (target.dataset.passDynamic) {
      await api(`/api/dynamics/${target.dataset.passDynamic}/status`, { method: "PATCH", body: JSON.stringify({ status: 0 }) });
      toast("动态审核已通过");
      await refreshCurrent();
    }
    if (target.dataset.restoreDynamic) {
      await api(`/api/dynamics/${target.dataset.restoreDynamic}/status`, { method: "PATCH", body: JSON.stringify({ status: 0 }) });
      toast("动态已恢复");
      await refreshCurrent();
    }
    if (target.dataset.editDynamic) {
      const cells = target.closest("tr").querySelectorAll("td");
      openFormModal("editDynamic", {
        id: target.dataset.editDynamic,
        content: cells[3].textContent.trim(),
        like_count: cells[4].textContent.trim() || 0,
      });
    }
    if (target.dataset.apply) {
      await api(`/api/friend-applies/${target.dataset.apply}`, {
        method: "PATCH",
        body: JSON.stringify({ status: Number(target.dataset.status) }),
      });
      toast("申请状态已更新");
      await refreshCurrent();
    }
    if (target.dataset.deleteFriend) {
      const selfID = target.dataset.deleteFriend;
      const friendID = target.dataset.friendId;
      if (!await customConfirm(`确定删除 UID ${selfID} 和 UID ${friendID} 的好友关系吗？`)) return;
      await api(`/api/friends/${selfID}/${friendID}`, { method: "DELETE" });
      toast("好友关系已删除");
      await refreshCurrent();
    }
    if (target.dataset.starTitle) {
      if (!await customConfirm(`确定删除公告「${target.dataset.starTitle}」吗？`)) return;
      const title = encodeURIComponent(target.dataset.starTitle);
      const author = encodeURIComponent(target.dataset.starAuthor);
      await api(`/api/star-notices?title=${title}&author=${author}`, { method: "DELETE" });
      toast("公告已删除");
      await refreshCurrent();
    }
    if (target.dataset.editStar) {
      const cells = target.closest("tr").querySelectorAll("td");
      openFormModal("editStar", {
        original_title: target.dataset.editStar,
        original_author: target.dataset.starAuthor,
        title: cells[0].textContent.trim(),
        author: cells[1].textContent.trim(),
        content: cells[2].textContent.trim(),
      });
    }
    if (target.dataset.deleteNotice) {
      if (!await customConfirm(`确定删除通知 ${target.dataset.deleteNotice} 吗？`)) return;
      await api(`/api/admin-notices/${target.dataset.deleteNotice}`, { method: "DELETE" });
      toast("通知已删除");
      await refreshCurrent();
    }
    if (target.dataset.editNotice) {
      await loadNoticeTargetOptions();
      const cells = target.closest("tr").querySelectorAll("td");
      const targetText = cells[1].textContent.trim();
      const deliveredText = cells[5].textContent.trim();
      const deliveredVal = deliveredText === "已处理" || deliveredText === "Delivered" ? "1" : "0";
      openFormModal("editNotice", {
        id: target.dataset.editNotice,
        target_uid: targetText === "广播" ? "" : targetText,
        level: cells[2].textContent.trim().toLowerCase(),
        title: cells[3].textContent.trim(),
        content: cells[4].textContent.trim(),
        delivered: deliveredVal,
      });
    }
  } catch (err) { toast(err.message, "error"); }
});

document.addEventListener("keydown", (event) => {
  const tag = event.target.tagName.toLowerCase();
  if (tag === "input" || tag === "textarea" || tag === "select") return;
  if (event.key.toLowerCase() === "r") {
    resetAutoRefresh();
    const el = $("#lastRefreshTime");
    if (el) el.textContent = `更新于 ${new Date().toLocaleTimeString("zh-CN")}`;
    refreshCurrent().catch((err) => toast(err.message, "error"));
  }
  if (event.key.toLowerCase() === "d") {
    setTheme(document.body.classList.contains("dark") ? "light" : "dark");
  }
});
window.addEventListener("resize", debounce(() => {
  if (lastAnalyticsData && currentView === "dashboard") renderAnalytics(lastAnalyticsData);
}, 150));

setTheme(localStorage.getItem("mhkh_admin_theme") || "light");
loadSavedBg();
activateView(currentView);

// 恢复用户信息
const savedName = localStorage.getItem("mhkh_user_name");
const savedEmail = localStorage.getItem("mhkh_user_email");
if (savedName) {
  $("#brandName").textContent = savedName;
  $("#brandEmail").textContent = savedEmail || "";
}

function showLogin() {
  document.body.classList.add("logged-out");
  $("#loginPage").classList.add("show");
  localStorage.removeItem("mhkh_logged_in");
  localStorage.removeItem("mhkh_user_name");
  localStorage.removeItem("mhkh_user_email");
  localStorage.removeItem("mhkh_user_uid");
  localStorage.removeItem("mhkh_user_role");
  $("#brandName").textContent = "Admin";
  $("#brandEmail").textContent = "";
}

function hideLogin() {
  document.body.classList.remove("logged-out");
  $("#loginPage").classList.remove("show");
  localStorage.setItem("mhkh_logged_in", "1");
}

// 如果 localStorage 有标记，先隐藏登录页避免闪烁
if (localStorage.getItem("mhkh_logged_in")) {
  document.body.classList.remove("logged-out");
  $("#loginPage").classList.remove("show");
}

let loginSubmitting = false;
$("#passwordToggle")?.addEventListener("click", () => {
  const input = $("#loginPassword");
  const btn = $("#passwordToggle");
  const visible = input.type === "text";
  input.type = visible ? "password" : "text";
  btn.title = visible ? "显示密码" : "隐藏密码";
  btn.setAttribute("aria-label", btn.title);
  btn.textContent = visible ? "👁" : "🙈";
});

$("#loginForm")?.addEventListener("submit", async (event) => {
  event.preventDefault();
  if (loginSubmitting) return;
  const form = event.currentTarget;
  if (!validateForm(form)) return;
  const name = form.name.value.trim();
  const password = form.password.value;
  const btn = form.querySelector('button[type="submit"]');
  $("#loginError").textContent = "";
  loginSubmitting = true;
  btn.textContent = "登录中...";
  btn.disabled = true;
  try {
    const data = await api("/api/login", { method: "POST", body: JSON.stringify({ name, password }) });
    form.reset();
    hideLogin();
    $("#brandName").textContent = data.name;
    $("#brandEmail").textContent = data.email || "";
    localStorage.setItem("mhkh_user_name", data.name);
    localStorage.setItem("mhkh_user_email", data.email || "");
    localStorage.setItem("mhkh_user_uid", data.uid || "");
    localStorage.setItem("mhkh_user_role", data.role || "");
    toast("登录成功", "success");
    await refreshCurrent();
    if (currentView === "dashboard") {
      startAutoRefresh();
      const el = $("#lastRefreshTime");
      if (el) el.textContent = `最后刷新 ${new Date().toLocaleTimeString("zh-CN")}`;
    }
  } catch (err) {
    $("#loginError").textContent = err.message || "登录失败";
  } finally {
    loginSubmitting = false;
    btn.textContent = "登录";
    btn.disabled = false;
  }
});

$("#logoutBtn")?.addEventListener("click", async () => {
  try { await api("/api/logout", { method: "POST" }); } catch {}
  showLogin();
});

(async () => {
  try {
    const res = await fetch("/api/summary", { headers: { "Content-Type": "application/json" } });
    if (res.ok) {
      hideLogin();
      await refreshCurrent();
      if (currentView === "dashboard") {
        startAutoRefresh();
        const el = $("#lastRefreshTime");
        if (el) el.textContent = `最后刷新 ${new Date().toLocaleTimeString("zh-CN")}`;
      }
    } else if (res.status === 401) {
      showLogin();
    } else if (res.status === 403) {
      if (localStorage.getItem("mhkh_logged_in")) {
        hideLogin();
        toast("当前账号权限不足或登录校验暂时失败，请稍后刷新", "error");
      } else {
        showLogin();
      }
    } else if (localStorage.getItem("mhkh_logged_in")) {
      hideLogin();
      toast("后台数据加载失败，请检查服务状态", "error");
    } else {
      showLogin();
    }
  } catch {
    if (localStorage.getItem("mhkh_logged_in")) hideLogin();
  }
})();
// ---- AI Assistant ----
let aiPendingAction = null;
let aiCurrentSessionId = 0;
let aiTypewriterTimer = null;
let aiSending = false;

function setAISending(sending) {
  aiSending = sending;
  const input = document.getElementById("aiInput");
  const sendBtn = document.getElementById("aiSendBtn");
  const confirmBtn = document.getElementById("aiConfirmAction");
  const cancelBtn = document.getElementById("aiCancelAction");
  if (input) input.disabled = sending;
  if (sendBtn) {
    sendBtn.disabled = sending;
    sendBtn.textContent = sending ? "发送中..." : "发送";
  }
  if (confirmBtn) confirmBtn.disabled = sending;
  if (cancelBtn) cancelBtn.disabled = sending;
}

function renderMarkdown(text) {
  if (!text) return "";
  const codeBlocks = [];
  const inlineCodes = [];

  let src = text.replace(/```(\w*)\n?([\s\S]*?)```/g, (_, _lang, code) => {
    codeBlocks.push(code.replace(/\n$/, ""));
    return `CB${codeBlocks.length - 1}`;
  });
  src = src.replace(/`([^`\n]+)`/g, (_, code) => {
    inlineCodes.push(code);
    return `IC${inlineCodes.length - 1}`;
  });

  let s = escapeHtml(src);

  const lines = s.split("\n");
  const out = [];
  let inList = false;
  for (const line of lines) {
    const li = line.match(/^[ \t]*(?:[-*]|\d+\.)[ \t]+(.+)$/);
    if (li) {
      if (!inList) { out.push("<ul>"); inList = true; }
      out.push(`<li>${li[1]}</li>`);
      continue;
    }
    if (inList) { out.push("</ul>"); inList = false; }
    let m;
    if ((m = line.match(/^###\s+(.+)$/))) { out.push(`<h5>${m[1]}</h5>`); continue; }
    if ((m = line.match(/^##\s+(.+)$/))) { out.push(`<h4>${m[1]}</h4>`); continue; }
    if ((m = line.match(/^#\s+(.+)$/))) { out.push(`<h3>${m[1]}</h3>`); continue; }
    out.push(line);
  }
  if (inList) out.push("</ul>");
  s = out.join("\n");

  s = s.replace(/\*\*([^*\n]+?)\*\*/g, "<strong>$1</strong>");
  s = s.replace(/(^|[^*])\*([^*\n]+?)\*(?!\*)/g, "$1<em>$2</em>");
  s = s.replace(/\[([^\]\n]+)\]\((https?:\/\/[^)\s]+)\)/g,
    '<a href="$2" target="_blank" rel="noopener noreferrer">$1</a>');

  s = s.replace(/\n+(<(?:\/?ul|li|h[3-5]))/g, "$1");
  s = s.replace(/(<\/(?:ul|li|h[3-5])>)\n+/g, "$1");
  s = s.replace(/\n/g, "<br>");

  s = s.replace(/CB(\d+)/g, (_, i) =>
    `<pre class="md-code"><code>${escapeHtml(codeBlocks[Number(i)])}</code></pre>`);
  s = s.replace(/IC(\d+)/g, (_, i) =>
    `<code class="md-inline-code">${escapeHtml(inlineCodes[Number(i)])}</code>`);

  return s;
}

function showAIThinking() {
  const box = document.getElementById("aiMessages");
  if (!box) return null;
  const item = document.createElement("div");
  item.className = "ai-message assistant ai-thinking";
  item.innerHTML = `<div class="ai-thinking-dots"><span></span><span></span><span></span></div>`;
  box.appendChild(item);
  box.scrollTop = box.scrollHeight;
  return item;
}

async function appendAIMessage(role, content, result, opts = {}) {
  const box = document.getElementById("aiMessages");
  if (!box) return;
  const item = document.createElement("div");
  item.className = `ai-message ${role}`;
  const text = document.createElement("div");
  item.appendChild(text);
  box.appendChild(item);
  box.scrollTop = box.scrollHeight;
  if (!opts.instant && role === "assistant" && content) {
    await typewriterText(text, content, 18);
    text.innerHTML = renderMarkdown(content);
  } else if (role === "assistant" && content) {
    text.innerHTML = renderMarkdown(content);
  } else {
    text.textContent = content || "";
  }
  if (result !== undefined && result !== null) {
    const rendered = renderAIResult(result);
    if (rendered) item.appendChild(rendered);
  }
  box.scrollTop = box.scrollHeight;
}

function typewriterText(el, text, speed = 18) {
  return new Promise((resolve) => {
    if (aiTypewriterTimer) { clearInterval(aiTypewriterTimer); aiTypewriterTimer = null; }
    let i = 0;
    el.classList.add("ai-cursor");
    el.textContent = "";
    aiTypewriterTimer = setInterval(() => {
      i++;
      el.textContent = text.slice(0, i);
      const box = document.getElementById("aiMessages");
      if (box) box.scrollTop = box.scrollHeight;
      if (i >= text.length) {
        clearInterval(aiTypewriterTimer);
        aiTypewriterTimer = null;
        el.classList.remove("ai-cursor");
        resolve();
      }
    }, speed);
  });
}

function renderAIResult(result) {
  if (result === null || result === undefined) return null;
  if (typeof result === "object" && !Array.isArray(result)) {
    const keys = Object.keys(result);
    if ("ok" in result && (keys.length === 1 || (keys.length === 2 && "id" in result))) {
      const tag = document.createElement("div");
      tag.className = `ai-result-tag ${result.ok ? "ok" : "fail"}`;
      if (result.ok) {
        tag.textContent = "id" in result ? `✓ 已创建 (ID ${result.id})` : "✓ 执行成功";
      } else {
        tag.textContent = "✗ 执行失败";
      }
      return tag;
    }
    return buildAITable([result]);
  }
  if (Array.isArray(result)) {
    if (result.length === 0) {
      const empty = document.createElement("div");
      empty.className = "ai-empty-state";
      empty.textContent = "无匹配数据";
      return empty;
    }
    if (typeof result[0] === "object" && result[0] !== null) {
      return buildAITable(result);
    }
  }
  const pre = document.createElement("pre");
  pre.textContent = JSON.stringify(result, null, 2);
  return pre;
}

function buildAITable(rows) {
  const wrap = document.createElement("div");
  wrap.style.overflowX = "auto";
  wrap.style.marginTop = "8px";
  const keys = Object.keys(rows[0]);
  const tbl = document.createElement("table");
  tbl.className = "ai-result-table";
  const thead = document.createElement("thead");
  const trh = document.createElement("tr");
  for (const k of keys) {
    const th = document.createElement("th");
    th.textContent = k;
    trh.appendChild(th);
  }
  thead.appendChild(trh);
  tbl.appendChild(thead);
  const tbody = document.createElement("tbody");
  for (const r of rows) {
    const tr = document.createElement("tr");
    for (const k of keys) {
      const td = document.createElement("td");
      let v = r[k];
      if (v === null || v === undefined) v = "";
      else if (typeof v === "object") v = JSON.stringify(v);
      td.textContent = String(v);
      tr.appendChild(td);
    }
    tbody.appendChild(tr);
  }
  tbl.appendChild(tbody);
  wrap.appendChild(tbl);
  return wrap;
}

function setAIPending(action, text) {
  aiPendingAction = action || null;
  const box = document.getElementById("aiPendingBox");
  if (!box) return;
  if (!action) {
    box.classList.remove("show");
    const pwd = document.getElementById("aiDeletePassword");
    if (pwd) pwd.value = "";
    return;
  }
  document.getElementById("aiPendingText").textContent = text || "请确认是否执行该操作。";
  const pwd = document.getElementById("aiDeletePassword");
  if (pwd) {
    pwd.value = "";
    pwd.style.display = action.name === "delete_user" ? "block" : "none";
  }
  box.classList.add("show");
}

async function sendAIMessage(confirm = false) {
  if (aiSending) return;
  const input = document.getElementById("aiInput");
  if (!input) return;
  const message = input.value.trim();
  if (!confirm && !message) return;
  if (!aiCurrentSessionId) aiCurrentSessionId = Date.now();
  if (!confirm) {
    await appendAIMessage("user", message, null, { instant: true });
    input.value = "";
  }
  const payload = confirm ? {
    session_id: aiCurrentSessionId,
    confirm: true,
    pending_action: aiPendingAction,
    delete_password: document.getElementById("aiDeletePassword")?.value || "",
  } : { session_id: aiCurrentSessionId, message };

  setAISending(true);
  const thinkingEl = showAIThinking();
  try {
    const data = await api("/api/ai/chat", { method: "POST", body: JSON.stringify(payload), skipLoading: true });
    if (thinkingEl && thinkingEl.parentNode) thinkingEl.remove();
    if (data.session_id) aiCurrentSessionId = data.session_id;
    await appendAIMessage("assistant", data.reply || "", data.result);
    if (data.requires_confirm && data.action) {
      setAIPending(data.action, data.reply);
    } else {
      setAIPending(null);
      if (data.action) handleClientAIAction(data.action);
    }
    if (data.result && currentView !== "ai") refreshCurrent().catch(() => {});
    loadAISessions().catch(() => {});
  } finally {
    if (thinkingEl && thinkingEl.parentNode) thinkingEl.remove();
    setAISending(false);
  }
}

function handleClientAIAction(action) {
  if (!action || !action.name) return;
  const args = action.args || {};
  switch (action.name) {
    case "switch_wallpaper":
      refreshBg();
      break;
    case "set_theme": {
      const mode = args.mode;
      if (mode === "toggle") {
        setTheme(document.body.classList.contains("dark") ? "light" : "dark");
      } else if (mode === "dark" || mode === "light") {
        setTheme(mode);
      }
      break;
    }
    case "download_wallpaper":
      document.getElementById("bgSaveBtn")?.click();
      break;
    case "upload_wallpaper":
      document.getElementById("customBgBtn")?.click();
      break;
    case "toggle_bg_preview":
      document.getElementById("bgViewBtn")?.click();
      break;
    case "navigate": {
      const view = String(args.view || "").toLowerCase();
      const allowed = ["dashboard", "users", "dynamics", "friends", "applies", "star", "notices", "ai"];
      if (allowed.includes(view)) {
        activateView(view);
        refreshCurrent().catch((err) => toast(err.message, "error"));
      } else {
        toast("未知页面：" + (args.view || ""), "error");
      }
      break;
    }
    case "refresh_view":
      refreshCurrent().catch((err) => toast(err.message, "error"));
      break;
    case "logout":
      (async () => {
        try { await api("/api/logout", { method: "POST" }); } catch {}
        if (typeof showLogin === "function") showLogin();
        else location.reload();
      })();
      break;
  }
}

async function loadAISessions() {
  const list = document.getElementById("aiSessionsList");
  if (!list) return;
  try {
    const sessions = await api("/api/ai/sessions", { skipLoading: true });
    if (!sessions || sessions.length === 0) {
      list.innerHTML = `<div class="ai-empty-state">还没有历史聊天</div>`;
      return;
    }
    list.innerHTML = sessions.map((s) => {
      const id = s.session_id;
      const rawTitle = (s.title || "新对话").toString();
      const title = rawTitle.length > 28 ? rawTitle.slice(0, 28) + "…" : rawTitle;
      const active = String(id) === String(aiCurrentSessionId) ? " active" : "";
      return `<div class="ai-session-item${active}" data-session-id="${id}" title="${escapeHtml(rawTitle)}">
        <span class="ai-session-title">${escapeHtml(title)}</span>
        <button class="ai-session-delete" data-delete-session="${id}" title="删除">✕</button>
      </div>`;
    }).join("");
  } catch (err) {
    list.innerHTML = `<div class="ai-empty-state">加载失败：${escapeHtml(err.message || "")}</div>`;
    console.error("loadAISessions error:", err);
  }
}

async function switchAISession(sessionId) {
  aiCurrentSessionId = Number(sessionId);
  setAIPending(null);
  const box = document.getElementById("aiMessages");
  if (box) box.innerHTML = "";
  try {
    const rows = await api(`/api/ai/sessions/${sessionId}/messages`, { skipLoading: true });
    if (!rows || rows.length === 0) {
      await appendAIMessage("assistant", "（此会话暂无消息）", null, { instant: true });
    } else {
      for (const m of rows) {
        let result = null;
        if (m.result_json) {
          try { result = typeof m.result_json === "string" ? JSON.parse(m.result_json) : m.result_json; } catch {}
        }
        await appendAIMessage(m.role, m.content || "", result, { instant: true });
      }
    }
    loadAISessions().catch(() => {});
  } catch (err) {
    toast(err.message || "加载失败", "error");
  }
}

function newAISession() {
  aiCurrentSessionId = 0;
  setAIPending(null);
  const box = document.getElementById("aiMessages");
  if (box) {
    box.innerHTML = `<div class="ai-message assistant">你好，我可以帮你查询用户、查询动态、审核/隐藏动态、发送通知，也能切换壁纸和主题。删除操作会要求二次确认。</div>`;
  }
  loadAISessions().catch(() => {});
}

async function deleteAISession(sessionId) {
  if (!await customConfirm("确定删除该聊天记录吗？")) return;
  try {
    await api(`/api/ai/sessions/${sessionId}`, { method: "DELETE", skipLoading: true });
    if (Number(sessionId) === Number(aiCurrentSessionId)) {
      newAISession();
    } else {
      loadAISessions().catch(() => {});
    }
    toast("已删除", "success");
  } catch (err) {
    toast(err.message || "删除失败", "error");
  }
}

document.getElementById("aiSendBtn")?.addEventListener("click", () => {
  sendAIMessage(false).catch((err) => appendAIMessage("assistant", err.message, null, { instant: true }));
});

document.getElementById("aiInput")?.addEventListener("keydown", (e) => {
  if (e.key === "Enter" && !e.shiftKey && !e.isComposing) {
    e.preventDefault();
    sendAIMessage(false).catch((err) => appendAIMessage("assistant", err.message, null, { instant: true }));
  }
});

document.getElementById("aiConfirmAction")?.addEventListener("click", () => {
  if (!aiPendingAction) return;
  sendAIMessage(true).catch((err) => appendAIMessage("assistant", err.message, null, { instant: true }));
});

document.getElementById("aiCancelAction")?.addEventListener("click", () => {
  setAIPending(null);
  appendAIMessage("assistant", "已取消本次操作。", null, { instant: true });
});

document.getElementById("aiNewChatBtn")?.addEventListener("click", () => {
  newAISession();
});

document.getElementById("aiSessionsList")?.addEventListener("click", (e) => {
  const delBtn = e.target.closest("[data-delete-session]");
  if (delBtn) {
    e.stopPropagation();
    deleteAISession(delBtn.dataset.deleteSession);
    return;
  }
  const item = e.target.closest("[data-session-id]");
  if (item) {
    switchAISession(item.dataset.sessionId);
  }
});













