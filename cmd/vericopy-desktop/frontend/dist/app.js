const bridge = () => window.go?.main?.Bridge;

async function invoke(method, ...args) {
  const target = bridge();
  if (!target || typeof target[method] !== "function") {
    throw new Error("The desktop bridge is unavailable. Launch Vericopy as the desktop application, not in a browser.");
  }
  return target[method](...args);
}

const elements = {
  nav: [...document.querySelectorAll("[data-view]")],
  panels: [...document.querySelectorAll("[data-view-panel]")],
  form: document.querySelector("#transfer-form"),
  source: document.querySelector("#source"),
  destination: document.querySelector("#destination"),
  port: document.querySelector("#port"),
  permissions: document.querySelector("#permissions"),
  identity: document.querySelector("#identity"),
  knownHosts: document.querySelector("#known-hosts"),
  group: document.querySelector("#group"),
  readableBy: document.querySelector("#readable-by"),
  recursive: document.querySelector("#recursive"),
  resume: document.querySelector("#resume"),
  overwrite: document.querySelector("#overwrite"),
  preserveTime: document.querySelector("#preserve-time"),
  profileSelect: document.querySelector("#profile-select"),
  profileName: document.querySelector("#profile-name"),
  saveProfile: document.querySelector("#save-profile"),
  profilesList: document.querySelector("#profiles-list"),
  profilesEmpty: document.querySelector("#profiles-empty"),
  historyList: document.querySelector("#history-list"),
  historyEmpty: document.querySelector("#history-empty"),
  clearHistory: document.querySelector("#clear-history"),
  reviewEmpty: document.querySelector("#review-empty"),
  reviewResult: document.querySelector("#review-result"),
  reviewList: document.querySelector("#review-list"),
  reviewTitle: document.querySelector("#review-title"),
  reviewButton: document.querySelector("#review-button"),
  startButton: document.querySelector("#start-transfer"),
  cancelButton: document.querySelector("#cancel-transfer"),
  progressPanel: document.querySelector("#progress-panel"),
  progressPhase: document.querySelector("#progress-phase"),
  progressFile: document.querySelector("#progress-file"),
  progressMeter: document.querySelector("#progress-meter"),
  progressDetail: document.querySelector("#progress-detail"),
  notice: document.querySelector("#notice"),
  themeToggle: document.querySelector("#theme-toggle"),
};

let reviewedRequest = null;
let profiles = [];
let selectedProfileID = "";

function showView(view) {
  elements.nav.forEach((button) => {
    const active = button.dataset.view === view;
    button.classList.toggle("is-active", active);
    if (active) button.setAttribute("aria-current", "page");
    else button.removeAttribute("aria-current");
  });
  elements.panels.forEach((panel) => panel.classList.toggle("is-visible", panel.dataset.viewPanel === view));
  if (view === "transfer") elements.source.focus();
  if (view === "profiles") loadProfiles();
  if (view === "activity") loadHistory();
}

function systemTheme() {
  return window.matchMedia?.("(prefers-color-scheme: dark)")?.matches ? "dark" : "light";
}

function activeTheme() {
  return document.documentElement.dataset.theme || systemTheme();
}

function updateThemeToggle() {
  const dark = activeTheme() === "dark";
  elements.themeToggle.setAttribute("aria-pressed", String(dark));
  elements.themeToggle.setAttribute("aria-label", dark ? "Switch to light mode" : "Switch to dark mode");
}

function setTheme(theme, persist = true) {
  document.documentElement.dataset.theme = theme;
  if (persist) {
    try {
      window.localStorage.setItem("vericopy.theme", theme);
    } catch {
      // Theme choice is purely presentational. A locked-down WebView may deny storage.
    }
  }
  updateThemeToggle();
}

function initializeTheme() {
  try {
    const saved = window.localStorage.getItem("vericopy.theme");
    if (saved === "light" || saved === "dark") {
      setTheme(saved, false);
      return;
    }
  } catch {
    // Fall back to the operating system preference when storage is unavailable.
  }
  updateThemeToggle();
}

