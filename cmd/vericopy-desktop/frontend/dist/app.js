const bridge = () => window.go?.main?.Bridge;

async function invoke(method, ...args) {
  const target = bridge();
  if (!target || typeof target[method] !== "function") {
    throw new Error("The desktop bridge is unavailable. Launch Vericopy as the desktop application, not in a browser.");
  }
  return target[method](...args);
}

const $ = (selector) => document.querySelector(selector);
const els = {
  tabs: [...document.querySelectorAll("[data-view]")],
  panels: [...document.querySelectorAll("[data-view-panel]")],
  form: $("#transfer-form"),
  source: $("#source"),
  destination: $("#destination"),
  port: $("#port"),
  permissions: $("#permissions"),
  authRadios: [...document.querySelectorAll('input[name="authentication"]')],
  authOptions: [...document.querySelectorAll(".auth-option")],
  keyAuthPanel: $("#key-auth-panel"),
  passwordAuthPanel: $("#password-auth-panel"),
  password: $("#password"),
  togglePassword: $("#toggle-password"),
  identity: $("#identity"),
  knownHosts: $("#known-hosts"),
  group: $("#group"),
  readableBy: $("#readable-by"),
  recursive: $("#recursive"),
  resume: $("#resume"),
  overwrite: $("#overwrite"),
  preserveTime: $("#preserve-time"),
  sessionChips: $("#session-chips"),
  sessionName: $("#session-name"),
  saveSession: $("#save-session"),
  sessionsList: $("#sessions-list"),
  sessionsEmpty: $("#sessions-empty"),
  dashboardJobsList: $("#dashboard-jobs-list"),
  dashboardJobsEmpty: $("#dashboard-jobs-empty"),
  jobsList: $("#jobs-list"),
  jobsEmpty: $("#jobs-empty"),
  queueRunning: $("#queue-running"),
  queueWaiting: $("#queue-waiting"),
  queueCapacity: $("#queue-capacity"),
  managerNotice: $("#manager-notice"),
  clearFinishedJobs: $("#clear-finished-jobs"),
  historyList: $("#history-list"),
  historyEmpty: $("#history-empty"),
  clearHistory: $("#clear-history"),
  advancedToggle: $("#advanced-toggle"),
  advanced: $("#advanced"),
  reviewButton: $("#review-button"),
  startButton: $("#start-transfer"),
  notice: $("#notice"),
  reviewPanel: $("#review-panel"),
  reviewTitle: $("#review-title"),
  reviewList: $("#review-list"),
  reviewSecurity: $("#review-security"),
  progressPanel: $("#progress-panel"),
  progressPhase: $("#progress-phase"),
  progressDetail: $("#progress-detail"),
  progressFill: $("#progress-fill"),
  progressBar: document.querySelector(".bar"),
  progressFile: $("#progress-file"),
  themeToggle: $("#theme-toggle"),
  statusEngine: $("#status-engine"),
  statusAgent: $("#status-agent"),
  statusVersion: $("#status-version"),
  statusJobs: $("#status-jobs"),
};

let sessions = [];
let selectedSession = "";
let reviewedRequest = null;
let lastHistory = [];
let transferQueue = { jobs: [], running: 0, queued: 0, max_concurrent: 2 };
let retryingJobID = "";
let jobRefreshTimer = null;

/* ---------- theme ---------- */

function systemTheme() {
  return window.matchMedia?.("(prefers-color-scheme: dark)")?.matches ? "dark" : "light";
}
function activeTheme() {
  return document.documentElement.dataset.theme || systemTheme();
}
function updateThemeToggle() {
  const dark = activeTheme() === "dark";
  els.themeToggle.setAttribute("aria-pressed", String(dark));
  els.themeToggle.setAttribute("aria-label", dark ? "Switch to light mode" : "Switch to dark mode");
}
function setTheme(theme, persist = true) {
  document.documentElement.dataset.theme = theme;
  if (persist) {
    try { window.localStorage.setItem("vericopy.theme", theme); } catch { /* presentational only */ }
  }
  updateThemeToggle();
}
function initializeTheme() {
  try {
    const saved = window.localStorage.getItem("vericopy.theme");
    if (saved === "light" || saved === "dark") { setTheme(saved, false); return; }
  } catch { /* fall back to OS preference */ }
  updateThemeToggle();
}

