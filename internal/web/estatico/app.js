// JS próprio da web: nada mais que isto, sem dependência. O resto é do htmx e do servidor.
// 1) Abre os <details> ancestrais de location.hash (na carga e em hashchange) e rola até o alvo.
// 2) Faixa de abas: rola a aba ativa para a vista e marca, em data-esq e data-dir, de que lado
//    ainda há aba escondida (o CSS esmaece essa borda).
// 3) Envios de resposta: o hx-sync "queue all" do formulário guarda os pedidos presos ao elemento
//    original, e o htmx descarta o pedido que sai da fila com o elemento já fora da página. Por
//    isso, enquanto há envio em voo ou na fila, a página não troca o fragmento (filtro) nem
//    restaura o histórico (Voltar): os dois esperam o último envio terminar. Sair da página de vez
//    (beforeunload) pede confirmação.
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
  // Envios de resposta ainda não terminados, pelo evento que os disparou (o mesmo evento volta no
  // htmx:confirm de quem sai da fila). Quando esvazia, roda o que esperou.
  function envios() {
    var abertos = new Set(), esperas = [];
    function liberar() {
      if (abertos.size) return;
      var fila = esperas;
      esperas = [];
      fila.forEach(function (f) { f(); });
    }
    function deResposta(el) { return el && el.closest && el.closest('form.resposta'); }
    function espera(chave, f) {
      esperas = esperas.filter(function (e) { return e.chave !== chave; });
      esperas.push(Object.assign(f, { chave: chave }));
    }
    document.addEventListener('htmx:confirm', function (e) {
      var d = e.detail;
      if (deResposta(d.elt)) {
        if (d.triggeringEvent) abertos.add(d.triggeringEvent);
      } else if (abertos.size && d.elt.closest && d.elt.closest('.filtros')) {
        e.preventDefault();
        espera('filtro', function () { d.issueRequest(true); });
      }
    });
    document.addEventListener('htmx:afterRequest', function (e) {
      var c = e.detail.requestConfig;
      if (c && c.triggeringEvent && abertos.delete(c.triggeringEvent)) liberar();
    });
    // O htmx só envia o pedido do histórico se o evento não for cancelado: guardamos o pedido e o
    // enviamos quando o último envio terminar.
    document.addEventListener('htmx:historyCacheMiss', function (e) {
      if (!abertos.size) return;
      e.preventDefault();
      espera('historico', function () { e.detail.xhr.send(); });
    });
    window.addEventListener('beforeunload', function (e) {
      if (!abertos.size) return;
      e.preventDefault();
      e.returnValue = '';
    });
  }
  envios();
  window.addEventListener('hashchange', abrirAlvo);
  document.addEventListener('DOMContentLoaded', function () {
    abas();
    abrirAlvo();
  });
})();
