// Remembers the latest order per project in this browser, so a buyer who comes
// back is offered that order before making another.
(function () {
  const day = 24 * 60 * 60 * 1000;

  function key(project) {
    return 'dahlunas-order:' + project;
  }

  function remember() {
    const order = document.getElementById('order');
    if (!order) return;
    const data = order.dataset;
    try {
      const saved = JSON.parse(localStorage.getItem(key(data.project)) || 'null');
      const ended = data.state !== 'paid' && data.state !== 'pending' && data.state !== 'capture';
      if (ended) {
        if (saved && saved.path === data.path) localStorage.removeItem(key(data.project));
        return;
      }
      // Opening an older order must not replace a newer one.
      if (saved && saved.path !== data.path && saved.created > Number(data.created)) return;
      localStorage.setItem(key(data.project), JSON.stringify({
        path: data.path,
        state: data.state === 'paid' ? 'paid' : 'pending',
        created: Number(data.created)
      }));
    } catch (_) {}
  }

  function recall(project) {
    try {
      const saved = JSON.parse(localStorage.getItem(key(project)) || 'null');
      if (saved && /^\/orders\/[a-f0-9]{32}\/[a-f0-9]{64}$/.test(saved.path) && Date.now() - saved.created < day) return saved;
      localStorage.removeItem(key(project));
    } catch (_) {}
    return null;
  }

  function offer() {
    const notice = document.getElementById('recent-order');
    const form = document.getElementById('checkout');
    if (!notice || !form) return;
    const saved = recall(notice.dataset.project);
    notice.hidden = form.hidden = false;
    if (!saved) {
      notice.hidden = true;
      return;
    }
    form.hidden = true;
    notice.querySelectorAll('p').forEach(function (line) {
      line.hidden = line.dataset.state !== saved.state;
    });
    notice.querySelector('a').href = saved.path + '?lang=' + document.documentElement.lang;
    notice.querySelector('button').onclick = function () {
      notice.hidden = true;
      form.hidden = false;
      const first = form.querySelector('input:not([type=hidden])');
      if (first) first.focus();
    };
  }

  const form = document.getElementById('checkout');
  if (form) {
    let sent = false;
    form.addEventListener('submit', function (event) {
      if (sent) event.preventDefault();
      sent = true;
      // Allow another try if the page never left, such as after a lost connection.
      setTimeout(function () { sent = false; }, 15000);
    });
    window.addEventListener('pageshow', function () {
      sent = false;
      offer();
    });
  }

  remember();
  document.body.addEventListener('htmx:afterSettle', remember);
  offer();
})();