/* ---------- views ---------- */

function showView(view) {
  els.tabs.forEach((button) => {
    const active = button.dataset.view === view;
    button.classList.toggle("is-active", active);
    if (active) button.setAttribute("aria-current", "page");
    else button.removeAttribute("aria-current");
  });
  els.panels.forEach((panel) => panel.classList.toggle("is-visible", panel.dataset.viewPanel === view));
  if (view === "dashboard") { loadDashboard(); renderSessions(); loadTransferJobs(); loadHistory(); }
  if (view === "transfer") els.source.focus();
  if (view === "activity") { loadTransferJobs(); loadHistory(); }
}

/* ---------- notices ---------- */

function setNotice(message, kind = "") {
  els.notice.hidden = !message;
  els.notice.className = `notice${kind ? ` is-${kind}` : ""}`;
  els.notice.textContent = message || "";
}

/* ---------- form <-> data ---------- */

function requestFromForm() {
  const authentication = els.authRadios.find((radio) => radio.checked)?.value || "key";
  return {
    source: els.source.value.trim(),
    destination: els.destination.value.trim(),
    port: Number(els.port.value || 22),
    permissions: els.permissions.value,
    authentication,
    identity: authentication === "key" ? els.identity.value.trim() : "",
    known_hosts: els.knownHosts.value.trim(),
    group: els.group.value.trim(),
    readable_by: els.readableBy.value.trim(),
    recursive: els.recursive.checked,
    resume: els.resume.checked,
    overwrite: els.overwrite.checked,
    preserve_time: els.preserveTime.checked,
  };
}

function requestToForm(request) {
  els.source.value = request.source || "";
  els.destination.value = request.destination || "";
  els.port.value = request.port || "";
  els.permissions.value = request.permissions || "private";
  setAuthentication(request.authentication || "key");
  els.identity.value = request.identity || "";
  els.knownHosts.value = request.known_hosts || "";
  els.group.value = request.group || "";
  els.readableBy.value = request.readable_by || "";
  els.recursive.checked = !!request.recursive;
  els.resume.checked = !!request.resume;
  els.overwrite.checked = !!request.overwrite;
  els.preserveTime.checked = !!request.preserve_time;
  setAdvancedOpen(!!(request.known_hosts || request.group || request.readable_by));
}

function setAuthentication(authentication, clearPassword = true) {
  const method = authentication === "password" ? "password" : "key";
  els.authRadios.forEach((radio) => { radio.checked = radio.value === method; });
  els.authOptions.forEach((option) => {
    option.classList.toggle("is-selected", option.querySelector("input")?.value === method);
  });
  els.keyAuthPanel.hidden = method !== "key";
  els.passwordAuthPanel.hidden = method !== "password";
  if (method !== "password" && clearPassword) {
    els.password.value = "";
    els.password.type = "password";
    els.togglePassword.textContent = "Show";
    els.togglePassword.setAttribute("aria-pressed", "false");
  }
}

function setAdvancedOpen(open) {
  els.advanced.hidden = !open;
  els.advancedToggle.setAttribute("aria-expanded", String(open));
  els.advancedToggle.innerHTML = open ? "Advanced options &#9652;" : "Advanced options &#9662;";
}

/* ---------- sessions (full form, local to this computer) ---------- */

async function loadSessionsFromDisk() {
  try {
    sessions = await invoke("ListSessions") || [];
  } catch {
    sessions = [];
  }
}

