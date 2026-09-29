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

// A button marked data-dialog opens the dialog with that ID, such as the
// one that asks before deleting a list.
for (const button of document.querySelectorAll("[data-dialog]")) {
  button.addEventListener("click", () => document.getElementById(button.dataset.dialog).showModal());
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
