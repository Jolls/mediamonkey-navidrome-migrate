"use strict";

const state = {
  config: null, // last successfully-posted /api/config body
  userId: "",
};

const $ = (id) => document.getElementById(id);

function showStep(id) {
  for (const el of document.querySelectorAll(".step")) {
    el.hidden = el.id !== id;
  }
}

function showError(el, err) {
  el.textContent = err instanceof Error ? err.message : String(err);
  el.hidden = false;
}

function hideError(el) {
  el.hidden = true;
  el.textContent = "";
}

async function api(method, path, body) {
  const opts = { method, headers: {} };
  if (body !== undefined) {
    opts.headers["Content-Type"] = "application/json";
    opts.body = JSON.stringify(body);
  }
  const resp = await fetch(path, opts);
  const text = await resp.text();
  const data = text ? JSON.parse(text) : null;
  if (!resp.ok) {
    throw new Error((data && data.error) || `${method} ${path}: HTTP ${resp.status}`);
  }
  return data;
}

// ratingStepLabels[i] labels MM rating step i (0 = unrated, 1-10 = half-star
// steps 0.5-5.0), matching model.Track.RatingStep.
const ratingStepLabels = ["Unrated", "0.5", "1", "1.5", "2", "2.5", "3", "3.5", "4", "4.5", "5"];

function roundDownMap() {
  return ratingStepLabels.map((_, step) => (step === 0 ? 0 : Math.floor(step / 2)));
}

function roundUpMap() {
  return ratingStepLabels.map((_, step) => (step === 0 ? 0 : Math.ceil(step / 2)));
}

const ratingMapCustom = $("rating-map-custom");

function buildRatingMapCustomRows(initial) {
  ratingMapCustom.innerHTML = "";
  ratingStepLabels.forEach((label, step) => {
    const row = document.createElement("label");
    const select = document.createElement("select");
    select.dataset.ratingStep = String(step);
    for (let star = 0; star <= 5; star++) {
      const opt = document.createElement("option");
      opt.value = String(star);
      opt.textContent = String(star);
      if (star === initial[step]) opt.selected = true;
      select.appendChild(opt);
    }
    row.append(`MM ${label} ★  →  `, select);
    ratingMapCustom.appendChild(row);
  });
}

for (const radio of document.querySelectorAll('input[name="ratingMode"]')) {
  radio.addEventListener("change", () => {
    const custom = $("config-form").ratingMode.value === "custom";
    ratingMapCustom.hidden = !custom;
    if (custom && !ratingMapCustom.children.length) {
      buildRatingMapCustomRows(roundDownMap());
    }
  });
}

function ratingMapFromForm(form) {
  switch (form.ratingMode.value) {
    case "up":
      return roundUpMap();
    case "custom":
      return ratingStepLabels.map((_, step) =>
        Number(ratingMapCustom.querySelector(`[data-rating-step="${step}"]`).value)
      );
    default:
      return roundDownMap();
  }
}

function configBody(form, extra) {
  const fd = new FormData(form);
  return {
    mmDbPath: fd.get("mmDbPath") || "",
    navDbPath: fd.get("navDbPath") || "",
    serverUrl: fd.get("serverUrl") || "",
    username: fd.get("username") || "",
    password: fd.get("password") || "",
    musicRoot: fd.get("musicRoot") || "",
    userId: (extra && extra.userId) || "",
    fields: fd.getAll("fields"),
    starThreshold: Number(fd.get("starThreshold")) || 0,
    ratingMap: ratingMapFromForm(form),
  };
}

$("quit-btn").addEventListener("click", async () => {
  try {
    await api("POST", "/api/quit");
  } catch {
    // server is exiting; the fetch may fail to complete
  }
  document.body.innerHTML = "<p>You can close this window.</p>";
});

for (const btn of document.querySelectorAll(".browse-btn")) {
  btn.addEventListener("click", async () => {
    hideError($("config-error"));
    const input = document.querySelector(`[name="${btn.dataset.target}"]`);
    const label = btn.dataset.label || "Select file";
    btn.disabled = true;
    try {
      const res = await api("GET", `/api/browse-file?label=${encodeURIComponent(label)}`);
      if (res.path) {
        input.value = res.path;
      }
    } catch (err) {
      showError($("config-error"), err);
    } finally {
      btn.disabled = false;
    }
  });
}

