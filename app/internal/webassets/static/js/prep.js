// Tab switching only — structure.js and editor.js each own their own panel
// and don't know about tabs at all.
document.querySelectorAll('.prep-tab').forEach(btn => {
  btn.addEventListener('click', () => {
    document.querySelectorAll('.prep-tab').forEach(b => b.classList.toggle('active', b === btn));
    document.querySelectorAll('.prep-panel').forEach(p => {
      p.hidden = p.dataset.panel !== btn.dataset.tab;
    });
  });
});
