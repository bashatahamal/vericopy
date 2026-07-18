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
  recentList: $("#recent-list"),
  recentEmpty: $("#recent-empty"),
  historyList: $("#history-list"),
  historyEmpty: $("#history-empty"),
  clearHistory: $("#clear-history"),
  advancedToggle: $("#advanced-toggle"),
  advanced: $("#advanced"),
  reviewButton: $("#review-button"),
  startButton: $("#start-transfer"),
  cancelButton: $("#cancel-transfer"),
  notice: $("#notice"),
  reviewPanel: $("#review-panel"),
  reviewTitle: $("#review-title"),
  reviewList: $("#review-list"),
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
};

let sessions = [];
let selectedSession = "";
let reviewedRequest = null;
let lastHistory = [];

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
  if (view === "dashboard") { loadDashboard(); renderSessions(); loadHistory(); }
  if (view === "transfer") els.source.focus();
  if (view === "activity") loadHistory();
}

/* ---------- notices ---------- */

function setNotice(message, kind = "") {
  els.notice.hidden = !message;
  els.notice.className = `notice${kind ? ` is-${kind}` : ""}`;
  els.notice.textContent = message || "";
}

/* ---------- form <-> data ---------- */

function requestFromForm() {
  return {
    source: els.source.value.trim(),
    destination: els.destination.value.trim(),
    port: Number(els.port.value || 22),
    permissions: els.permissions.value,
    identity: els.identity.value.trim(),
    known_hosts: els.knownHosts.value.trim(),
    group: els.group.value.trim(),
    readable_by: els.readableBy.value.trim(),
    recursive: els.recursive.checked,
    resume: els.resume.checked,
    overwrite: els.overwrite.checked,
    preserve_time: els.preserveTime.checked,
  };
}

function setAdvancedOpen(open) {
  els.advanced.hidden = !open;
  els.advancedToggle.setAttribute("aria-expanded", String(open));
  els.advancedToggle.innerHTML = open ? "Advanced &#9652;" : "Advanced &#9662;";
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
  els.source.value = session.source || "";
  els.destination.value = session.destination || "";
  els.port.value = session.port || "";
  els.permissions.value = session.permissions || "private";
  els.identity.value = session.identity || "";
  els.knownHosts.value = session.known_hosts || "";
  els.group.value = session.group || "";
  els.readableBy.value = session.readable_by || "";
  els.recursive.checked = !!session.recursive;
  els.resume.checked = !!session.resume;
  els.overwrite.checked = !!session.overwrite;
  els.preserveTime.checked = !!session.preserve_time;
  els.sessionName.value = session.name;
  selectedSession = session.name;
  setAdvancedOpen(!!(session.identity || session.known_hosts || session.group || session.readable_by));
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
  els.reviewTitle.textContent = "Ready · no connection opened yet";
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
  els.startButton.disabled = true;
  els.cancelButton.hidden = false;
  els.reviewTitle.textContent = "Transferring";
  displayProgress({ phase: "connecting" });
  setNotice("");
  try {
    const outcome = await invoke("StartTransfer", reviewedRequest);
    els.reviewTitle.textContent = "Transfer verified";
    els.progressBar.classList.remove("is-indeterminate");
    els.progressFill.style.width = "100%";
    setNotice(outcome.summary || "Verified. Remote bytes match SHA-256.", "success");
  } catch (error) {
    els.reviewTitle.textContent = "Transfer not verified";
    setNotice(error?.message || String(error), "error");
  } finally {
    els.cancelButton.hidden = true;
    els.startButton.disabled = !reviewedRequest;
    loadHistory();
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
  els.recentList.replaceChildren(...lastHistory.slice(0, 3).map(historyRow));
  els.recentEmpty.hidden = lastHistory.length > 0;
}

async function clearHistory() {
  if (!window.confirm("Clear this computer's redacted transfer history?")) return;
  try {
    await invoke("ClearTransferHistory");
    await loadHistory();
  } catch (error) {
    setNotice(error?.message || String(error), "error");
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
    els.statusEngine.textContent = dashboard.transfer_active ? "engine active" : "engine ready";
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
    window.runtime.EventsOn("transfer:progress", displayProgress);
  }
}

/* ---------- wiring ---------- */

els.tabs.forEach((button) => button.addEventListener("click", () => showView(button.dataset.view)));
document.querySelectorAll("[data-open-transfer]").forEach((button) => button.addEventListener("click", () => showView("transfer")));
document.querySelectorAll("[data-open-activity]").forEach((button) => button.addEventListener("click", () => showView("activity")));
els.form.addEventListener("submit", (event) => { event.preventDefault(); reviewTransfer(); });
els.startButton.addEventListener("click", startTransfer);
els.cancelButton.addEventListener("click", async () => {
  try {
    const cancelled = await invoke("CancelTransfer");
    setNotice(cancelled ? "Cancellation requested. Partial state kept for resume." : "No active transfer to cancel.");
  } catch (error) {
    setNotice(error?.message || String(error), "error");
  }
});
els.advancedToggle.addEventListener("click", () => setAdvancedOpen(els.advanced.hidden));
els.saveSession.addEventListener("click", saveSession);
els.clearHistory.addEventListener("click", clearHistory);
els.themeToggle.addEventListener("click", () => setTheme(activeTheme() === "dark" ? "light" : "dark"));
$("#choose-file").addEventListener("click", () => choose("SelectSourceFile", els.source));
$("#choose-folder").addEventListener("click", () => choose("SelectSourceDirectory", els.source, true));
$("#choose-identity").addEventListener("click", () => choose("SelectIdentityFile", els.identity));
[els.source, els.destination, els.port, els.permissions, els.identity, els.knownHosts, els.group, els.readableBy,
 els.recursive, els.resume, els.overwrite, els.preserveTime].forEach((field) => {
  field.addEventListener("input", invalidateReview);
  field.addEventListener("change", invalidateReview);
});

initializeTheme();
subscribeToProgress();
initializeSessions();
loadDashboard();
loadHistory();
