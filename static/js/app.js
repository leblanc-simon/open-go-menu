// Progressive enhancement only: every form works without JavaScript.

const finePointer = window.matchMedia("(pointer: fine)").matches;

// enhance sets up what the listeners below cannot delegate, on the page and
// again on each page the script swaps in.
function enhance(root) {
  root.querySelectorAll("[data-autosubmit-fallback]").forEach((button) => {
    button.hidden = true;
  });
  if (finePointer) {
    root.querySelectorAll(".meal[data-movable]").forEach((meal) => {
      meal.draggable = true;
      meal.classList.add("meal--draggable");
    });
    root.querySelectorAll("[data-sortable] .drag-handle").forEach((handle) => {
      handle.hidden = false;
      handle.draggable = true;
    });
  }
}

document.addEventListener("change", (event) => {
  if (event.target.matches("select[data-autosubmit]")) {
    event.target.form.requestSubmit();
  }
});

// ── Moving a meal ──────────────────────────────────────────────────────────
// Both ways end in the "Move" form of the moved meal, which the server also
// gets without JavaScript.

function moveMeal(source, target) {
  const form = source.querySelector(".meal__actions");
  form.elements.to.value = target.dataset.slot;
  form.requestSubmit(form.querySelector("[data-move]"));
}

// Drag and drop, on precise pointers.
let dragged = null;

function clearDrop() {
  document.querySelectorAll(".meal--drop").forEach((m) => m.classList.remove("meal--drop"));
}

function dropTarget(event) {
  const meal = event.target.closest?.(".meal[data-slot]");
  return dragged && meal && meal !== dragged ? meal : null;
}

document.addEventListener("dragstart", (event) => {
  if (event.target.matches?.(".meal[data-movable]")) {
    dragged = event.target;
    event.dataTransfer.effectAllowed = "move";
    event.dataTransfer.setData("text/plain", dragged.dataset.slot);
    dragged.classList.add("meal--dragging");
  }
});

document.addEventListener("dragend", () => {
  dragged?.classList.remove("meal--dragging");
  dragged = null;
  clearDrop();
});

document.addEventListener("dragover", (event) => {
  const meal = dropTarget(event);
  if (meal) {
    event.preventDefault();
    event.dataTransfer.dropEffect = "move";
    meal.classList.add("meal--drop");
  }
});

document.addEventListener("dragleave", (event) => {
  const meal = dropTarget(event);
  if (meal && !meal.contains(event.relatedTarget)) {
    meal.classList.remove("meal--drop");
  }
});

document.addEventListener("drop", (event) => {
  const meal = dropTarget(event);
  if (meal) {
    event.preventDefault();
    moveMeal(dragged, meal);
  }
});

// Pick and place, on any pointer: "Move" picks the meal up, every other meal
// becomes a button to put it there, a bar at the bottom offers to cancel.
let carried = null;

function stopCarrying() {
  if (!carried) {
    return;
  }
  carried.classList.remove("meal--carried");
  carried.closest(".days")?.classList.remove("days--moving");
  document.querySelectorAll(".meal__drop, .move-bar").forEach((el) => el.remove());
  carried = null;
}

function carry(meal) {
  stopCarrying();
  carried = meal;
  const days = meal.closest(".days");
  meal.classList.add("meal--carried");
  days.classList.add("days--moving");

  days.querySelectorAll(".meal[data-slot]").forEach((target) => {
    if (target === meal) {
      return;
    }
    const button = document.createElement("button");
    button.type = "button";
    button.className = "meal__drop";
    button.textContent = target.classList.contains("meal--empty") ? days.dataset.moveHere : days.dataset.moveSwap;
    button.addEventListener("click", () => {
      stopCarrying();
      moveMeal(meal, target);
    });
    target.append(button);
  });

  const bar = document.createElement("div");
  bar.className = "move-bar";
  bar.setAttribute("role", "status");
  const text = document.createElement("p");
  text.className = "move-bar__text";
  text.textContent = meal.dataset.movePrompt;
  const cancel = document.createElement("button");
  cancel.type = "button";
  cancel.className = "button move-bar__cancel";
  cancel.textContent = days.dataset.moveCancel;
  cancel.addEventListener("click", stopCarrying);
  bar.append(text, cancel);
  document.body.append(bar);
}

document.addEventListener("click", (event) => {
  const summary = event.target.closest(".meal__move > summary");
  if (summary) {
    event.preventDefault(); // the select stays closed: the meals are the targets
    const meal = summary.closest(".meal");
    if (meal === carried) {
      stopCarrying();
    } else {
      carry(meal);
    }
  }
});

document.addEventListener("keydown", (event) => {
  if (event.key === "Escape") {
    stopCarrying();
  }
});

// ── Ordering a list by drag and drop (the aisles), on precise pointers ─────
// The row follows the pointer through the list; once dropped, the new order
// fills the form named by data-sortable, sent like the week's forms. The
// up and down buttons stay, for the keyboard and without script.

let sorting = null;
let sortStart = [];

function sortRows(list) {
  return [...list.querySelectorAll("[data-sort-id]")];
}

document.addEventListener("dragstart", (event) => {
  const handle = event.target.closest?.("[data-sortable] .drag-handle");
  if (!handle) {
    return;
  }
  sorting = handle.closest("[data-sort-id]");
  sortStart = sortRows(sorting.closest("[data-sortable]"));
  event.dataTransfer.effectAllowed = "move";
  event.dataTransfer.setData("text/plain", sorting.dataset.sortId);
  event.dataTransfer.setDragImage(sorting, 16, 16);
  sorting.classList.add("names__row--dragging");
});

