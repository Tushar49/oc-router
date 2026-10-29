// Page: usage history — metadata-only token and cost analytics from SQLite.
import { api } from "../api.js";
import { t, onChange } from "../i18n.js";
import { $, el, emptyRow, formatNumber, localTime, toast } from "../ui.js";

const state = {
  range: "7d",
  groupBy: "provider_model",
  sort: "total_tokens",
  provider: "",
  model: "",
  endpoint: "",
  status: "",
  loading: false,
  summary: null,
  points: [],
  events: [],
  error: "",
};

const RANGE_DAYS = { "24h": 1, "7d": 7, "30d": 30, "90d": 90, "365d": 365 };

function ensurePage() {
  if ($("#page-usage-history")) return;
  const group = document.querySelector(".nav-group");
  if (group && !group.querySelector('[data-page="usage-history"]')) {
    const button = el("button", {
      class: "nav-item",
      dataset: { page: "usage-history" },
      "data-i18n": "nav.usage-history",
      text: t("用量历史", "nav.usage-history"),
    });
    const diagnostics = group.querySelector('[data-page="diagnostics"]');
    if (diagnostics) diagnostics.after(button);
    else group.append(button);
  }
  const section = el("section", { id: "page-usage-history", class: "page hidden" });
  section.innerHTML = `
    <div class="filters">
      <select id="uh-range" aria-label="range">
        <option value="24h">24 小时</option>
        <option value="7d" selected>7 天</option>
        <option value="30d">30 天</option>
        <option value="90d">90 天</option>
        <option value="365d">365 天</option>
      </select>
      <select id="uh-group" aria-label="group">
        <option value="provider_model" selected>按供应商 / 模型</option>
        <option value="model">按模型</option>
        <option value="provider">按供应商</option>
        <option value="endpoint">按协议</option>
        <option value="day">按日期</option>
        <option value="server_key">按本地 Key</option>
      </select>
      <select id="uh-sort" aria-label="sort">
        <option value="total_tokens" selected>按 Token 排序</option>
        <option value="requests">按请求数排序</option>
        <option value="cost">按成本排序</option>
        <option value="input_tokens">按输入 Token 排序</option>
        <option value="output_tokens">按输出 Token 排序</option>
      </select>
      <div class="key-actions">
        <button id="uh-refresh" class="btn btn-sm" type="button">刷新</button>
        <button id="uh-csv" class="btn btn-sm btn-ghost" type="button">CSV</button>
        <button id="uh-json" class="btn btn-sm btn-ghost" type="button">JSON</button>
        <button id="uh-backup" class="btn btn-sm btn-ghost" type="button">备份</button>
      </div>
    </div>
    <div class="filters">
      <select id="uh-provider" aria-label="provider">
        <option value="">全部供应商</option>
        <option value="opencode">opencode</option>
        <option value="opencode-go">opencode-go</option>
        <option value="anonymous">anonymous</option>
      </select>
      <input id="uh-model" type="text" placeholder="模型（精确）" autocomplete="off" />
      <select id="uh-endpoint" aria-label="endpoint">
        <option value="">全部协议</option>
        <option value="chat">Chat</option>
        <option value="responses">Responses</option>
        <option value="messages">Messages</option>
        <option value="systemone">System One</option>
      </select>
      <select id="uh-status" aria-label="status">
        <option value="">全部状态</option>
        <option value="200">200</option>
        <option value="401">401</option>
        <option value="429">429</option>
        <option value="500">500</option>
        <option value="502">502</option>
      </select>
    </div>
    <div id="uh-cards" class="metrics"></div>
    <section class="section">
      <div class="section-head">
        <div>
          <h2 class="section-title">每日 Token 与成本</h2>
          <p class="section-caption">来自 UTC 日汇总；悬停查看当天数据。</p>
        </div>
      </div>
      <div id="uh-chart" class="trend"></div>
      <div id="uh-chart-meta" class="trend-meta"></div>
    </section>
    <section class="section">
      <div class="section-head">
        <div>
          <h2 class="section-title">分组汇总</h2>
          <p id="uh-status" class="section-caption"></p>
        </div>
      </div>
      <div class="table-wrap">
        <table class="table">
          <thead>
            <tr>
              <th>分组</th><th>请求</th><th>输入</th><th>缓存读</th><th>缓存写</th>
              <th>输出</th><th>推理</th><th>总计</th><th>成本</th><th>用量</th><th>价格</th>
            </tr>
          </thead>
          <tbody id="uh-groups"></tbody>
        </table>
      </div>
    </section>
    <section class="section">
      <div class="section-head">
        <div>
          <h2 class="section-title">最近请求</h2>
          <p class="section-caption">仅元数据：无提示词、响应或工具参数。</p>
        </div>
      </div>
      <div class="table-wrap">
        <table class="table">
          <thead>
            <tr>
              <th>时间</th><th>模型</th><th>协议</th><th>状态</th><th>Token</th><th>成本</th><th>结果</th>
            </tr>
          </thead>
          <tbody id="uh-events"></tbody>
        </table>
      </div>
    </section>`;
  $("#main").append(section);

  $("#uh-range").addEventListener("change", (event) => {
    state.range = event.target.value;
    void load();
  });
  $("#uh-group").addEventListener("change", (event) => {
    state.groupBy = event.target.value;
    void load();
  });
  $("#uh-sort").addEventListener("change", (event) => {
    state.sort = event.target.value;
    void load();
  });
  for (const [id, key] of [
    ["uh-provider", "provider"],
    ["uh-endpoint", "endpoint"],
    ["uh-status", "status"],
  ]) {
    $(`#${id}`).addEventListener("change", (event) => {
      state[key] = event.target.value;
      void load();
    });
  }
  $("#uh-model").addEventListener("change", (event) => {
    state.model = event.target.value.trim();
    void load();
  });
  $("#uh-refresh").addEventListener("click", () => void load());
  $("#uh-csv").addEventListener("click", () => download("csv"));
  $("#uh-json").addEventListener("click", () => download("json"));
  $("#uh-backup").addEventListener("click", () => void backup());
}