function requestFromForm() {
  return {
    source: elements.source.value.trim(),
    destination: elements.destination.value.trim(),
    port: Number(elements.port.value || 22),
    permissions: elements.permissions.value,
    identity: elements.identity.value.trim(),
    known_hosts: elements.knownHosts.value.trim(),
    group: elements.group.value.trim(),
    readable_by: elements.readableBy.value.trim(),
    recursive: elements.recursive.checked,
    resume: elements.resume.checked,
    overwrite: elements.overwrite.checked,
    preserve_time: elements.preserveTime.checked,
  };
}

function profileFromForm() {
  return {
    id: selectedProfileID,
    name: elements.profileName.value.trim(),
    destination: elements.destination.value.trim(),
    port: Number(elements.port.value || 22),
    known_hosts: elements.knownHosts.value.trim(),
  };
}

function setNotice(message, kind = "") {
  elements.notice.hidden = !message;
  elements.notice.className = `notice${kind ? ` is-${kind}` : ""}`;
  elements.notice.textContent = message || "";
}

function addReviewRow(label, value) {
  const row = document.createElement("div");
  const term = document.createElement("dt");
  const definition = document.createElement("dd");
  term.textContent = label;
  definition.textContent = value;
  row.append(term, definition);
  elements.reviewList.append(row);
}

function displayReview(review) {
  elements.reviewList.replaceChildren();
  addReviewRow("Source", review.source.path);
  addReviewRow("Source type", review.source.is_directory ? "Directory tree" : "Regular file");
  addReviewRow("Destination", `${review.destination.user}@${review.destination.host}:${review.destination.path}`);
  addReviewRow("SSH port", String(review.destination.port));
  addReviewRow("Policy", review.permissions);
  addReviewRow("Host key file", review.known_hosts);
  addReviewRow("Resume", review.resume ? "Compatible partial state allowed" : "Disabled");
  addReviewRow("Replacement", review.overwrite ? "Explicitly allowed" : "Existing destination protected");
  if (review.readable_by) addReviewRow("Access check", `Read as ${review.readable_by}`);
  elements.reviewEmpty.hidden = true;
  elements.reviewResult.hidden = false;
  elements.reviewTitle.textContent = "Ready for confirmation";
  elements.startButton.disabled = false;
}

function invalidateReview() {
  reviewedRequest = null;
  elements.startButton.disabled = true;
  elements.reviewEmpty.hidden = false;
  elements.reviewResult.hidden = true;
  setNotice("");
}

function formatBytes(bytes) {
  if (!Number.isFinite(bytes) || bytes < 1024) return `${Math.max(0, bytes || 0)} B`;
  const units = ["KB", "MB", "GB", "TB"];
  let value = bytes;
  let index = -1;
  do {
    value /= 1024;
    index += 1;
  } while (value >= 1024 && index < units.length - 1);
  return `${value.toFixed(value >= 10 ? 0 : 1)} ${units[index]}`;
}

function displayProgress(update) {
  const labels = {
    connecting: "Connecting",
    preparing: "Preparing",
    uploading: "Sending current file",
    verifying: "Verifying SHA-256",
    finalizing: "Applying policy and finalizing",
    verified: "Current file verified",
    completed: "Transfer verified",
    interrupted: "Transfer interrupted",
    failed: "Transfer failed",
  };
  elements.progressPanel.hidden = false;
  elements.progressPhase.textContent = labels[update.phase] || "Transfer update";
  elements.progressFile.textContent = update.file_name || update.message || "Working…";
  if (update.total_bytes > 0) {
    const transferred = Math.min(update.transferred_bytes || 0, update.total_bytes);
    elements.progressMeter.max = update.total_bytes;
    elements.progressMeter.value = transferred;
    const percentage = Math.round((transferred / update.total_bytes) * 100);
    const resumed = update.resumed_bytes > 0 ? ` · ${formatBytes(update.resumed_bytes)} already present` : "";
    elements.progressDetail.textContent = `${formatBytes(transferred)} of ${formatBytes(update.total_bytes)} · ${percentage}%${resumed}`;
  } else {
    elements.progressMeter.removeAttribute("value");
    elements.progressDetail.textContent = update.message || "Waiting for the next transfer stage.";
  }
}

async function reviewTransfer() {
  const request = requestFromForm();
  elements.reviewButton.disabled = true;
  setNotice("Reviewing local source and destination. No connection has been opened.");
  try {
    const review = await invoke("ReviewTransfer", request);
    reviewedRequest = request;
    displayReview(review);
    setNotice("The request is locally valid. Confirm to begin strict SSH transfer.", "success");
  } catch (error) {
    invalidateReview();
    setNotice(error?.message || String(error), "error");
  } finally {
    elements.reviewButton.disabled = false;
  }
}