function sessionFromForm(name) {
  return { name, updated_at: new Date().toISOString(), ...requestFromForm() };
}

function applySession(session) {
  requestToForm(session);
  els.sessionName.value = session.name;
  selectedSession = session.name;
  retryingJobID = "";
  invalidateReview();
  renderSessions();
  setNotice(`Loaded "${session.name}".`, "success");
}

async function saveSession() {
  const name = els.sessionName.value.trim();
  if (!name) {
    setNotice("Name the session first.", "error");
    els.sessionName.focus();
    return;
  }
  els.saveSession.disabled = true;
  try {
    const saved = await invoke("SaveSession", sessionFromForm(name));
    sessions = sessions.filter((session) => session.name !== saved.name).concat([saved]);
    selectedSession = saved.name;
    els.sessionName.value = saved.name;
    renderSessions();
    setNotice("Session saved on this computer.", "success");
  } catch (error) {
    setNotice(error?.message || String(error), "error");
  } finally {
    els.saveSession.disabled = false;
  }
}

async function deleteSession(name) {
  try {
    await invoke("DeleteSession", name);
    sessions = sessions.filter((session) => session.name !== name);
    if (selectedSession === name) selectedSession = "";
    renderSessions();
  } catch (error) {
    setNotice(error?.message || String(error), "error");
  }
}

function renderSessions() {
  els.sessionChips.replaceChildren(...sessions.map((session) => {
    const chip = document.createElement("button");
    chip.type = "button";
    chip.className = `chip${session.name === selectedSession ? " is-active" : ""}`;
    chip.textContent = session.name;
    chip.addEventListener("click", () => applySession(session));
    return chip;
  }));

  els.sessionsList.replaceChildren(...sessions.map((session) => {
    const card = document.createElement("article");
    card.className = "session-card";
    const body = document.createElement("div");
    body.className = "body";
    const name = document.createElement("div");
    name.className = "name";
    name.textContent = session.name;
    const summary = document.createElement("div");
    summary.className = "summary";
    summary.textContent = `${session.source}  \u2192  ${session.destination} · port ${session.port || 22}`;
    body.append(name, summary);
    const load = document.createElement("button");
    load.type = "button";
    load.className = "btn btn-secondary btn-sm";
    load.textContent = "Load";
    load.addEventListener("click", () => { applySession(session); showView("transfer"); });
    const remove = document.createElement("button");
    remove.type = "button";
    remove.className = "btn btn-quiet btn-sm";
    remove.textContent = "Delete";
    remove.setAttribute("aria-label", `Delete session ${session.name}`);
    remove.addEventListener("click", () => deleteSession(session.name));
    card.append(body, load, remove);
    return card;
  }));
  els.sessionsEmpty.hidden = sessions.length > 0;
}

/* One-time migration from the old non-secret connection profiles. */
async function migrateProfilesOnce() {
  if (sessions.length > 0) return;
  let migrated = false;
  try {
    const profiles = await invoke("ListProfiles");
    for (const profile of profiles || []) {
      if (sessions.some((s) => s.name === profile.name)) continue;
      const savedSession = await invoke("SaveSession", {
        name: profile.name,
        updated_at: profile.updated_at,
        source: "",
        destination: profile.destination,
        port: profile.port,
        permissions: "private",
        authentication: "key",
        identity: "",
        known_hosts: profile.known_hosts || "",
        group: "",
        readable_by: "",
        recursive: false,
        resume: true,
        overwrite: false,
        preserve_time: false,
      });
      sessions.push(savedSession);
      migrated = true;
    }
  } catch { /* bridge unavailable or no profiles */ }
  if (migrated) renderSessions();
}

async function initializeSessions() {
  await loadSessionsFromDisk();
  await migrateProfilesOnce();
  renderSessions();
}

/* ---------- review + transfer ---------- */

function addReviewRow(label, value) {
  const row = document.createElement("div");
  const term = document.createElement("dt");
  const definition = document.createElement("dd");
  term.textContent = label;
  definition.textContent = value;
  row.append(term, definition);
  els.reviewList.append(row);
}