// sortTarget returns the row of the dragged one's list under the pointer.
function sortTarget(event) {
  const row = sorting && event.target.closest?.("[data-sort-id]");
  return row && row.parentElement === sorting.parentElement ? row : null;
}

// A row moved under the pointer becomes the target: accepting dragenter as
// well as dragover keeps the drop allowed.
document.addEventListener("dragenter", (event) => {
  if (sortTarget(event)) {
    event.preventDefault();
  }
});

document.addEventListener("dragover", (event) => {
  const row = sortTarget(event);
  if (!row) {
    return;
  }
  event.preventDefault();
  event.dataTransfer.dropEffect = "move";
  if (row !== sorting) {
    const box = row.getBoundingClientRect();
    const after = event.clientY > box.top + box.height / 2;
    row.parentElement.insertBefore(sorting, after ? row.nextSibling : row);
  }
});

document.addEventListener("drop", (event) => {
  if (sorting && event.target.closest?.("[data-sortable]")) {
    event.preventDefault();
  }
});

document.addEventListener("dragend", (event) => {
  if (!sorting) {
    return;
  }
  const list = sorting.closest("[data-sortable]");
  sorting.classList.remove("names__row--dragging");
  sorting = null;
  const rows = sortRows(list);
  if (event.dataTransfer.dropEffect === "none") {
    // Dropped outside the list or cancelled with Escape: put it back.
    const anchor = rows[rows.length - 1].nextSibling;
    sortStart.forEach((row) => list.insertBefore(row, anchor));
    return;
  }
  if (rows.every((row, i) => row === sortStart[i])) {
    return;
  }
  const form = document.getElementById(list.dataset.sortable);
  form.querySelectorAll("input[name='id']").forEach((input) => input.remove());
  rows.forEach((row) => {
    const input = document.createElement("input");
    input.type = "hidden";
    input.name = "id";
    input.value = row.dataset.sortId;
    form.append(input);
  });
  form.requestSubmit();
});

// ── Forms of the week and the shopping list without reloading ─────────────
// The form is posted in the background and the page it leads to replaces the
// current one, kept where it was on screen: the meal acted on stays put, and
// the confirmation shows as a toast instead of pushing the week down.

let busy = false;

function showToast(flash) {
  document.querySelectorAll(".flash--toast").forEach((el) => el.remove());
  flash.classList.add("flash--toast");
  document.body.append(flash);
  setTimeout(() => flash.remove(), 4000);
}

async function submitInPlace(form, submitter) {
  const main = document.getElementById("main");
  const slot = form.closest("[data-slot]")?.dataset.slot;
  const before = slot ? main.querySelector(`[data-slot="${slot}"]`).getBoundingClientRect().top : null;
  const scroll = window.scrollY;

  busy = true;
  main.setAttribute("aria-busy", "true");
  try {
    // getAttribute: a field named "action" shadows form.action.
    const res = await fetch(form.getAttribute("action"), { method: "POST", body: new FormData(form, submitter) });
    const page = new DOMParser().parseFromString(await res.text(), "text/html");
    const next = page.getElementById("main");
    if (!next) {
      throw new Error("no page in the response");
    }

    const flash = next.querySelector(":scope > .flash");
    flash?.remove();
    main.replaceWith(next);
    document.title = page.title;
    const url = new URL(res.url);
    if (res.redirected && url.pathname + url.search !== location.pathname + location.search) {
      history.replaceState(null, "", url.pathname + url.search);
    }
    enhance(next);

    const after = slot && next.querySelector(`[data-slot="${slot}"]`);
    if (after) {
      window.scrollBy(0, after.getBoundingClientRect().top - before);
      // Keyboard users go on from the same button, when it is still there.
      if (submitter?.name) {
        after
          .querySelector(`button[name="${submitter.name}"][value="${submitter.value}"]`)
          ?.focus({ preventScroll: true });
      }
    } else {
      window.scrollTo(0, scroll);
    }
    if (flash) {
      showToast(flash);
    }
  } catch {
    // Anything unexpected: let the browser post the form the usual way,
    // carrying the button that was pressed.
    if (submitter?.name) {
      const field = document.createElement("input");
      field.type = "hidden";
      field.name = submitter.name;
      field.value = submitter.value;
      form.append(field);
    }
    form.submit();
  } finally {
    busy = false;
  }
}

document.addEventListener("submit", (event) => {
  const form = event.target;
  if (form.getAttribute("method") !== "post") {
    return;
  }
  // Offline, a post could only end on the browser's error page.
  if (!navigator.onLine) {
    event.preventDefault();
    showOffline();
    return;
  }
  if (!form.closest("[data-in-place]")) {
    return;
  }
  event.preventDefault();
  if (!busy) {
    stopCarrying();
    submitInPlace(form, event.submitter);
  }
});

// ── Installed application ─────────────────────────────────────────────────
// The service worker makes the application installable and keeps the last
// pages seen readable offline; a bar says when the page may be out of date.

function showOffline() {
  if (document.querySelector(".offline-bar")) {
    return;
  }
  const bar = document.createElement("p");
  bar.className = "offline-bar";
  bar.setAttribute("role", "status");
  bar.textContent = document.body.dataset.offline;
  document.body.prepend(bar);
}

window.addEventListener("offline", showOffline);
window.addEventListener("online", () => document.querySelector(".offline-bar")?.remove());
if (!navigator.onLine) {
  showOffline();
}

if ("serviceWorker" in navigator) {
  navigator.serviceWorker.register("/sw.js").catch(() => {
    // Without it the application still works, only not offline.
  });
}

enhance(document);
