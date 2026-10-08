// Progressive enhancement only: every form works without JavaScript.
document.querySelectorAll("select[data-autosubmit]").forEach((select) => {
  const form = select.form;
  form.querySelectorAll("[data-autosubmit-fallback]").forEach((button) => {
    button.hidden = true;
  });
  select.addEventListener("change", () => form.requestSubmit());
});