function displayReview(review) {
  els.reviewList.replaceChildren();
  addReviewRow("Source", review.source.path);
  addReviewRow("Type", review.source.is_directory ? "Directory tree" : "Regular file");
  addReviewRow("Destination", `${review.destination.user}@${review.destination.host}:${review.destination.path}`);
  addReviewRow("Port", String(review.destination.port));
  addReviewRow("Authentication", review.authentication === "password" ? "One-time SSH password" : "SSH key or agent");
  addReviewRow("Policy", review.permissions);
  addReviewRow("known_hosts", review.known_hosts);
  const options = [
    review.resume && "resume",
    review.overwrite && "overwrite",
    review.preserve_time && "preserve mtime",
  ].filter(Boolean).join(" · ") || "none";
  addReviewRow("Options", options);
  if (review.readable_by) addReviewRow("Access check", `read as ${review.readable_by}`);
  els.reviewPanel.hidden = false;
  els.reviewSecurity.textContent = review.authentication === "password"
    ? "strict known_hosts · one-time password · sha-256 readback"
    : "strict known_hosts · key authentication · sha-256 readback";
  els.reviewTitle.textContent = retryingJobID ? "Ready to retry · no connection opened yet" : "Ready · no connection opened yet";
  els.startButton.textContent = retryingJobID ? "Retry transfer" : "Add to queue";
  els.startButton.disabled = false;
}

function invalidateReview() {
  reviewedRequest = null;
  els.startButton.disabled = true;
  els.reviewPanel.hidden = true;
  els.progressPanel.hidden = true;
  setNotice("");
}

async function reviewTransfer() {
  const request = requestFromForm();
  if (!request.source) { setNotice("Source is required.", "error"); els.source.focus(); return; }
  if (!request.destination) { setNotice("Destination is required.", "error"); els.destination.focus(); return; }
  if (request.authentication === "password" && !els.password.value) {
    setNotice("Enter the SSH password for this connection.", "error");
    els.password.focus();
    return;
  }
  els.reviewButton.disabled = true;
  setNotice("Reviewing locally. No connection opened.");
  try {
    const review = await invoke("ReviewTransfer", request);
    reviewedRequest = request;
    displayReview(review);
    setNotice("");
  } catch (error) {
    invalidateReview();
    setNotice(error?.message || String(error), "error");
  } finally {
    els.reviewButton.disabled = false;
  }
}

function formatBytes(bytes) {
  if (!Number.isFinite(bytes) || bytes < 1024) return `${Math.max(0, bytes || 0)} B`;
  const units = ["KB", "MB", "GB", "TB"];
  let value = bytes;
  let index = -1;
  do { value /= 1024; index += 1; } while (value >= 1024 && index < units.length - 1);
  return `${value.toFixed(value >= 10 ? 0 : 1)} ${units[index]}`;
}

const PHASE_LABELS = {
  connecting: "Connecting",
  preparing: "Preparing",
  uploading: "Uploading",
  verifying: "Verifying SHA-256",
  finalizing: "Finalizing",
  verified: "File verified",
  completed: "Transfer verified",
  interrupted: "Interrupted",
  failed: "Failed",
};

function displayProgress(update) {
  els.progressPanel.hidden = false;
  els.progressPhase.textContent = PHASE_LABELS[update.phase] || "Working";
  els.progressFile.textContent = update.file_name || update.message || "";
  if (update.total_bytes > 0) {
    const transferred = Math.min(update.transferred_bytes || 0, update.total_bytes);
    const percentage = Math.round((transferred / update.total_bytes) * 100);
    els.progressBar.classList.remove("is-indeterminate");
    els.progressFill.style.width = `${percentage}%`;
    const resumed = update.resumed_bytes > 0 ? ` · ${formatBytes(update.resumed_bytes)} already present` : "";
    els.progressDetail.textContent = `${formatBytes(transferred)} of ${formatBytes(update.total_bytes)} · ${percentage}%${resumed}`;
  } else {
    els.progressBar.classList.add("is-indeterminate");
    els.progressDetail.textContent = "";
  }
}