function windowRange() {
  const days = RANGE_DAYS[state.range] || 7;
  const to = new Date();
  const from = new Date(to.getTime() - days * 86400000);
  return { from, to };
}

function applyFilters(params) {
  if (state.provider) params.set("provider", state.provider);
  if (state.model) params.set("model", state.model);
  if (state.endpoint) params.set("endpoint", state.endpoint);
  if (state.status) params.set("status", state.status);
}

async function load() {
  if (state.loading) return;
  state.loading = true;
  const { from, to } = windowRange();
  const summaryParams = new URLSearchParams({
    from: from.toISOString(),
    to: to.toISOString(),
    group_by: state.groupBy,
    sort: state.sort,
    order: "desc",
  });
  const eventParams = new URLSearchParams({
    from: from.toISOString(),
    to: to.toISOString(),
    limit: "20",
  });
  applyFilters(summaryParams);
  applyFilters(eventParams);
  try {
    const [summary, series, events] = await Promise.all([
      api(`/api/usage/summary?${summaryParams}`),
      api(`/api/usage/timeseries?${summaryParams}`),
      api(`/api/usage/events?${eventParams}`),
    ]);
    state.summary = summary;
    state.points = series.points || [];
    state.events = events.events || [];
    state.error = "";
  } catch (error) {
    state.summary = null;
    state.points = [];
    state.events = [];
    state.error = error.message;
  } finally {
    state.loading = false;
    render();
  }
}

function render() {
  ensurePage();
  const summary = state.summary;
  const totals = summary?.totals;
  const cards = $("#uh-cards");
  cards.replaceChildren(
    metric("请求数", totals ? formatNumber(totals.requests) : "-", totals ? `${formatNumber(totals.success)} 成功` : ""),
    metric("输入 Token", totals ? formatNumber(totals.input_tokens) : "-", "不含缓存"),
    metric("缓存读 Token", totals ? formatNumber(totals.cached_tokens) : "-", "provider 回报的缓存命中"),
    metric("缓存写 Token", totals ? formatNumber(totals.cache_write_tokens) : "-", "缓存创建 / 写入"),
    metric("输出 Token", totals ? formatNumber(totals.output_tokens) : "-", "包含推理 Token"),
    metric("推理 Token", totals ? formatNumber(totals.reasoning_tokens) : "-", "输出的子集，不重复计入"),
    metric("Token 总量", totals ? formatNumber(totals.total_tokens) : "-", "按规范求和"),
    metric("估算成本", totals ? usd(totals.estimated_cost_usd) : "-", "整数微美元"),
    metric("用量覆盖率", totals ? pct(totals.usage_coverage) : "-", "provider 回报 / 请求"),
    metric("成本覆盖率", totals ? pct(totals.cost_coverage) : "-", "已知价格 / 请求"),
  );
  renderChart();
  renderGroups();
  renderEvents();
  const status = $("#uh-status");
  if (state.error) status.textContent = state.error;
  else if (summary?.truncated) status.textContent = "结果已截断，请缩小范围或增加过滤条件。";
  else status.textContent = summary ? `来源：${summary.source} · 时区：UTC` : "";
}

