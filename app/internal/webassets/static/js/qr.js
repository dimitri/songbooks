// showQR(url) — a tiny lightweight modal with a scannable QR code for
// one link, so it can be shared without typing it into WhatsApp/contacts
// first. The PNG itself comes from the server (/api/qr) rather than a
// client-side encoder, so this file stays a few lines instead of
// vendoring a whole QR-encoding library.
function showQR(url) {
  const overlay = document.createElement('div');
  overlay.className = 'qr-modal-overlay';
  overlay.innerHTML = `
    <div class="qr-modal">
      <img class="qr-modal-img" alt="QR code for this link">
      <p class="hint qr-modal-url"></p>
      <button type="button" class="qr-modal-close">Close</button>
    </div>`;
  overlay.querySelector('.qr-modal-img').src = '/api/qr?data=' + encodeURIComponent(url);
  overlay.querySelector('.qr-modal-url').textContent = url;
  const close = () => overlay.remove();
  overlay.addEventListener('click', (e) => { if (e.target === overlay) close(); });
  overlay.querySelector('.qr-modal-close').addEventListener('click', close);
  document.body.appendChild(overlay);
}