async function startTransfer() {
  if (!reviewedRequest) return;
  const password = reviewedRequest.authentication === "password" ? els.password.value : "";
  if (reviewedRequest.authentication === "password" && !password) {
    setNotice("Enter the SSH password, then review the transfer again.", "error");
    els.password.focus();
    return;
  }
  els.startButton.disabled = true;
  els.reviewTitle.textContent = retryingJobID ? "Returning job to queue" : "Adding to queue";
  setNotice("");
  try {
    const liveRequest = { ...reviewedRequest, password };
    const job = retryingJobID
      ? await invoke("RetryTransferJob", retryingJobID, password)
      : await invoke("EnqueueTransfer", liveRequest);
    liveRequest.password = "";
    if (reviewedRequest.authentication === "password") {
      els.password.value = "";
      els.password.type = "password";
      els.togglePassword.textContent = "Show";
      els.togglePassword.setAttribute("aria-pressed", "false");
    }
    retryingJobID = "";
    reviewedRequest = null;
    els.reviewPanel.hidden = true;
    els.startButton.textContent = "Add to queue";
    setNotice(`Added ${job.source_name || "transfer"} to the queue. You can prepare another transfer now.`, "success");
    await loadTransferJobs();
    showView("activity");
  } catch (error) {
    els.reviewTitle.textContent = "Transfer was not queued";
    setNotice(error?.message || String(error), "error");
  } finally {
    els.startButton.disabled = !reviewedRequest;
  }
}

/* ---------- native pickers ---------- */

async function choose(method, destination, markRecursive = false) {
  try {
    const selected = await invoke(method);
    if (selected) {
      destination.value = selected;
      if (markRecursive) els.recursive.checked = true;
      invalidateReview();
    }
  } catch (error) {
    setNotice(error?.message || String(error), "error");
  }
}

/* ---------- transfer manager ---------- */

const JOB_LABELS = {
  queued: "Queued",
  running: "Running",
  cancelling: "Cancelling",
  paused: "Paused",
  needs_password: "Needs password",
  verified: "Verified",
  interrupted: "Interrupted",
  failed: "Failed",
  canceled: "Canceled",
};

function setManagerNotice(message, kind = "") {
  els.managerNotice.hidden = !message;
  els.managerNotice.className = `notice${kind ? ` is-${kind}` : ""}`;
  els.managerNotice.textContent = message || "";
}

function jobPriority(status) {
  if (status === "running" || status === "cancelling") return 0;
  if (status === "queued") return 1;
  if (status === "needs_password" || status === "paused") return 2;
  return 3;
}

function orderedJobs(jobs) {
  return [...jobs].sort((left, right) => {
    const priority = jobPriority(left.status) - jobPriority(right.status);
    if (priority) return priority;
    return new Date(right.created_at || 0) - new Date(left.created_at || 0);
  });
}

function jobProgress(job) {
  if (!(job.total_bytes > 0)) return null;
  const transferred = Math.min(job.transferred_bytes || 0, job.total_bytes);
  return { transferred, percentage: Math.round((transferred / job.total_bytes) * 100) };
}

