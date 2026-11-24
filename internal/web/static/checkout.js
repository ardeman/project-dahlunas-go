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

  // Payment covers the screen, because Snap reports no height: anything smaller
  // leaves a gap or scrolls inside a page that scrolls too.
  function show(open) {
    const view = document.getElementById('payment');
    if (!view || view.hidden === !open) return;
    view.hidden = !open;
    document.body.classList.toggle('paying', open);
    // Name our own parts: Snap keeps its iframe in the body until it embeds it,
    // and an inert iframe takes no clicks.
    document.querySelectorAll('.sandbox, body > header, body > main, body > footer').forEach(function (part) {
      part.inert = open;
    });
    const target = document.getElementById(open ? 'payment-back' : 'snap-resume');
    if (target) target.focus();
  }

  function clearSnap() {
    const box = document.getElementById('snap-container');
    if (!box) return;
    show(false);
    if (window.snap && typeof window.snap.hide === 'function') {
      try { window.snap.hide(); } catch (_) {}
    }
    document.getElementById('payment').remove();
    const resume = document.getElementById('snap-resume');
    if (resume) resume.remove();
  }

  function openSnap(attempt) {
    const box = document.getElementById('snap-container');
    if (!box || !box.dataset.snapToken || box.dataset.snapReady) return;
    if (!window.snap || typeof window.snap.embed !== 'function') {
      if ((attempt || 0) < 40) {
        setTimeout(function () { openSnap((attempt || 0) + 1); }, 50);
        return;
      }
      show(false);
      fail(language() === 'en');
      return;
    }
    show(true);
    box.dataset.snapReady = '1';
    const en = language() === 'en';
    // Snap's own X removes its frame, so offer a way back to it. It reports a
    // close before a method is chosen, and a pending payment after.
    function closed() {
      delete box.dataset.snapReady;
      show(false);
    }
    window.snap.embed(box.dataset.snapToken, {
      embedId: 'snap-container',
      language: en ? 'en' : 'id',
      onClose: closed,
      onPending: closed,
      onSuccess: function () { window.location.reload(); },
      onError: function () {
        closed();
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

  // A poll may report that the payment ended elsewhere, such as in another tab.
  document.body.addEventListener('htmx:afterSettle', function () {
    const order = document.getElementById('order');
    const box = document.getElementById('snap-container');
    if (!order || !box) return;
    if (order.dataset.state !== 'pending' && order.dataset.state !== 'capture') clearSnap();
  });

  let changing = false;
  document.body.addEventListener('submit', function (event) {
    const question = event.target.dataset.confirm;
    if (!question) return;
    if (changing || !window.confirm(question)) {
      event.preventDefault();
      return;
    }
    changing = true;
    setTimeout(function () { changing = false; }, 15000);
  });

  const resume = document.getElementById('snap-resume');
  if (resume) {
    resume.addEventListener('click', function () {
      show(true);
      openSnap(0);
    });
    document.getElementById('payment-back').addEventListener('click', function () { show(false); });
    document.addEventListener('keydown', function (event) {
      if (event.key === 'Escape') show(false);
    });
  }

  openSnap(0);
})();
