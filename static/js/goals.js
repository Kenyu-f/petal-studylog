// StudyLog goals page behavior — create/edit/delete goals via the API.
(function () {
  "use strict";

  const newGoalButton = document.getElementById("new-goal-button");
  const editButtons = document.querySelectorAll(".edit-goal-button");

  const dialog = document.getElementById("goal-dialog");
  const dialogTitle = document.getElementById("goal-dialog-title");
  const idField = document.getElementById("goal-id");
  const titleField = document.getElementById("goal-title");
  const descField = document.getElementById("goal-description");
  const parentField = document.getElementById("goal-parent");
  const typeField = document.getElementById("goal-type");
  const sharedField = document.getElementById("goal-shared");
  const progressModeField = document.getElementById("goal-progress-mode");
  const targetFields = document.getElementById("goal-target-fields");
  const targetValueField = document.getElementById("goal-target-value");
  const targetPeriodField = document.getElementById("goal-target-period");
  const saveButton = document.getElementById("goal-save-button");
  const deleteButton = document.getElementById("goal-delete-button");
  const errorEl = document.getElementById("goal-form-error");

  let allGoals = [];

  async function loadGoals() {
    const res = await fetch("/api/goals");
    allGoals = await res.json();
  }

  function populateParentSelect(excludeId) {
    parentField.innerHTML = '<option value="">— (top level)</option>';
    allGoals
      .filter((g) => g.id !== excludeId)
      .sort((a, b) => a.title.localeCompare(b.title))
      .forEach((g) => {
        const opt = document.createElement("option");
        opt.value = g.id;
        opt.textContent = g.title;
        parentField.appendChild(opt);
      });
  }

  function updateTargetFieldsVisibility() {
    const mode = progressModeField.value;
    targetFields.hidden = mode === "none";
    if (mode === "cumulative") {
      targetPeriodField.value = "total";
    } else if (mode === "rate" && targetPeriodField.value === "total") {
      targetPeriodField.value = "day";
    }
  }
  progressModeField.addEventListener("change", updateTargetFieldsVisibility);

  function resetForm() {
    idField.value = "";
    titleField.value = "";
    descField.value = "";
    parentField.value = "";
    typeField.value = "short-term";
    sharedField.value = "false";
    progressModeField.value = "none";
    targetValueField.value = "";
    targetPeriodField.value = "day";
    errorEl.hidden = true;
    deleteButton.hidden = true;
    updateTargetFieldsVisibility();
  }

  async function openForCreate() {
    await loadGoals();
    resetForm();
    populateParentSelect(null);
    dialogTitle.textContent = "New goal";
    dialog.showModal();
  }

  async function openForEdit(goalId) {
    await loadGoals();
    const g = allGoals.find((x) => x.id === goalId);
    if (!g) return;
    resetForm();
    populateParentSelect(goalId);
    dialogTitle.textContent = "Edit goal";
    idField.value = g.id;
    titleField.value = g.title;
    descField.value = g.description || "";
    parentField.value = g.parentGoalId || "";
    typeField.value = g.type;
    sharedField.value = g.shared ? "true" : "false";
    progressModeField.value = g.progressMode || "none";
    targetValueField.value = g.targetValue || "";
    targetPeriodField.value = g.targetPeriod || "day";
    updateTargetFieldsVisibility();
    deleteButton.hidden = false;
    dialog.showModal();
  }

  newGoalButton.addEventListener("click", openForCreate);
  editButtons.forEach((btn) => {
    btn.addEventListener("click", () => openForEdit(btn.dataset.goalId));
  });

  saveButton.addEventListener("click", async () => {
    errorEl.hidden = true;
    if (!titleField.value.trim()) {
      errorEl.textContent = "Title is required.";
      errorEl.hidden = false;
      return;
    }
    const body = {
      title: titleField.value.trim(),
      description: descField.value.trim(),
      parentGoalId: parentField.value,
      type: typeField.value,
      shared: sharedField.value === "true",
      progressMode: progressModeField.value,
      targetValue: targetFields.hidden ? 0 : Number(targetValueField.value || 0),
      targetUnit: targetFields.hidden ? "" : "min",
      targetPeriod: targetFields.hidden ? "" : targetPeriodField.value,
    };
    const id = idField.value;
    const res = await fetch(id ? `/api/goals/${id}` : "/api/goals", {
      method: id ? "PUT" : "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    });
    if (!res.ok) {
      const err = await res.json().catch(() => ({}));
      errorEl.textContent = err.error || "Failed to save.";
      errorEl.hidden = false;
      return;
    }
    window.location.reload();
  });

  deleteButton.addEventListener("click", async () => {
    const id = idField.value;
    if (!id) return;
    if (!confirm("Delete this goal? This cannot be undone.")) return;
    const res = await fetch(`/api/goals/${id}`, { method: "DELETE" });
    if (!res.ok) {
      const err = await res.json().catch(() => ({}));
      errorEl.textContent = err.error || "Failed to delete (does it have sub-goals?).";
      errorEl.hidden = false;
      return;
    }
    window.location.reload();
  });
})();