function jobRow(job, compact = false) {
  const row = document.createElement("article");
  row.className = `job-row${compact ? " is-compact" : ""}`;

  const heading = document.createElement("div");
  heading.className = "job-heading";
  const status = document.createElement("span");
  status.className = `status is-${job.status}`;
  status.textContent = JOB_LABELS[job.status] || job.status || "Unknown";
  const name = document.createElement("strong");
  name.textContent = job.source_name || "Transfer";
  const destination = document.createElement("span");
  destination.className = "job-destination";
  destination.textContent = job.destination || "";
  heading.append(status, name, destination);
  row.append(heading);

  if (!compact) {
    const detail = document.createElement("div");
    detail.className = "job-detail";
    const phase = document.createElement("span");
    phase.textContent = PHASE_LABELS[job.phase] || JOB_LABELS[job.status] || "Waiting";
    const message = document.createElement("span");
    message.textContent = job.message || "";
    detail.append(phase, message);
    row.append(detail);

    const progress = jobProgress(job);
    if (progress && (job.status === "running" || job.status === "cancelling")) {
      const progressWrap = document.createElement("div");
      progressWrap.className = "job-progress";
      const bar = document.createElement("div");
      bar.className = "bar";
      const fill = document.createElement("div");
      fill.className = "bar-fill";
      fill.style.width = `${progress.percentage}%`;
      bar.append(fill);
      const progressText = document.createElement("span");
      progressText.textContent = `${formatBytes(progress.transferred)} of ${formatBytes(job.total_bytes)} · ${progress.percentage}%`;
      progressWrap.append(bar, progressText);
      row.append(progressWrap);
    }

    const actions = document.createElement("div");
    actions.className = "job-actions";
    if (["queued", "running", "cancelling", "paused", "needs_password"].includes(job.status)) {
      const cancel = document.createElement("button");
      cancel.type = "button";
      cancel.className = "btn btn-quiet btn-sm";
      cancel.textContent = job.status === "cancelling" ? "Cancelling…" : "Cancel";
      cancel.disabled = job.status === "cancelling";
      cancel.addEventListener("click", () => cancelJob(job.id));
      actions.append(cancel);
    }
    if (["paused", "needs_password", "interrupted", "failed", "canceled"].includes(job.status)) {
      const retry = document.createElement("button");
      retry.type = "button";
      retry.className = "btn btn-secondary btn-sm";
      retry.textContent = job.authentication === "password" ? "Provide password" : "Retry";
      retry.addEventListener("click", () => retryJob(job));
      actions.append(retry);
    }
    if (["verified", "interrupted", "failed", "canceled"].includes(job.status)) {
      const remove = document.createElement("button");
      remove.type = "button";
      remove.className = "btn btn-quiet btn-sm";
      remove.textContent = "Remove";
      remove.addEventListener("click", () => removeJob(job.id));
      actions.append(remove);
    }
    if (actions.childElementCount) row.append(actions);
  }
  return row;
}

function renderTransferJobs() {
  const jobs = orderedJobs(transferQueue.jobs || []);
  els.jobsList.replaceChildren(...jobs.map((job) => jobRow(job)));
  els.jobsEmpty.hidden = jobs.length > 0;
  const dashboardJobs = jobs.filter((job) => job.status !== "verified").slice(0, 3);
  els.dashboardJobsList.replaceChildren(...dashboardJobs.map((job) => jobRow(job, true)));
  els.dashboardJobsEmpty.hidden = dashboardJobs.length > 0;
  els.queueRunning.textContent = String(transferQueue.running || 0);
  els.queueWaiting.textContent = String(transferQueue.queued || 0);
  els.queueCapacity.textContent = String(transferQueue.max_concurrent || 2);
  els.statusJobs.textContent = `${transferQueue.running || 0} active · ${transferQueue.queued || 0} queued`;
}

async function loadTransferJobs() {
  try {
    transferQueue = await invoke("ListTransferJobs") || { jobs: [], running: 0, queued: 0, max_concurrent: 2 };
    transferQueue.jobs ||= [];
    renderTransferJobs();
  } catch {
    transferQueue = { jobs: [], running: 0, queued: 0, max_concurrent: 2 };
    renderTransferJobs();
  }
}

async function cancelJob(id) {
  try {
    const canceled = await invoke("CancelTransferJob", id);
    setManagerNotice(canceled ? "Cancellation requested. Compatible partial state will be kept." : "That job is no longer active.");
    await loadTransferJobs();
  } catch (error) {
    setManagerNotice(error?.message || String(error), "error");
  }
}