$("config-form").addEventListener("submit", async (e) => {
  e.preventDefault();
  hideError($("config-error"));
  const body = configBody(e.target);
  try {
    await api("POST", "/api/config", body);
    state.config = body;
    const users = await api("GET", "/api/users");
    const select = $("user-select");
    select.innerHTML = "";
    for (const u of users || []) {
      const opt = document.createElement("option");
      opt.value = u.ID;
      opt.textContent = u.Username;
      select.appendChild(opt);
    }
    showStep("step-user");
  } catch (err) {
    showError($("config-error"), err);
  }
});

$("user-continue").addEventListener("click", async () => {
  hideError($("global-error"));
  state.userId = $("user-select").value;
  try {
    await api("POST", "/api/config", { ...state.config, userId: state.userId });
    showStep("step-scope");
  } catch (err) {
    showError($("global-error"), err);
  }
});

const statusNames = ["unmatched", "matched", "ambiguous"];

$("scan-btn").addEventListener("click", async () => {
  hideError($("global-error"));
  const dir = $("scope-input").value.trim();
  try {
    const matches = await api("GET", `/api/scan?dir=${encodeURIComponent(dir)}`);
    const counts = { matched: 0, ambiguous: 0, unmatched: 0 };
    for (const m of matches || []) {
      counts[statusNames[m.Status]]++;
    }
    $("scan-summary").innerHTML = `
      <div class="buckets">
        <span>Matched: ${counts.matched}</span>
        <span>Ambiguous: ${counts.ambiguous}</span>
        <span>Unmatched: ${counts.unmatched}</span>
      </div>`;
    showStep("step-review");
    await runDryRun(dir);
  } catch (err) {
    showError($("global-error"), err);
  }
});

async function runDryRun(dir) {
  const rep = await api("GET", `/api/dry-run?dir=${encodeURIComponent(dir)}`);
  $("dryrun-summary").innerHTML = `
    <div class="buckets">
      <span>Matched: ${rep.Matched}</span>
      <span>Ambiguous: ${rep.Ambiguous}</span>
      <span>Unmatched: ${rep.Unmatched}</span>
      <span>Changes: ${(rep.Changes || []).length}</span>
    </div>`;

  const table = $("dryrun-table");
  const tbody = table.querySelector("tbody");
  tbody.innerHTML = "";
  for (const c of rep.Changes || []) {
    const tr = document.createElement("tr");
    tr.innerHTML = `
      <td>${escapeHTML(c.RelPath)}</td>
      <td>${c.Rating ?? ""}</td>
      <td>${c.PlayCount ?? ""}</td>
      <td>${c.LastPlayed ? formatNaiveDate(c.LastPlayed) : ""}</td>
      <td>${c.Starred === null || c.Starred === undefined ? "" : c.Starred}</td>`;
    tbody.appendChild(tr);
  }
  table.hidden = (rep.Changes || []).length === 0;

  $("commit-btn").dataset.dir = dir;
}

$("review-back-btn").addEventListener("click", () => {
  hideError($("global-error"));
  showStep("step-scope");
});

// formatNaiveDate renders an ISO timestamp's literal digits (the wall-clock
// MediaMonkey recorded, which has no reliable real-world offset — see
// mm.FromMMDate) without letting the browser's timezone shift them, the way
// `new Date(iso).toLocaleString()` would.
function formatNaiveDate(iso) {
  const m = iso.match(/^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})/);
  if (!m) return iso;
  const [, y, mo, d, h, mi, s] = m.map(Number);
  const asUTC = new Date(Date.UTC(y, mo - 1, d, h, mi, s));
  return asUTC.toLocaleString(undefined, { timeZone: "UTC" });
}

function escapeHTML(s) {
  const div = document.createElement("div");
  div.textContent = s;
  return div.innerHTML;
}

$("commit-btn").addEventListener("click", async () => {
  hideError($("global-error"));
  const dir = $("commit-btn").dataset.dir || "";
  $("commit-result").textContent = "Committing…";
  try {
    const res = await api("POST", "/api/commit", { dir });
    const errCount = (res.result.Errors || []).length;
    $("commit-result").textContent =
      `Applied ${res.result.Applied} change(s). ${errCount} error(s). Backup: ${res.backupPath}`;
  } catch (err) {
    $("commit-result").textContent = "";
    showError($("global-error"), err);
  }
});
