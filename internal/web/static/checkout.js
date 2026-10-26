document.addEventListener('click', function (event) {
  const button = event.target.closest('[data-snap-token]');
  if (!button) return;
  const message = document.getElementById('payment-error');
  if (!window.snap) {
    message.textContent = 'Pembayaran belum dimuat. Coba lagi. / Payment has not loaded. Try again.';
    return;
  }
  button.disabled = true;
  const release = function () { button.disabled = false; };
  window.snap.pay(button.dataset.snapToken, {
    onSuccess: function () { window.location.reload(); },
    onPending: function () { window.location.reload(); },
    onError: function () {
      release();
      const current = document.getElementById('payment-error');
      if (current) current.textContent = 'Pembayaran gagal. Coba lagi. / Payment failed. Try again.';
    },
    onClose: release
  });
});
