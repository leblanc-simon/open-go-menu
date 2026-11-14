// Progressive enhancement only: every form works without JavaScript.

const finePointer = window.matchMedia("(pointer: fine)").matches;

// enhance sets up what the listeners below cannot delegate, on the page and
// again on each page the script swaps in.
function enhance(root) {
  root.querySelectorAll("input[data-autocomplete][list]").forEach(autocomplete);
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
    applyPending(next);

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
  // Ticking a line of the shopping list never waits for the network.
  if (form.matches("[data-tick]")) {
    event.preventDefault();
    tick(form, event.submitter);
    return;
  }
  // Offline, a post could only end on the browser's error page.
  if (!navigator.onLine) {
    event.preventDefault();
    showOffline();
    return;
  }
  // Leaving the account: the pages kept offline go first, for the browsers
  // that ignore the Clear-Site-Data header of the response.
  if (form.matches("[data-logout]") && "caches" in window) {
    event.preventDefault();
    outbox.length = 0;
    saveOutbox();
    caches
      .keys()
      .then((keys) => Promise.all(keys.map((key) => caches.delete(key))))
      .finally(() => form.submit());
    return;
  }
  if (!form.closest("[data-in-place]")) {
    // The page the form leads to must come from the server, however slow.
    navigator.serviceWorker?.controller?.postMessage({ type: "fresh" });
    return;
  }
  event.preventDefault();
  if (!busy) {
    stopCarrying();
    submitInPlace(form, event.submitter);
  }
});

// ── Shopping list: instant, and offline ──────────────────────────────────
// A tick moves the line at once, to the basket or back to its aisle; the
// change waits in an outbox, kept across reloads, until the server has it.
// Offline, the ticks pile up there and leave once back online; the page shown
// meanwhile — even the saved copy — gets them applied.

const OUTBOX = "opengomenu-outbox";
let outbox = [];
try {
  outbox = JSON.parse(localStorage.getItem(OUTBOX)) || [];
} catch {
  // Without storage the ticks still leave, only not across a reload.
}

function saveOutbox() {
  try {
    if (outbox.length) {
      localStorage.setItem(OUTBOX, JSON.stringify(outbox));
    } else {
      localStorage.removeItem(OUTBOX);
    }
  } catch {
    // Same as above.
  }
}

// placeLine shows a line ticked (in the basket) or not (in its aisle), at its
// place in the list.
function placeLine(line, ticked) {
  const button = line.querySelector(".tick");
  line.classList.toggle("line--checked", ticked);
  button.value = ticked ? "untick" : "tick";
  button.setAttribute("aria-pressed", String(ticked));
  button.setAttribute("aria-label", ticked ? button.dataset.untickLabel : button.dataset.tickLabel);
  line.querySelectorAll("input[name='anchor']").forEach((input) => {
    input.value = ticked ? "done" : line.dataset.group;
  });

  const from = line.closest(".aisle");
  const to = document.getElementById(ticked ? "done" : line.dataset.group);
  if (!from || !to || from === to) {
    return;
  }
  // Keyboard users go on with the next line of the list they were in.
  let focus = null;
  if (document.activeElement === button) {
    const near = line.nextElementSibling || line.previousElementSibling;
    focus = near?.querySelector(".tick") || button;
  }
  const lines = to.querySelector(".lines");
  const rank = Number(line.dataset.rank);
  const next = [...lines.children].find((other) => Number(other.dataset.rank) > rank);
  lines.insertBefore(line, next || null);
  to.hidden = false;
  from.hidden = !from.querySelector(".line");
  focus?.focus({ preventScroll: focus !== button });
}

function tick(form, submitter) {
  const button = submitter || form.querySelector(".tick");
  const action = button.value;
  const url = form.getAttribute("action");
  // Read before placeLine turns the button the other way.
  const body = new URLSearchParams(new FormData(form, button)).toString();
  placeLine(form.closest(".line"), action === "tick");
  // The last change of a line is the one that counts.
  outbox = outbox.filter((op) => op.url !== url || op.sending);
  outbox.push({ url, action, body });
  saveOutbox();
  updateOfflineBar();
  flush();
}

// applyPending shows the ticks still in the outbox on a page from the server
// or the cache.
function applyPending(root) {
  for (const op of outbox) {
    const form = root.querySelector(`form[data-tick][action="${CSS.escape(op.url)}"]`);
    if (form) {
      placeLine(form.closest(".line"), op.action === "tick");
    }
  }
}

let flushing = false;
let retry = null;