function renderChart() {
  const chart = $("#uh-chart");
  if (!chart) return;
  chart.replaceChildren();
  const points = state.points.slice(-90);
  const maximum = Math.max(1, ...points.map((point) => point.total_tokens || 0));
  points.forEach((point) => {
    const bar = el("div", { class: "trend-bar" });
    bar.style.height = `${Math.max(2, Math.round(((point.total_tokens || 0) / maximum) * 100))}%`;
    bar.title = `${point.day} · ${formatNumber(point.total_tokens)} tokens · ${usd(point.estimated_cost_usd)}`;
    chart.append(bar);
  });
  const meta = $("#uh-chart-meta");
  if (!points.length) {
    meta.textContent = state.error || "暂无日汇总数据。";
    return;
  }
  meta.textContent = `${points[0].day} → ${points[points.length - 1].day} · 峰值 ${formatNumber(maximum)} tokens`;
}

function renderGroups() {
  const host = $("#uh-groups");
  if (!host) return;
  host.replaceChildren();
  const groups = state.summary?.groups || [];
  groups.forEach((group) => {
    host.append(
      el("tr", {}, [
        el("td", { class: "mono", text: groupLabel(group) }),
        el("td", { class: "num", text: formatNumber(group.requests) }),
        el("td", { class: "num", text: formatNumber(group.input_tokens) }),
        el("td", { class: "num", text: formatNumber(group.cached_tokens) }),
        el("td", { class: "num", text: formatNumber(group.cache_write_tokens) }),
        el("td", { class: "num", text: formatNumber(group.output_tokens) }),
        el("td", { class: "num", text: formatNumber(group.reasoning_tokens) }),
        el("td", { class: "num", text: formatNumber(group.total_tokens) }),
        el("td", { class: "num", text: usd(group.estimated_cost_usd) }),
        el("td", { text: group.usage_source || "-" }),
        el("td", { text: group.pricing_source || "-" }),
      ]),
    );
  });
  if (!groups.length) emptyRow(host, 11, state.error || "该范围内没有用量。");
}

function renderEvents() {
  const host = $("#uh-events");
  if (!host) return;
  host.replaceChildren();
  for (const event of state.events) {
    host.append(
      el("tr", {}, [
        el("td", { class: "mono", text: localTime(event.ts) }),
        el("td", { class: "mono", text: event.model }),
        el("td", { text: event.endpoint }),
        el("td", { class: "num", text: String(event.status) }),
        el("td", { class: "num", text: formatNumber(event.total_tokens) }),
        el("td", { class: "num", text: usd(event.cost_micro_usd == null ? null : event.cost_micro_usd / 1e6) }),
        el("td", { text: event.outcome }),
      ]),
    );
  }
  if (!state.events.length) emptyRow(host, 7, state.error || "没有最近请求。");
}

function metric(label, value, note) {
  return el("div", { class: "metric" }, [
    el("div", { class: "metric-label", text: label }),
    el("div", { class: "metric-value", text: value }),
    el("div", { class: "metric-note", text: note }),
  ]);
}

function groupLabel(group) {
  if (group.day) return group.day;
  if (group.server_key_fp) return group.server_key_fp;
  if (group.endpoint) return group.endpoint;
  return [group.provider, group.model].filter(Boolean).join(" / ") || "-";
}

function pct(value) {
  return `${((Number(value) || 0) * 100).toFixed(1)}%`;
}

function usd(value) {
  if (value == null) return "—";
  const amount = Number(value);
  if (Number.isNaN(amount)) return "—";
  if (amount === 0) return "$0.00";
  return `$${amount.toFixed(amount < 0.01 ? 6 : 4)}`;
}

function download(format) {
  const { from, to } = windowRange();
  const params = new URLSearchParams({
    format,
    from: from.toISOString(),
    to: to.toISOString(),
  });
  applyFilters(params);
  window.location.assign(`/api/usage/export?${params}`);
}

async function backup() {
  try {
    const result = await api("/api/usage/backup", { method: "POST", body: "{}" });
    toast(`备份已创建：${result.path}`);
  } catch (error) {
    toast(error.message, true);
  }
}

export default {
  id: "usage-history",
  title: "用量历史",
  sub: "按供应商、模型和日期统计 Token、成本与覆盖率。",
  init() {
    ensurePage();
    onChange(() => render());
  },
  show() {
    void load();
  },
  refresh() {
    void load();
  },
};