async function retryJob(job) {
  if (job.authentication === "password") {
    try {
      const request = await invoke("GetTransferJobRequest", job.id);
      requestToForm(request);
      retryingJobID = job.id;
      reviewedRequest = null;
      els.sessionName.value = "";
      selectedSession = "";
      showView("transfer");
      invalidateReview();
      setNotice("Enter the one-time password, review the transfer, then retry it.");
      els.password.focus();
    } catch (error) {
      setManagerNotice(error?.message || String(error), "error");
    }
    return;
  }
  try {
    await invoke("RetryTransferJob", job.id, "");
    setManagerNotice("Transfer returned to the queue.", "success");
    await loadTransferJobs();
  } catch (error) {
    setManagerNotice(error?.message || String(error), "error");
  }
}

async function removeJob(id) {
  try {
    await invoke("RemoveTransferJob", id);
    await loadTransferJobs();
  } catch (error) {
    setManagerNotice(error?.message || String(error), "error");
  }
}

async function clearFinishedJobs() {
  try {
    const removed = await invoke("ClearFinishedTransferJobs");
    setManagerNotice(removed ? `Removed ${removed} finished job${removed === 1 ? "" : "s"}.` : "No finished jobs to remove.", "success");
    await loadTransferJobs();
  } catch (error) {
    setManagerNotice(error?.message || String(error), "error");
  }
}

function scheduleJobRefresh() {
  if (jobRefreshTimer) return;
  jobRefreshTimer = window.setTimeout(async () => {
    jobRefreshTimer = null;
    await loadTransferJobs();
    await loadHistory();
    loadDashboard();
  }, 120);
}

/* ---------- history ---------- */

function historyRow(entry) {
  const row = document.createElement("article");
  row.className = "row";
  const status = document.createElement("span");
  status.className = `status${entry.status === "verified" ? "" : ` is-${entry.status}`}`;
  status.textContent = entry.status || "unknown";
  const name = document.createElement("span");
  name.className = "name";
  name.textContent = entry.source_name || "Transfer";
  const dest = document.createElement("span");
  dest.className = "dest";
  dest.textContent = entry.destination || "";
  const size = document.createElement("span");
  size.className = "meta";
  size.textContent = entry.bytes ? formatBytes(entry.bytes) : "";
  const time = document.createElement("span");
  time.className = "meta";
  time.textContent = entry.completed_at ? new Date(entry.completed_at).toLocaleString() : "";
  row.append(status, name, dest, size, time);
  return row;
}

async function loadHistory() {
  try {
    lastHistory = await invoke("ListTransferHistory") || [];
  } catch {
    lastHistory = [];
  }
  els.historyList.replaceChildren(...lastHistory.map(historyRow));
  els.historyEmpty.hidden = lastHistory.length > 0;
}

async function clearHistory() {
  if (!window.confirm("Clear this computer's redacted transfer history?")) return;
  try {
    await invoke("ClearTransferHistory");
    await loadHistory();
    setManagerNotice("Redacted history cleared.", "success");
  } catch (error) {
    setManagerNotice(error?.message || String(error), "error");
  }
}

/* ---------- dashboard + statusbar ---------- */