// flush sends the outbox, in order. A refused tick (the list was replaced,
// the article is gone) is dropped; a server or network failure keeps it for
// later.
async function flush() {
  if (flushing || !outbox.length || !navigator.onLine) {
    return;
  }
  flushing = true;
  clearTimeout(retry);
  let sent = false;
  try {
    while (outbox.length) {
      const op = outbox[0];
      op.sending = true;
      let res;
      try {
        res = await fetch(op.url, { method: "POST", body: new URLSearchParams(op.body) });
      } catch {
        op.sending = false;
        retry = setTimeout(flush, 30000);
        break;
      }
      op.sending = false;
      if (res.status >= 500 || res.status === 429 || new URL(res.url).pathname === "/login") {
        retry = setTimeout(flush, 30000);
        break;
      }
      outbox = outbox.filter((other) => other !== op);
      saveOutbox();
      sent = true;
    }
  } finally {
    flushing = false;
    updateOfflineBar();
  }
  // The saved copy of the list catches up, for the next time offline.
  if (sent && !outbox.length) {
    fetch("/shopping").catch(() => {});
  }
}

window.addEventListener("online", flush);
document.addEventListener("visibilitychange", () => {
  if (document.visibilityState === "visible") {
    flush();
  }
});

// ── Installed application ─────────────────────────────────────────────────
// The service worker makes the application installable and serves the saved
// pages when the network is missing or slow; a bar says so.

function showBar(text, action) {
  let bar = document.querySelector(".offline-bar");
  if (!bar) {
    bar = document.createElement("div");
    bar.className = "offline-bar";
    bar.setAttribute("role", "status");
    document.body.prepend(bar);
  }
  const p = document.createElement("p");
  p.className = "offline-bar__text";
  p.textContent = text;
  bar.replaceChildren(p);
  if (action) {
    bar.append(action);
  }
}

function hideBar() {
  document.querySelector(".offline-bar")?.remove();
}

let stale = false;
let updated = false;

// pageURL is the address of the page as the service worker saw it.
function pageURL() {
  return location.href.split("#")[0];
}

function updateOfflineBar() {
  const body = document.body.dataset;
  if (!navigator.onLine) {
    let text = body.offline;
    if (outbox.length) {
      text += " " + body.offlineTicks;
    }
    showBar(text);
  } else if (stale) {
    let reload = null;
    if (updated) {
      reload = document.createElement("button");
      reload.type = "button";
      reload.className = "link-button offline-bar__reload";
      reload.textContent = body.reload;
      reload.addEventListener("click", () => location.reload());
    }
    showBar(body.stale, reload);
  } else {
    hideBar();
  }
}

function showOffline() {
  updateOfflineBar();
}

window.addEventListener("offline", updateOfflineBar);
window.addEventListener("online", updateOfflineBar);

if ("serviceWorker" in navigator) {
  navigator.serviceWorker.register("/sw.js").catch(() => {
    // Without it the application still works, only not offline.
  });
  navigator.serviceWorker.addEventListener("message", (event) => {
    const data = event.data || {};
    if (data.url !== pageURL()) {
      return;
    }
    if (data.type === "stale") {
      stale = true;
      updated = updated || data.updated;
    } else if (data.type === "updated") {
      updated = true;
    }
    updateOfflineBar();
  });
  const controller = navigator.serviceWorker.controller;
  if (controller) {
    controller.postMessage({ type: "hello", url: pageURL() });
    // The week and the shopping list stay at hand offline.
    if (document.querySelector(".masthead__nav") && navigator.onLine) {
      controller.postMessage({ type: "warm", urls: ["/", "/shopping"].filter((u) => u !== location.pathname) });
    }
  }
}

// ── Suggestions while typing a name ───────────────────────────────────────
// The names of a datalist show under the field, matched whatever the case
// and the accents. Where the names are the existing entries of the list
// (data-autocomplete="existing"), picking one goes to it instead, and a name
// close to an existing one — accents, case, plural — is pointed out: no
// duplicates by mistake.

function fold(text) {
  return text
    .normalize("NFD")
    .replace(/\p{M}/gu, "")
    .toLowerCase()
    .replace(/œ/g, "oe")
    .replace(/æ/g, "ae")
    .replace(/\s+/g, " ")
    .trim();
}

// stem drops the plural marks, word by word: "haricots verts" ~ "haricot vert".
function stem(folded) {
  return folded
    .split(" ")
    .map((word) => (word.length > 3 ? word.replace(/[sx]$/, "") : word))
    .join(" ");
}

let suggestCount = 0;

