// JS próprio da web: nada mais que isto, sem dependência. O resto é do htmx e do servidor.
// 1) Abre os <details> ancestrais de location.hash (na carga e em hashchange) e rola até o alvo.
// 2) Faixa de abas: rola a aba ativa para a vista e marca, em data-esq e data-dir, de que lado
//    ainda há aba escondida (o CSS esmaece essa borda).
// 3) Envios de resposta: o hx-sync "queue all" do formulário guarda os pedidos presos ao elemento
//    original, e o htmx descarta o pedido que sai da fila com o elemento já fora da página. Por
//    isso, enquanto há envio em voo ou na fila, a página não troca o fragmento (filtro) nem
//    restaura o histórico (Voltar): os dois esperam o último envio terminar, e a página diz isso
//    (#espera, aria-busy). Cada formulário envia uma vez só e leva o que estava escolhido no
//    clique. Sair da página de vez (beforeunload) pede confirmação.
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
  // Formulários de resposta com envio ainda não terminado (em voo ou na fila do htmx). O controle
  // é por formulário, e não por evento: um envio que o htmx abandona (formulário já fora da página
  // quando a vez dele chega) nunca dispara htmx:afterRequest, e por isso quem saiu do <body> não
  // conta. Quando esvazia, roda o que esperou.
  function envios() {
    var abertos = new Set(), esperas = [], fixos = new WeakMap();
    var AVISO = 'Aguardando o envio das respostas…';
    function pendentes() {
      abertos.forEach(function (f) { if (!f.isConnected) abertos.delete(f); });
      return abertos.size;
    }
    function indicar(on) {
      var av = document.getElementById('espera'), pr = document.getElementById('principal');
      if (av) av.textContent = on ? AVISO : '';
      if (pr) { if (on) pr.setAttribute('aria-busy', 'true'); else pr.removeAttribute('aria-busy'); }
    }
    function liberar() {
      if (pendentes()) return;
      var fila = esperas;
      esperas = [];
      indicar(false);
      fila.forEach(function (f) { f(); });
    }
    function espera(chave, f) {
      esperas = esperas.filter(function (e) { return e.chave !== chave; });
      esperas.push(Object.assign(f, { chave: chave }));
      indicar(true);
    }
    function deResposta(el) { return el && el.closest && el.closest('form.resposta[hx-post]'); }
    // O que a pessoa escolheu ao clicar em Enviar é o que vai ao servidor, mesmo que o pedido saia
    // da fila depois: guardamos a opção e o texto no envio e travamos os campos até a resposta.
    function fixar(f) {
      var o = f.querySelector('input[name="opcao"]:checked'), t = f.querySelector('textarea[name="texto"]');
      fixos.set(f, { opcao: o ? o.value : null, texto: t ? t.value : '' });
      if (t) t.readOnly = true;
      f.querySelectorAll('input[name="opcao"]').forEach(function (r) { r.disabled = true; });
      var b = f.querySelector('button[type="submit"]');
      if (b) b.disabled = true;
      f.setAttribute('aria-busy', 'true');
    }
    function soltar(f) {
      fixos.delete(f);
      var t = f.querySelector('textarea[name="texto"]');
      if (t) t.readOnly = false;
      f.querySelectorAll('input[name="opcao"]').forEach(function (r) { r.disabled = false; });
      var b = f.querySelector('button[type="submit"]');
      if (b) b.disabled = false;
      f.removeAttribute('aria-busy');
    }
    function encerrar(f) {
      if (f && abertos.delete(f)) { soltar(f); liberar(); }
    }
    // Em captura, antes do htmx: o segundo envio do mesmo formulário (toque repetido) é ignorado.
    document.addEventListener('submit', function (e) {
      var f = deResposta(e.target);
      if (!f) return;
      if (abertos.has(f)) { e.preventDefault(); e.stopImmediatePropagation(); return; }
      abertos.add(f);
      fixar(f);
    }, true);
    document.addEventListener('htmx:configRequest', function (e) {
      var f = e.detail.elt, s = fixos.get(f);
      if (!s) return;
      if (s.opcao === null) delete e.detail.parameters.opcao; else e.detail.parameters.opcao = s.opcao;
      e.detail.parameters.texto = s.texto;
    });
    document.addEventListener('htmx:confirm', function (e) {
      var d = e.detail;
      if (pendentes() && d.elt.closest && d.elt.closest('.filtros')) {
        e.preventDefault();
        espera('filtro', function () { d.issueRequest(true); });
      }
    });
    document.addEventListener('htmx:afterRequest', function (e) {
      encerrar(deResposta(e.detail.elt));
      liberar();
    });
    document.addEventListener('htmx:validation:halted', function (e) { encerrar(deResposta(e.detail.elt)); });
    // O htmx só envia o pedido do histórico se o evento não for cancelado: guardamos o pedido e o
    // enviamos quando o último envio terminar.
    document.addEventListener('htmx:historyCacheMiss', function (e) {
      if (!pendentes()) return;
      e.preventDefault();
      espera('historico', function () { e.detail.xhr.send(); });
    });
    window.addEventListener('beforeunload', function (e) {
      if (!pendentes()) return;
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