async function startTransfer() {
  if (!reviewedRequest) return;
  elements.startButton.disabled = true;
  elements.cancelButton.hidden = false;
  displayProgress({ phase: "connecting", message: "Opening a strict SSH connection" });
  setNotice("Transferring through native SFTP. Finalization waits for SHA-256 verification.");
  try {
    const outcome = await invoke("StartTransfer", reviewedRequest);
    elements.reviewTitle.textContent = "Transfer verified";
    setNotice(outcome.summary || "Transfer verified.", "success");
  } catch (error) {
    setNotice(error?.message || String(error), "error");
  } finally {
    elements.cancelButton.hidden = true;
    elements.startButton.disabled = !reviewedRequest;
    loadHistory();
  }
}

async function choose(method, destination, recursive = false) {
  try {
    const selected = await invoke(method);
    if (selected) {
      destination.value = selected;
      if (recursive) elements.recursive.checked = true;
      invalidateReview();
    }
  } catch (error) {
    setNotice(error?.message || String(error), "error");
  }
}

function profileCard(profile) {
  const card = document.createElement("article");
  card.className = "collection-card";
  const content = document.createElement("div");
  const title = document.createElement("h2");
  const destination = document.createElement("p");
  const metadata = document.createElement("p");
  title.textContent = profile.name;
  destination.textContent = `${profile.destination} · port ${profile.port}`;
  metadata.className = "meta";
  metadata.textContent = `known_hosts: ${profile.known_hosts}`;
  content.append(title, destination, metadata);
  const actions = document.createElement("div");
  actions.className = "collection-actions";
  const apply = document.createElement("button");
  apply.type = "button";
  apply.className = "button button-secondary";
  apply.textContent = "Use connection";
  apply.addEventListener("click", () => {
    applyProfile(profile);
    showView("transfer");
  });
  const remove = document.createElement("button");
  remove.type = "button";
  remove.className = "button button-quiet";
  remove.textContent = "Remove";
  remove.addEventListener("click", () => deleteProfile(profile));
  actions.append(apply, remove);
  card.append(content, actions);
  return card;
}

function renderProfiles() {
  elements.profileSelect.replaceChildren();
  const emptyOption = document.createElement("option");
  emptyOption.value = "";
  emptyOption.textContent = "No saved connection";
  elements.profileSelect.append(emptyOption);
  profiles.forEach((profile) => {
    const option = document.createElement("option");
    option.value = profile.id;
    option.textContent = profile.name;
    elements.profileSelect.append(option);
  });
  elements.profileSelect.value = selectedProfileID;
  elements.profilesList.replaceChildren(...profiles.map(profileCard));
  elements.profilesEmpty.hidden = profiles.length > 0;
}

async function loadProfiles() {
  try {
    profiles = await invoke("ListProfiles");
    renderProfiles();
  } catch (error) {
    elements.profilesEmpty.hidden = false;
    elements.profilesEmpty.textContent = error?.message || String(error);
  }
}

function applyProfile(profile) {
  selectedProfileID = profile.id;
  elements.destination.value = profile.destination;
  elements.port.value = profile.port;
  elements.knownHosts.value = profile.known_hosts;
  elements.profileName.value = profile.name;
  renderProfiles();
  invalidateReview();
}

async function saveProfile() {
  if (!elements.profileName.value.trim()) {
    setNotice("Enter a profile name before saving the connection.", "error");
    elements.profileName.focus();
    return;
  }
  elements.saveProfile.disabled = true;
  try {
    const saved = await invoke("SaveProfile", profileFromForm());
    selectedProfileID = saved.id;
    elements.profileName.value = saved.name;
    await loadProfiles();
    setNotice("Saved the non-secret connection reference on this computer.", "success");
  } catch (error) {
    setNotice(error?.message || String(error), "error");
  } finally {
    elements.saveProfile.disabled = false;
  }
}

async function deleteProfile(profile) {
  try {
    const removed = await invoke("DeleteProfile", profile.id);
    if (removed && selectedProfileID === profile.id) {
      selectedProfileID = "";
      elements.profileName.value = "";
    }
    await loadProfiles();
  } catch (error) {
    setNotice(error?.message || String(error), "error");
  }
}

