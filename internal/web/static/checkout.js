(function () {
  function language() {
    return document.documentElement.lang === 'en' ? 'en' : 'id';
  }

  function fail(en) {
    const message = document.getElementById('payment-error');
    if (message) {
      message.textContent = en ? 'Payment has not loaded. Try again.' : 'Pembayaran belum dimuat. Coba lagi.';
    }
  }

  function clearSnap() {
    const box = document.getElementById('snap-container');
    if (!box) return;
    if (window.snap && typeof window.snap.hide === 'function') {
      try { window.snap.hide(); } catch (_) {}
    }
    box.remove();
  }

  function openSnap(attempt) {
    const box = document.getElementById('snap-container');
    if (!box || !box.dataset.snapToken || box.dataset.snapReady) return;
    if (!window.snap || typeof window.snap.embed !== 'function') {
      if ((attempt || 0) < 40) {
        setTimeout(function () { openSnap((attempt || 0) + 1); }, 50);
        return;
      }
      fail(language() === 'en');
      return;
    }
    box.dataset.snapReady = '1';
    const en = language() === 'en';
    window.snap.embed(box.dataset.snapToken, {
      embedId: 'snap-container',
      language: en ? 'en' : 'id',
      onSuccess: function () { window.location.reload(); },
      onError: function () {
        const current = document.getElementById('payment-error');
        if (current) current.textContent = en ? 'Payment failed. Try again.' : 'Pembayaran gagal. Coba lagi.';
      }
    });
  }

  document.body.addEventListener('htmx:afterSwap', function (event) {
    if (event.detail && event.detail.target && event.detail.target.id === 'order' &&
        event.detail.target.querySelector('.stamp')) {
      clearSnap();
    }
  });

  openSnap(0);
})();
