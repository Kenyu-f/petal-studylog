// StudyLog dashboard (Grass) behavior.
// See explanation.md sections 14 ("Frontend Rendering Flow") and
// 15 ("Cell Click Interaction") for the design of this file.
(function () {
  "use strict";

  const goalSelect = document.getElementById("goal-select");
  const yearSelect = document.getElementById("year-select");
  const grassContainer = document.getElementById("grass-container");
  const todaySummaryList = document.getElementById("today-summary-list");

  const dayDialog = document.getElementById("day-dialog");
  const dayDialogDate = document.getElementById("day-dialog-date");
  const dayExistingSessions = document.getElementById("day-existing-sessions");
  const sessionGoalSelect = document.getElementById("session-goal");
  const sessionSubject = document.getElementById("session-subject");
  const sessionDuration = document.getElementById("session-duration");
  const sessionDescription = document.getElementById("session-description");
  const sessionSaveButton = document.getElementById("session-save");
  const sessionFormError = document.getElementById("session-form-error");

  if (!grassContainer) return; // no goals yet: nothing to wire up

  const WEEKDAY_LABELS = ["", "Mon", "", "Wed", "", "Fri", ""]; // Sun=0..Sat=6, sparse like GitHub
  const MONTH_LABELS = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];

  let allGoals = [];
  let currentGoalId = null;
  let currentYear = new Date().getFullYear();
  let selectedDate = null;

  function pinkForIntensity(intensity) {
    // intensity in [0,1]. Interpolate white -> strongest pink.
    // See explanation.md section 10 "Grass Color Normalization".
    if (intensity <= 0) return "#ffffff";
    const stops = [
      [0, [255, 255, 255]],
      [0.25, [251, 228, 233]], // --pink-100
      [0.5, [246, 201, 211]],  // --pink-200
      [0.75, [226, 131, 154]], // --pink-400
      [1, [209, 101, 127]],    // --pink-500
    ];
    for (let i = 1; i < stops.length; i++) {
      const [t0, c0] = stops[i - 1];
      const [t1, c1] = stops[i];
      if (intensity <= t1) {
        const t = t1 === t0 ? 1 : (intensity - t0) / (t1 - t0);
        const c = c0.map((v, idx) => Math.round(v + (c1[idx] - v) * t));
        return `rgb(${c[0]}, ${c[1]}, ${c[2]})`;
      }
    }
    return "rgb(209, 101, 127)";
  }

  function fmtDate(d) {
    const y = d.getFullYear();
    const m = String(d.getMonth() + 1).padStart(2, "0");
    const day = String(d.getDate()).padStart(2, "0");
    return `${y}-${m}-${day}`;
  }

  function populateYearSelect() {
    const thisYear = new Date().getFullYear();
    yearSelect.innerHTML = "";
    for (let y = thisYear; y >= thisYear - 4; y--) {
      const opt = document.createElement("option");
      opt.value = String(y);
      opt.textContent = String(y);
      yearSelect.appendChild(opt);
    }
    yearSelect.value = String(currentYear);
  }

  async function loadGoals() {
    const res = await fetch("/api/goals");
    allGoals = await res.json();
  }

  async function loadGrass() {
    if (!currentGoalId) return;
    const start = `${currentYear}-01-01`;
    const end = `${currentYear}-12-31`;
    const res = await fetch(`/api/grass?goalId=${encodeURIComponent(currentGoalId)}&start=${start}&end=${end}`);
    if (!res.ok) {
      grassContainer.innerHTML = '<p class="muted">Failed to load.</p>';
      return;
    }
    const payload = await res.json();
    renderGrass(payload.goal, payload.cells);
  }

  function renderGrass(goal, cells) {
    // Cells are Jan1..Dec31, one per day. Build GitHub-style columns:
    // each column is one week (Sun-Sat), rows are weekdays.
    const byDate = {};
    cells.forEach((c) => (byDate[c.date] = c));

    const firstDate = new Date(cells[0].date + "T00:00:00");
    const leadingBlank = firstDate.getDay(); // 0=Sun

    const cellsForGrid = [];
    for (let i = 0; i < leadingBlank; i++) cellsForGrid.push(null);
    cells.forEach((c) => cellsForGrid.push(c));
    while (cellsForGrid.length % 7 !== 0) cellsForGrid.push(null);

    const weekCount = cellsForGrid.length / 7;

    // Month labels: for each week-column, label it if that column contains
    // the 1st of a month.
    const monthLabels = [];
    for (let w = 0; w < weekCount; w++) {
      let label = "";
      for (let r = 0; r < 7; r++) {
        const cell = cellsForGrid[w * 7 + r];
        if (cell) {
          const d = new Date(cell.date + "T00:00:00");
          if (d.getDate() <= 7 && d.getDay() === r) {
            label = MONTH_LABELS[d.getMonth()];
          }
        }
      }
      monthLabels.push(label);
    }

    const monthsHTML = monthLabels.map((l) => `<span>${l}</span>`).join("");

    const weekdaysHTML = WEEKDAY_LABELS.map((l) => `<span>${l}</span>`).join("");

    let gridHTML = "";
    for (let w = 0; w < weekCount; w++) {
      for (let r = 0; r < 7; r++) {
        const cell = cellsForGrid[w * 7 + r];
        if (!cell) {
          gridHTML += `<button class="grass-cell is-empty-slot" tabindex="-1"></button>`;
          continue;
        }
        const color = pinkForIntensity(cell.intensity);
        const pct = Math.round(cell.ratio * 100);
        const title = `${cell.date} — ${cell.actualMin} / ${cell.targetMin} min (${pct}%)`;
        gridHTML += `<button class="grass-cell" style="background:${color}" data-date="${cell.date}" title="${title}"></button>`;
      }
    }

    grassContainer.innerHTML = `
      <div class="grass-scroll">
        <div class="grass-months">${monthsHTML}</div>
        <div class="grass-body">
          <div class="grass-weekdays">${weekdaysHTML}</div>
          <div class="grass-grid">${gridHTML}</div>
        </div>
        <div class="grass-legend">
          <span>Less</span>
          <span class="grass-legend-swatch" style="background:${pinkForIntensity(0)}"></span>
          <span class="grass-legend-swatch" style="background:${pinkForIntensity(0.25)}"></span>
          <span class="grass-legend-swatch" style="background:${pinkForIntensity(0.5)}"></span>
          <span class="grass-legend-swatch" style="background:${pinkForIntensity(0.75)}"></span>
          <span class="grass-legend-swatch" style="background:${pinkForIntensity(1)}"></span>
          <span>More (100%+)</span>
        </div>
      </div>`;

    grassContainer.querySelectorAll(".grass-cell[data-date]").forEach((btn) => {
      btn.addEventListener("click", () => openDayDialog(btn.dataset.date));
    });
  }

  async function loadTodaySummary() {
    const today = fmtDate(new Date());
    const res = await fetch(`/api/day?date=${today}`);
    if (!res.ok) return;
    const payload = await res.json();
    const totals = {}; // userId -> {name, minutes}
    payload.sessions.forEach((s) => {
      if (!totals[s.userId]) totals[s.userId] = { name: s.userDisplayName, minutes: 0 };
      totals[s.userId].minutes += s.durationMin;
    });
    // Make sure every partner shows up even with 0 minutes.
    document.querySelectorAll("[data-partner-id]"); // no-op placeholder for clarity

    const rows = Object.values(totals);
    if (rows.length === 0) {
      todaySummaryList.innerHTML = '<p class="muted">まだ記録がありません。</p>';
      return;
    }
    todaySummaryList.innerHTML = rows
      .map(
        (r) => `<div class="today-card">
          <div class="today-card-name">${escapeHTML(r.name || "—")}</div>
          <div class="today-card-value">${r.minutes} min</div>
        </div>`
      )
      .join("");
  }

  function escapeHTML(s) {
    const div = document.createElement("div");
    div.textContent = s;
    return div.innerHTML;
  }

  function populateSessionGoalSelect(preferredGoalId) {
    sessionGoalSelect.innerHTML = '<option value="">(no goal)</option>';
    allGoals
      .slice()
      .sort((a, b) => a.title.localeCompare(b.title))
      .forEach((g) => {
        const opt = document.createElement("option");
        opt.value = g.id;
        opt.textContent = g.title;
        sessionGoalSelect.appendChild(opt);
      });
    if (preferredGoalId) sessionGoalSelect.value = preferredGoalId;
  }

  async function openDayDialog(dateStr) {
    selectedDate = dateStr;
    dayDialogDate.textContent = dateStr;
    sessionFormError.hidden = true;
    sessionSubject.value = "";
    sessionDuration.value = "";
    sessionDescription.value = "";
    populateSessionGoalSelect(currentGoalId);
    dayExistingSessions.innerHTML = '<p class="muted">Loading…</p>';
    dayDialog.showModal();

    const res = await fetch(`/api/day?date=${dateStr}`);
    const payload = await res.json();
    renderExistingSessions(payload.sessions);
  }

  function renderExistingSessions(sessions) {
    if (sessions.length === 0) {
      dayExistingSessions.innerHTML = '<p class="muted">この日の記録はまだありません。</p>';
      return;
    }
    dayExistingSessions.innerHTML = sessions
      .map((s) => {
        const isMine = s.userId === window.STUDYLOG_USER_ID;
        return `<div class="existing-session">
          <div class="existing-session-top">
            <span>${escapeHTML(s.userDisplayName)} · ${escapeHTML(s.goalTitle || s.subject || "—")}</span>
            <span class="existing-session-duration">${s.durationMin} min</span>
          </div>
          ${s.description ? `<div class="existing-session-desc">${escapeHTML(s.description)}</div>` : ""}
          ${isMine ? `<button class="link-button danger delete-session-button" data-id="${s.id}">Delete</button>` : ""}
        </div>`;
      })
      .join("");

    dayExistingSessions.querySelectorAll(".delete-session-button").forEach((btn) => {
      btn.addEventListener("click", async () => {
        await fetch(`/api/sessions/${btn.dataset.id}`, { method: "DELETE" });
        const res = await fetch(`/api/day?date=${selectedDate}`);
        const payload = await res.json();
        renderExistingSessions(payload.sessions);
        loadGrass();
        loadTodaySummary();
      });
    });
  }

  sessionSaveButton.addEventListener("click", async () => {
    sessionFormError.hidden = true;
    const duration = sessionDuration.value === "" ? 0 : Number(sessionDuration.value);
    const description = sessionDescription.value.trim();
    if (duration === 0 && description === "") {
      sessionFormError.textContent = "時間か説明のどちらかを入力してください。";
      sessionFormError.hidden = false;
      return;
    }
    const body = {
      date: selectedDate,
      durationMin: duration,
      subject: sessionSubject.value.trim(),
      goalId: sessionGoalSelect.value,
      description,
    };
    const res = await fetch("/api/sessions", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    });
    if (!res.ok) {
      const err = await res.json().catch(() => ({}));
      sessionFormError.textContent = err.error || "保存に失敗しました。";
      sessionFormError.hidden = false;
      return;
    }
    sessionSubject.value = "";
    sessionDuration.value = "";
    sessionDescription.value = "";
    const dayRes = await fetch(`/api/day?date=${selectedDate}`);
    const payload = await dayRes.json();
    renderExistingSessions(payload.sessions);
    loadGrass();
    loadTodaySummary();
  });

  goalSelect.addEventListener("change", () => {
    currentGoalId = goalSelect.value;
    loadGrass();
  });
  yearSelect.addEventListener("change", () => {
    currentYear = Number(yearSelect.value);
    loadGrass();
  });

  (async function init() {
    populateYearSelect();
    await loadGoals();
    if (goalSelect.options.length > 0) {
      currentGoalId = goalSelect.value;
      await loadGrass();
    }
    loadTodaySummary();
  })();
})();
