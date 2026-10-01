// Keyboard shortcuts: a key listed in an element's data-keys clicks it,
// unless the user is typing in a field.
document.addEventListener("keydown", (e) => {
  if (e.ctrlKey || e.metaKey || e.altKey || e.target.closest("input, textarea, select")) {
    return;
  }
  for (const el of document.querySelectorAll("[data-keys]")) {
    if (el.dataset.keys.split(" ").includes(e.key)) {
      e.preventDefault();
      el.click();
      return;
    }
  }
});

// Forms marked data-autosubmit apply each change straight away, except
// changes inside something marked data-wait (such as a custom template,
// which is only complete once all its fields are filled in); those wait
// for the form's button.
for (const form of document.querySelectorAll("form[data-autosubmit]")) {
  form.addEventListener("change", (e) => {
    if (!e.target.closest("[data-wait]")) {
      form.requestSubmit();
    }
  });
}

// A form marked data-chart has a chart (#chart) that follows the form as
// it is filled in, before the changes are applied: after each change the
// chart is fetched again from the URL in data-chart. Options that don't
// make sense yet leave the old chart, dimmed, with the reason under it.
for (const form of document.querySelectorAll("form[data-chart]")) {
  let timer;
  let asked = 0;
  form.addEventListener("input", () => {
    clearTimeout(timer);
    timer = setTimeout(async () => {
      const n = ++asked;
      try {
        const res = await fetch(form.dataset.chart + "?" + new URLSearchParams(new FormData(form)));
        const text = await res.text();
        const chart = document.getElementById("chart");
        if (n !== asked || !chart) {
          return; // a later change is on its way
        }
        if (res.ok) {
          chart.outerHTML = text;
        } else {
          chart.classList.add("stale");
          chart.querySelector(".chart-note").textContent = text;
        }
      } catch {
        // The program may have been stopped; the chart stays as it was.
      }
    }, 150);
  });
}

// A button marked data-dialog opens the dialog with that ID, such as the
// one that asks before deleting a list.
for (const button of document.querySelectorAll("[data-dialog]")) {
  button.addEventListener("click", () => document.getElementById(button.dataset.dialog).showModal());
}

// A dialog the page comes with open, such as the one that asks which
// details to keep when merging the ticked entries, is opened again as a
// modal one, in front of the page.
for (const dialog of document.querySelectorAll("dialog[open][data-modal]")) {
  dialog.close();
  dialog.showModal();
}

// Send each form once, so that a double click or a held key cannot answer
// twice. Closing a dialog with its Cancel button sends nothing, so it
// doesn't count.
document.addEventListener("submit", (e) => {
  if (e.submitter?.getAttribute("formmethod") === "dialog") {
    return;
  }
  if (e.target.dataset.sent) {
    e.preventDefault();
  }
  e.target.dataset.sent = "yes";
});
