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
  reviewEmpty: document.querySelector("#review-empty"),
  reviewResult: document.querySelector("#review-result"),
  reviewList: document.querySelector("#review-list"),
  reviewTitle: document.querySelector("#review-title"),
  reviewButton: document.querySelector("#review-button"),
  startButton: document.querySelector("#start-transfer"),
  cancelButton: document.querySelector("#cancel-transfer"),
  notice: document.querySelector("#notice"),
};

let reviewedRequest = null;

function showView(view) {
  elements.nav.forEach((button) => button.classList.toggle("is-active", button.dataset.view === view));
  elements.panels.forEach((panel) => panel.classList.toggle("is-visible", panel.dataset.viewPanel === view));
  if (view === "transfer") elements.source.focus();
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

elements.nav.forEach((button) => button.addEventListener("click", () => showView(button.dataset.view)));
document.querySelectorAll("[data-open-transfer]").forEach((button) => button.addEventListener("click", () => showView("transfer")));
elements.form.addEventListener("submit", (event) => { event.preventDefault(); reviewTransfer(); });
elements.startButton.addEventListener("click", startTransfer);
elements.cancelButton.addEventListener("click", async () => {
  const cancelled = await invoke("CancelTransfer");
  setNotice(cancelled ? "Cancellation requested. Compatible partial state is retained for resume." : "No active transfer to cancel.");
});
document.querySelector("#choose-file").addEventListener("click", () => choose("SelectSourceFile", elements.source));
document.querySelector("#choose-folder").addEventListener("click", () => choose("SelectSourceDirectory", elements.source, true));
document.querySelector("#choose-identity").addEventListener("click", () => choose("SelectIdentityFile", elements.identity));
[elements.source, elements.destination, elements.port, elements.permissions, elements.identity, elements.knownHosts, elements.group, elements.readableBy, elements.recursive, elements.resume, elements.overwrite, elements.preserveTime].forEach((field) => field.addEventListener("input", invalidateReview));

loadDashboard();