function autocomplete(input) {
  const datalist = document.getElementById(input.getAttribute("list"));
  if (!datalist) {
    return;
  }
  const existing = input.dataset.autocomplete === "existing";
  const options = [...datalist.options].map((o) => ({
    name: o.value,
    folded: fold(o.value),
    target: o.dataset.target,
  }));
  input.removeAttribute("list");

  const id = `suggest-${++suggestCount}`;
  const box = document.createElement("ul");
  box.className = "suggest";
  box.id = id;
  box.setAttribute("role", "listbox");
  box.hidden = true;
  input.after(box);
  input.parentElement.classList.add("suggest-host");
  input.setAttribute("role", "combobox");
  input.setAttribute("aria-autocomplete", "list");
  input.setAttribute("aria-controls", id);
  input.setAttribute("aria-expanded", "false");

  let note = null;
  if (existing) {
    note = document.createElement("p");
    note.className = "suggest-note";
    note.id = `${id}-note`;
    note.setAttribute("aria-live", "polite");
    note.hidden = true;
    box.after(note);
    input.setAttribute("aria-describedby", [input.getAttribute("aria-describedby"), note.id].filter(Boolean).join(" "));
  }

  let matches = [];
  let active = -1;

  function close() {
    box.hidden = true;
    input.setAttribute("aria-expanded", "false");
    input.removeAttribute("aria-activedescendant");
    active = -1;
  }

  function highlight(i) {
    active = i;
    [...box.children].forEach((li, j) => li.setAttribute("aria-selected", String(j === i)));
    if (i >= 0) {
      input.setAttribute("aria-activedescendant", `${id}-${i}`);
      box.children[i].scrollIntoView({ block: "nearest" });
    } else {
      input.removeAttribute("aria-activedescendant");
    }
  }

  function choose(option) {
    close();
    if (existing && option.target) {
      // Already there: go to it rather than typing it twice.
      const field = document.getElementById(option.target);
      field?.scrollIntoView({ block: "center" });
      field?.focus({ preventScroll: true });
      return;
    }
    input.value = option.name;
    update(false);
  }

  function update(open = true) {
    const q = fold(input.value);
    if (note) {
      const same = q && options.find((o) => o.folded === q || stem(o.folded) === stem(q));
      note.hidden = !same;
      if (same) {
        const name = document.createElement("strong");
        name.textContent = same.name;
        note.replaceChildren(input.dataset.similar + " ", name);
      }
    }
    if (!q || !open) {
      close();
      return;
    }
    // Names starting with the text first, then those with a word starting
    // with it, then those holding it.
    const score = (o) => (o.folded.startsWith(q) ? 0 : o.folded.includes(" " + q) ? 1 : 2);
    matches = options
      .filter((o) => o.folded.includes(q) && (existing || o.folded !== q))
      .sort((a, b) => score(a) - score(b))
      .slice(0, 8);
    box.replaceChildren(
      ...matches.map((o, i) => {
        const li = document.createElement("li");
        li.id = `${id}-${i}`;
        li.className = "suggest__option";
        li.setAttribute("role", "option");
        li.setAttribute("aria-selected", "false");
        li.textContent = o.name;
        if (existing) {
          const tag = document.createElement("span");
          tag.className = "suggest__tag";
          tag.textContent = input.dataset.existing;
          li.append(" ", tag);
        }
        // pointerdown would blur the field first and close the list.
        li.addEventListener("pointerdown", (event) => event.preventDefault());
        li.addEventListener("click", () => choose(o));
        return li;
      }),
    );
    box.hidden = !matches.length;
    input.setAttribute("aria-expanded", String(!box.hidden));
    active = -1;
  }

  input.addEventListener("input", () => update());
  input.addEventListener("blur", close);
  input.addEventListener("keydown", (event) => {
    if (box.hidden) {
      if (event.key === "ArrowDown" && input.value) {
        update();
        event.preventDefault();
      }
      return;
    }
    switch (event.key) {
      case "ArrowDown":
        highlight((active + 1) % matches.length);
        break;
      case "ArrowUp":
        highlight(active <= 0 ? matches.length - 1 : active - 1);
        break;
      case "Enter":
        if (active < 0) {
          close();
          return; // the form is sent
        }
        choose(matches[active]);
        break;
      case "Escape":
        close();
        break;
      default:
        return;
    }
    event.preventDefault();
  });
  if (input.value) {
    update(false);
  }
}

// ── Start ─────────────────────────────────────────────────────────────────

enhance(document);
applyPending(document);
updateOfflineBar();
flush();