async function loadDashboard() {
  try {
    const dashboard = await invoke("GetDashboard");
    const hostsDot = $("#hosts-dot");
    const agentDot = $("#agent-dot");
    $("#hosts-state").textContent = dashboard.strict_host_keys_ready ? "known_hosts found" : "Needs attention";
    $("#hosts-detail").textContent = dashboard.strict_host_keys_ready
      ? `${dashboard.known_hosts_path} · strict`
      : "Add the server fingerprint before connecting.";
    hostsDot.className = `dot ${dashboard.strict_host_keys_ready ? "is-on" : "is-warn"}`;
    $("#agent-state").textContent = dashboard.ssh_agent_available ? "SSH agent ready" : "Agent not detected";
    $("#agent-detail").textContent = dashboard.ssh_agent_available ? "identities from the agent" : "an explicit key path still works";
    agentDot.className = `dot ${dashboard.ssh_agent_available ? "is-on" : "is-warn"}`;
    const running = dashboard.running_transfers || 0;
    const queued = dashboard.queued_transfers || 0;
    els.statusEngine.textContent = running ? `engine active · ${running}` : queued ? "engine waiting" : "engine ready";
    els.statusAgent.textContent = dashboard.ssh_agent_available ? "agent connected" : "agent not detected";
    els.statusVersion.textContent = `${dashboard.version} · ${dashboard.platform}`;
    if (dashboard.known_hosts_path) els.knownHosts.placeholder = dashboard.known_hosts_path;
  } catch {
    $("#hosts-state").textContent = "Bridge unavailable";
    $("#agent-state").textContent = "Bridge unavailable";
    els.statusVersion.textContent = "desktop bridge unavailable";
  }
}

function subscribeToProgress() {
  if (typeof window.runtime?.EventsOn === "function") {
    window.runtime.EventsOn("transfer:progress", scheduleJobRefresh);
  }
}

/* ---------- wiring ---------- */

els.tabs.forEach((button) => button.addEventListener("click", () => showView(button.dataset.view)));
document.querySelectorAll("[data-open-transfer]").forEach((button) => button.addEventListener("click", () => {
  retryingJobID = "";
  els.startButton.textContent = "Add to queue";
  showView("transfer");
}));
document.querySelectorAll("[data-open-activity]").forEach((button) => button.addEventListener("click", () => showView("activity")));
document.querySelectorAll("[data-open-help]").forEach((button) => button.addEventListener("click", () => showView("help")));
els.form.addEventListener("submit", (event) => { event.preventDefault(); reviewTransfer(); });
els.startButton.addEventListener("click", startTransfer);
els.advancedToggle.addEventListener("click", () => setAdvancedOpen(els.advanced.hidden));
els.saveSession.addEventListener("click", saveSession);
els.clearHistory.addEventListener("click", clearHistory);
els.clearFinishedJobs.addEventListener("click", clearFinishedJobs);
els.themeToggle.addEventListener("click", () => setTheme(activeTheme() === "dark" ? "light" : "dark"));
els.authRadios.forEach((radio) => radio.addEventListener("change", () => {
  retryingJobID = "";
  setAuthentication(radio.value);
  invalidateReview();
}));
els.togglePassword.addEventListener("click", () => {
  const reveal = els.password.type === "password";
  els.password.type = reveal ? "text" : "password";
  els.togglePassword.textContent = reveal ? "Hide" : "Show";
  els.togglePassword.setAttribute("aria-pressed", String(reveal));
});
$("#choose-file").addEventListener("click", () => choose("SelectSourceFile", els.source));
$("#choose-folder").addEventListener("click", () => choose("SelectSourceDirectory", els.source, true));
$("#choose-identity").addEventListener("click", () => choose("SelectIdentityFile", els.identity));
[els.source, els.destination, els.port, els.permissions, els.password, els.identity, els.knownHosts, els.group, els.readableBy,
 els.recursive, els.resume, els.overwrite, els.preserveTime].forEach((field) => {
  field.addEventListener("input", () => { if (field !== els.password) retryingJobID = ""; invalidateReview(); });
  field.addEventListener("change", () => { if (field !== els.password) retryingJobID = ""; invalidateReview(); });
});

initializeTheme();
subscribeToProgress();
initializeSessions();
loadDashboard();
loadTransferJobs();
loadHistory();
window.setInterval(() => {
  if ((transferQueue.running || 0) > 0 || (transferQueue.queued || 0) > 0) loadTransferJobs();
}, 1500);
