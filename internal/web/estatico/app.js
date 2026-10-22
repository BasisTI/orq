// Abre os <details> ancestrais de location.hash (na carga e em hashchange) e rola até o alvo.
// Nada mais: o resto é do htmx e do servidor.
(function () {
  'use strict';
  function abrirAlvo() {
    var id;
    try { id = decodeURIComponent(location.hash.slice(1)); } catch (e) { return; }
    var el = id && document.getElementById(id);
    if (!el) return;
    for (var n = el; n; n = n.parentElement) if (n.tagName === 'DETAILS') n.open = true;
    el.scrollIntoView({ block: 'start' });
  }
  window.addEventListener('hashchange', abrirAlvo);
  document.addEventListener('DOMContentLoaded', abrirAlvo);
})();
