// JS próprio da web: nada mais que isto, sem dependência. O resto é do htmx e do servidor.
// 1) Abre os <details> ancestrais de location.hash (na carga e em hashchange) e rola até o alvo.
// 2) Faixa de abas: rola a aba ativa para a vista e marca, em data-esq e data-dir, de que lado
//    ainda há aba escondida (o CSS esmaece essa borda).
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
  function abas() {
    var faixa = document.querySelector('.abas-in');
    if (!faixa) return;
    function marcar() {
      faixa.toggleAttribute('data-esq', faixa.scrollLeft > 1);
      faixa.toggleAttribute('data-dir', faixa.scrollLeft < faixa.scrollWidth - faixa.clientWidth - 1);
    }
    var atual = faixa.querySelector('a[aria-current="page"]');
    if (atual) {
      var f = faixa.getBoundingClientRect(), a = atual.getBoundingClientRect();
      faixa.scrollLeft += a.left - f.left - (f.width - a.width) / 2;
    }
    faixa.addEventListener('scroll', marcar, { passive: true });
    window.addEventListener('resize', marcar);
    marcar();
  }
  window.addEventListener('hashchange', abrirAlvo);
  document.addEventListener('DOMContentLoaded', function () {
    abas();
    abrirAlvo();
  });
})();
