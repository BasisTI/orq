// Avisos do navegador (Notification API): uma notificação por pendência nova e por resposta nova do
// coordenador a uma mensagem da pessoa. Sem serviço externo, sem service worker: a página consulta
// /avisos (JSON do próprio servidor, só chave, título, projeto e link) e notifica o que é novo.
//
// Permissão: só pelo clique em #avisos-btn, nunca ao carregar. Sem suporte ou com a permissão
// negada, o botão some ou fica apagado e nada mais acontece (nem consulta).
//
// Uma notificação só, também com várias abas. Três camadas, da mais forte à mais fraca:
//  1) Marca de base por aba (sessionStorage): o que já existia quando a aba começou a consultar
//     (e o que ela já viu) nunca notifica nessa aba. Uma aba nova não "recebe" o passado.
//  2) Registro compartilhado dos ids já notificados, com reivindicação atômica entre abas: um
//     add() numa transação readwrite do IndexedDB ("orq-avisos"/"notificados", chave = id). Duas
//     transações no mesmo store se serializam; a segunda recebe ConstraintError e cala. Foi a
//     escolha porque localStorage + Web Lock NÃO bastou: o Chromium propaga escrita de localStorage
//     entre processos de forma assíncrona, e uma segunda aba leu o registro velho dentro do lock
//     (visto no teste de navegador, duplicando a notificação). Sem IndexedDB, cai para
//     localStorage "orq.avisos.notificados" dentro de um Web Lock, com essa janela de corrida
//     aceita e a camada 3 cobrindo.
//  3) tag = id do item na notificação: o navegador troca a notificação de mesma tag em vez de
//     empilhar outra.
// Funções puras (selecionar, textoDe, hrefSeguro) ficam em module.exports para o teste em Node.
(function (raiz) {
  'use strict';
  var REGISTRO = 'orq.avisos.notificados';
  var BASE = 'orq.avisos.base';
  var LIMITE_REGISTRO = 500;
  var CICLO_MS = 30000;

  // selecionar devolve, na ordem recebida, os itens que nem a base da aba nem o registro conhecem.
  function selecionar(itens, base, registro) {
    var visto = {};
    (base || []).forEach(function (id) { visto[id] = true; });
    (registro || []).forEach(function (id) { visto[id] = true; });
    var vistos = {};
    return (itens || []).filter(function (it) {
      if (!it || typeof it.id !== 'string' || visto[it.id] || vistos[it.id]) return false;
      vistos[it.id] = true;
      return true;
    });
  }

  // hrefSeguro só aceita caminho da própria Central ("/..." e não "//host" nem "/\host").
  function hrefSeguro(h) {
    return typeof h === 'string' && /^\/(?![\/\\])/.test(h) ? h : '/';
  }

  // textoDe monta a notificação. Só chave, título e projeto do item entram: nada que a pessoa
  // escreveu, nem o corpo da resposta do coordenador.
  function textoDe(it) {
    var titulo = it.tipo === 'resposta' ? 'Resposta do coordenador' : 'Pendência ' + it.chave;
    var corpo = [];
    if (it.tipo === 'resposta' && it.chave) corpo.push(it.chave);
    if (it.projeto) corpo.push(it.projeto);
    if (it.titulo) corpo.push(it.titulo);
    return { titulo: titulo, corpo: corpo.join(' · '), tag: 'orq-' + it.id, href: hrefSeguro(it.href) };
  }

  function ler(arm, chave) {
    try {
      var v = JSON.parse(arm.getItem(chave) || 'null');
      return Array.isArray(v) ? v : null;
    } catch (e) { return null; }
  }
  function gravar(arm, chave, v) {
    try { arm.setItem(chave, JSON.stringify(v)); } catch (e) { /* armazenamento indisponível */ }
  }

  var VALIDADE_MS = 7 * 24 * 3600 * 1000;
  // abrirRegistro abre o IndexedDB uma vez por página; null se não há ou falhou.
  function abrirRegistro(win) {
    if (win.__orqAvisosRegistro) return win.__orqAvisosRegistro;
    win.__orqAvisosRegistro = new Promise(function (ok) {
      try {
        var rq = win.indexedDB.open('orq-avisos', 1);
        rq.onupgradeneeded = function () { rq.result.createObjectStore('notificados'); };
        rq.onsuccess = function () {
          var db = rq.result;
          try { // o que passa de 7 dias não volta: sai do registro
            var c = db.transaction('notificados', 'readwrite').objectStore('notificados').openCursor();
            c.onsuccess = function () {
              var cur = c.result;
              if (!cur) return;
              if (typeof cur.value === 'number' && cur.value < Date.now() - VALIDADE_MS) cur.delete();
              cur.continue();
            };
          } catch (e) { /* limpeza é só higiene */ }
          ok(db);
        };
        rq.onerror = rq.onblocked = function () { ok(null); };
      } catch (e) { ok(null); }
    });
    return win.__orqAvisosRegistro;
  }
  // reivindicarIDB tenta registrar cada id; só o add() que vence a corrida devolve o item.
  function reivindicarIDB(db, itens) {
    return Promise.all(itens.map(function (it) {
      return new Promise(function (ok) {
        try {
          var tx = db.transaction('notificados', 'readwrite');
          var rq = tx.objectStore('notificados').add(Date.now(), it.id);
          rq.onsuccess = function () { ok(it); };
          rq.onerror = function (e) { if (e && e.preventDefault) e.preventDefault(); ok(null); };
          tx.onabort = function () { ok(null); };
        } catch (e) { ok(null); }
      });
    })).then(function (r) { return r.filter(Boolean); });
  }

  function iniciar(doc, win) {
    var btn = doc.getElementById('avisos-btn');
    var N = win.Notification;
    if (!btn || !N || !win.fetch) return;
    var rotulo = btn.querySelector('span') || btn;
    var timer = null;

    function estado() {
      var p = N.permission;
      if (p === 'granted') { btn.hidden = false; btn.disabled = true; rotulo.textContent = 'Avisos ativos'; btn.title = 'Você recebe avisos de pendência nova e de resposta do coordenador enquanto a Central estiver aberta.'; }
      else if (p === 'denied') { btn.hidden = false; btn.disabled = true; rotulo.textContent = 'Avisos bloqueados'; btn.title = 'O navegador bloqueou os avisos desta página. Libere nas configurações do site para ativar.'; }
      else { btn.hidden = false; btn.disabled = false; rotulo.textContent = 'Ativar avisos'; btn.title = 'Avisar no navegador de pendência nova e de resposta do coordenador.'; }
    }

    function buscar() {
      return win.fetch('/avisos', { cache: 'no-store', credentials: 'same-origin', headers: { Accept: 'application/json' } })
        .then(function (r) { return r.ok ? r.json() : null; })
        .then(function (j) { return j && Array.isArray(j.itens) ? j.itens : null; })
        .catch(function () { return null; });
    }

    function exibir(it) {
      var t = textoDe(it);
      try {
        var n = new N(t.titulo, { body: t.corpo, tag: t.tag, icon: '/estatico/orq-icon-64.png', data: { href: t.href } });
        n.onclick = function () {
          try { win.focus(); n.close(); } catch (e) { /* já fechada */ }
          // Link para um item da própria página que o /vivo ainda não trouxe (a lista de mensagens
          // atualiza a cada 30 s): só mudar o hash não acharia o alvo, então recarrega.
          var h = t.href.split('#'), local = win.location, mesma = h[0] === (local.pathname || '') + (local.search || '');
          local.href = t.href;
          if (mesma && h[1] && !doc.getElementById(h[1]) && local.reload) local.reload();
        };
      } catch (e) { /* contexto que só notifica por service worker: sem aviso, sem erro */ }
    }

    // reivindicar devolve (numa promessa) os itens que esta aba deve notificar: os que ela
    // conseguiu registrar primeiro. O registro de reserva (localStorage) só vale sem IndexedDB.
    function reivindicarReserva(novos) {
      var reg = ler(win.localStorage, REGISTRO) || [];
      var meus = selecionar(novos, [], reg);
      if (meus.length) {
        meus.forEach(function (it) { reg.push(it.id); });
        gravar(win.localStorage, REGISTRO, reg.slice(-LIMITE_REGISTRO));
      }
      return meus;
    }
    function reivindicar(novos) {
      return abrirRegistro(win).then(function (db) {
        if (db) return reivindicarIDB(db, novos);
        if (win.navigator && win.navigator.locks && win.navigator.locks.request) {
          return win.navigator.locks.request('orq-avisos', function () { return reivindicarReserva(novos); });
        }
        return reivindicarReserva(novos);
      });
    }

    function ciclo() {
      if (N.permission !== 'granted') { estado(); return Promise.resolve(); }
      return buscar().then(function (itens) {
        if (!itens) return;
        var base = ler(win.sessionStorage, BASE);
        if (base === null) {
          // Primeira consulta desta aba: o que existe agora é o passado, não notifica.
          gravar(win.sessionStorage, BASE, itens.map(function (i) { return i.id; }));
          return;
        }
        var novos = selecionar(itens, base, []);
        if (!novos.length) return;
        gravar(win.sessionStorage, BASE, base.concat(novos.map(function (i) { return i.id; })).slice(-LIMITE_REGISTRO));
        return reivindicar(novos).then(function (meus) { meus.forEach(exibir); });
      });
    }

    function agendar() {
      if (timer === null) timer = win.setInterval(ciclo, CICLO_MS);
    }

    btn.addEventListener('click', function () {
      if (N.permission !== 'default') { estado(); return; }
      var feito = false;
      var fim = function () {
        if (feito) return;
        feito = true;
        estado();
        if (N.permission === 'granted') {
          // O que já existe no clique é o passado: a base nasce agora.
          try { win.sessionStorage.removeItem(BASE); } catch (e) { /* sem armazenamento */ }
          ciclo().then(agendar);
        }
      };
      try {
        var p = N.requestPermission(fim);
        if (p && p.then) p.then(fim, fim);
      } catch (e) { fim(); }
    });

    estado();
    if (N.permission === 'granted') { ciclo().then(agendar); }
  }

  var api = { selecionar: selecionar, textoDe: textoDe, hrefSeguro: hrefSeguro, iniciar: iniciar };
  if (typeof module !== 'undefined' && module.exports) module.exports = api;
  if (raiz && raiz.document) {
    raiz.orqAvisos = api;
    if (raiz.document.readyState === 'loading') raiz.document.addEventListener('DOMContentLoaded', function () { iniciar(raiz.document, raiz); });
    else iniciar(raiz.document, raiz);
  }
})(typeof window !== 'undefined' ? window : null);
