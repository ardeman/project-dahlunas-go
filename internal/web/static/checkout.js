document.addEventListener('click', function (event) {
  const button = event.target.closest('[data-snap-token]');
  if (!button) return;
  const en = document.documentElement.lang === 'en';
  const message = document.getElementById('payment-error');
  if (!window.snap) {
    message.textContent = en ? 'Payment has not loaded. Try again.' : 'Pembayaran belum dimuat. Coba lagi.';
    return;
  }
  button.disabled = true;
  const release = function () { button.disabled = false; };
  window.snap.pay(button.dataset.snapToken, {
    language: en ? 'en' : 'id',
    onSuccess: function () { window.location.reload(); },
    onPending: function () { window.location.reload(); },
    onError: function () {
      release();
      const current = document.getElementById('payment-error');
      if (current) current.textContent = en ? 'Payment failed. Try again.' : 'Pembayaran gagal. Coba lagi.';
    },
    onClose: release
  });
});