function historyCard(entry) {
  const card = document.createElement("article");
  card.className = "collection-card";
  const content = document.createElement("div");
  const title = document.createElement("h2");
  const destination = document.createElement("p");
  const metadata = document.createElement("p");
  title.textContent = entry.source_name || "Transfer";
  destination.textContent = entry.destination;
  metadata.className = "meta";
  const completed = entry.completed_at ? new Date(entry.completed_at).toLocaleString() : "Unknown time";
  metadata.textContent = `${completed} · ${formatBytes(entry.bytes || 0)}${entry.resumed_bytes ? ` · resumed ${formatBytes(entry.resumed_bytes)}` : ""}${entry.diagnostic_code ? ` · ${entry.diagnostic_code}` : ""}`;
  content.append(title, destination, metadata);
  const status = document.createElement("span");
  status.className = `history-status${entry.status === "verified" ? "" : ` is-${entry.status}`}`;
  status.textContent = entry.status || "unknown";
  card.append(content, status);
  return card;
}

async function loadHistory() {
  try {
    const history = await invoke("ListTransferHistory");
    elements.historyList.replaceChildren(...history.map(historyCard));
    elements.historyEmpty.hidden = history.length > 0;
  } catch (error) {
    elements.historyEmpty.hidden = false;
    elements.historyEmpty.textContent = error?.message || String(error);
  }
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

async function loadDashboard() {
  try {
    const dashboard = await invoke("GetDashboard");
    document.querySelector("#build-note").textContent = `Version ${dashboard.version} · ${dashboard.platform}`;
    document.querySelector("#known-hosts-state").textContent = dashboard.strict_host_keys_ready ? "Strict verification ready" : "Needs known_hosts attention";
    document.querySelector("#known-hosts-detail").textContent = dashboard.strict_host_keys_ready ? dashboard.known_hosts_path : "Add and verify the server fingerprint before connecting.";
    document.querySelector("#agent-state").textContent = dashboard.ssh_agent_available ? "SSH agent available" : "Agent not detected";
    if (dashboard.known_hosts_path) elements.knownHosts.placeholder = dashboard.known_hosts_path;
  } catch (error) {
    document.querySelector("#build-note").textContent = "Desktop bridge unavailable";
  }
}

function subscribeToProgress() {
  if (typeof window.runtime?.EventsOn === "function") {
    window.runtime.EventsOn("transfer:progress", displayProgress);
  }
}

elements.nav.forEach((button) => button.addEventListener("click", () => showView(button.dataset.view)));
document.querySelectorAll("[data-open-transfer]").forEach((button) => button.addEventListener("click", () => showView("transfer")));
elements.form.addEventListener("submit", (event) => { event.preventDefault(); reviewTransfer(); });
elements.startButton.addEventListener("click", startTransfer);
elements.cancelButton.addEventListener("click", async () => {
  const cancelled = await invoke("CancelTransfer");
  setNotice(cancelled ? "Cancellation requested. Compatible partial state is retained for resume." : "No active transfer to cancel.");
});
elements.profileSelect.addEventListener("change", () => {
  const profile = profiles.find((item) => item.id === elements.profileSelect.value);
  if (profile) applyProfile(profile);
  if (!profile) {
    selectedProfileID = "";
    elements.profileName.value = "";
  }
});
elements.saveProfile.addEventListener("click", saveProfile);
elements.clearHistory.addEventListener("click", clearHistory);
elements.themeToggle.addEventListener("click", () => setTheme(activeTheme() === "dark" ? "light" : "dark"));
document.querySelector("#choose-file").addEventListener("click", () => choose("SelectSourceFile", elements.source));
document.querySelector("#choose-folder").addEventListener("click", () => choose("SelectSourceDirectory", elements.source, true));
document.querySelector("#choose-identity").addEventListener("click", () => choose("SelectIdentityFile", elements.identity));
[elements.source, elements.destination, elements.port, elements.permissions, elements.identity, elements.knownHosts, elements.group, elements.readableBy, elements.recursive, elements.resume, elements.overwrite, elements.preserveTime].forEach((field) => {
  field.addEventListener("input", invalidateReview);
  field.addEventListener("change", invalidateReview);
});

initializeTheme();
subscribeToProgress();
loadDashboard();
loadProfiles();
loadHistory();
