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
  // reivindicarIDB tenta registrar cada id e devolve {ganhou, erro}: ganhou são os itens cujo add()
  // venceu e já foi confirmado (oncomplete, não só onsuccess do pedido); erro são os que falharam
  // por motivo que não é "outra aba chegou antes" (cota, conexão fechada) e vão para o registro de
  // reserva. ConstraintError é a derrota normal e não notifica.
  function reivindicarIDB(db, itens) {
    return Promise.all(itens.map(function (it) {
      return new Promise(function (ok) {
        var fim = false;
        function acaba(r) { if (!fim) { fim = true; ok({ it: it, r: r }); } }
        try {
          var tx = db.transaction('notificados', 'readwrite');
          var rq = tx.objectStore('notificados').add(Date.now(), it.id);
          rq.onerror = function (e) {
            if (rq.error && rq.error.name === 'ConstraintError') { if (e && e.preventDefault) e.preventDefault(); acaba('perdeu'); }
            else acaba('erro');
          };
          tx.oncomplete = function () { acaba('ganhou'); };
          tx.onabort = function () { acaba('erro'); };
        } catch (e) { acaba('erro'); }
      });
    })).then(function (rs) {
      var sai = { ganhou: [], erro: [] };
      rs.forEach(function (x) { if (x.r !== 'perdeu') sai[x.r].push(x.it); });
      return sai;
    });
  }

  function iniciar(doc, win) {
    var btn = doc.getElementById('avisos-btn');
    var N = win.Notification;
    if (!btn || !N || !win.fetch) return null;
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
          // Destino na própria página (a conversa da resposta, em "/"): a lista de mensagens só
          // atualiza a cada 30 s no /vivo, então o item pode faltar ou estar sem a resposta. A página
          // promete não trocar o que está aberto ou digitado, então o clique pede um /vivo na hora
          // (evento vivo-agora do #vivo, que preserva rascunhos e pendências em edição) e abre o
          // destino depois da atualização. Recarregar a página (que descarta rascunhos) é o último
          // recurso: sem #vivo/htmx, ou item ainda sem a resposta depois do /vivo.
          var h = t.href.split('#'), local = win.location, mesma = h[0] === (local.pathname || '') + (local.search || '');
          var id = h[1];
          if (!(mesma && id)) { local.href = t.href; return; }
          var pronto = function () {
            var alvo = doc.getElementById(id);
            return !!alvo && (it.tipo !== 'resposta' || !!(alvo.querySelector && alvo.querySelector('.msg-resposta')));
          };
          var abrir = function () {
            var igual = local.hash === '#' + id;
            local.href = t.href;
            // Mesmo hash: não há hashchange, e a pessoa pode ter fechado o <details>.
            if (igual && win.dispatchEvent && win.Event) win.dispatchEvent(new win.Event('hashchange'));
          };
          if (pronto()) { abrir(); return; }
          var vivo = doc.getElementById('vivo');
          if (!(vivo && win.htmx && win.htmx.trigger && vivo.addEventListener)) { local.href = t.href; if (local.reload) local.reload(); return; }
          var resolvido = false;
          var depois = function (falhou) {
            if (resolvido) return;
            resolvido = true;
            vivo.removeEventListener('htmx:afterRequest', ao);
            if (pronto() || falhou) { abrir(); return; }
            local.href = t.href;
            if (local.reload) local.reload();
          };
          var ao = function (e) {
            if (e && e.target && e.target !== vivo) return;
            // Só 2xx é sucesso (detail.successful === true), como o app.js faz: erro de rede, aborto e
            // timeout do htmx disparam afterRequest sem failed nem successful.
            depois(!(e && e.detail && e.detail.successful === true));
          };
          vivo.addEventListener('htmx:afterRequest', ao);
          win.setTimeout(function () { depois(true); }, 8000);
          win.htmx.trigger(vivo, 'vivo-agora');
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
        if (db) {
          return reivindicarIDB(db, novos).then(function (r) {
            if (!r.erro.length) return r.ganhou;
            return Promise.resolve(reservar(r.erro)).then(function (m) { return r.ganhou.concat(m); });
          });
        }
        return reservar(novos);
      });
    }
    function reservar(novos) {
      if (win.navigator && win.navigator.locks && win.navigator.locks.request) {
          return win.navigator.locks.request('orq-avisos', function () { return reivindicarReserva(novos); });
      }
      return reivindicarReserva(novos);
    }

    // A base da aba vive em sessionStorage (sobrevive a recarga) e, se ele falhar, na memória da
    // página: sem isso a aba nunca saberia o que é novo. Ela guarda só os ids da última resposta do
    // servidor, então não cresce nem expulsa item antigo ainda aberto. Custo aceito: um item que
    // some e volta com o mesmo id, sem nunca ter sido notificado, voltaria a ser "novo"; pendência
    // resolvida e onda encerrada não voltam, então isso não ocorre.
    var baseMem = null;
    function lerBase() {
      return baseMem !== null ? baseMem : ler(win.sessionStorage, BASE);
    }
    function gravarBase(ids) {
      baseMem = ids;
      gravar(win.sessionStorage, BASE, ids);
    }
    // geracao sobe a cada desativar/ativar: um ciclo iniciado antes (consulta ainda em voo) não pode
    // gravar a base nem notificar depois, ou repovoaria a base que a desativação apagou.
    var geracao = 0;
    function ciclo() {
      if (N.permission !== 'granted') { desativar(); estado(); return Promise.resolve(); }
      var g = geracao;
      return buscar().then(function (itens) {
        if (!itens || g !== geracao) return;
        var base = lerBase();
        var ids = itens.map(function (i) { return i.id; });
        gravarBase(ids);
        // Primeira consulta desta aba: o que existe agora é o passado, não notifica.
        if (base === null) return;
        var novos = selecionar(itens, base, []);
        if (!novos.length) return;
        return reivindicar(novos).then(function (meus) { if (g === geracao) meus.forEach(exibir); });
      });
    }

    function agendar() {
      if (ativo && timer === null) timer = win.setInterval(ciclo, CICLO_MS);
    }

    // ativar começa a consultar: a base nasce agora (o que existe é passado) e o ciclo é agendado.
    // "Avisos ativos" no botão sempre implica consulta em andamento. Idempotente.
    var ativo = false;
    var ultima = Promise.resolve(); // o último ciclo iniciado por ativar/carregamento (para os testes esperarem)
    // desativar é o inverso: permissão perdida (revogada nas configurações, outra aba). Para o
    // timer e esquece a base, para que uma nova concessão passe por ativar() e trate o que chegou
    // nesse meio-tempo como passado, em vez de notificar o acumulado.
    function desativar() {
      ativo = false;
      geracao++;
      if (timer !== null) { try { win.clearInterval(timer); } catch (e) { /* sem clearInterval */ } timer = null; }
      baseMem = null;
      try { win.sessionStorage.removeItem(BASE); } catch (e) { /* sem armazenamento */ }
    }
    function ativar() {
      if (ativo || N.permission !== 'granted') return;
      ativo = true;
      geracao++;
      baseMem = null;
      try { win.sessionStorage.removeItem(BASE); } catch (e) { /* sem armazenamento */ }
      ultima = ciclo().then(agendar);
      return ultima;
    }

    btn.addEventListener('click', function () {
      if (N.permission === 'granted') { estado(); return ativar(); } // concedida fora deste clique (outra aba, configuração do site)
      if (N.permission !== 'default') { estado(); return; }
      var feito = false;
      var fim = function () {
        if (feito) return ultima;
        feito = true;
        estado();
        return ativar();
      };
      try {
        var p = N.requestPermission(fim);
        if (p && p.then) return p.then(fim, fim);
      } catch (e) { return fim(); }
    });

    estado();
    // Permissão já concedida ao carregar (por um gesto anterior, nesta ou em outra aba): só consulta,
    // sem pedir nada, mantendo a base da aba se ela já tem uma (recarga). Não fere "permissão só
    // por gesto": o gesto foi o da concessão; carregar a página nunca pede a permissão.
    if (N.permission === 'granted') { ativo = true; ultima = ciclo().then(agendar); }
    // Concedida ou revogada em outra aba/configuração depois do carregamento: acompanha, sem clique.
    try {
      if (win.navigator && win.navigator.permissions && win.navigator.permissions.query) {
        win.navigator.permissions.query({ name: 'notifications' }).then(function (ps) {
          ps.onchange = function () { estado(); if (N.permission === 'granted') ativar(); else desativar(); };
        }, function () { /* sem Permissions API */ });
      }
    } catch (e) { /* idem */ }
    return { aguardar: function () { return ultima; } };
  }

  var api = { selecionar: selecionar, textoDe: textoDe, hrefSeguro: hrefSeguro, iniciar: iniciar };
  if (typeof module !== 'undefined' && module.exports) module.exports = api;
  if (raiz && raiz.document) {
    raiz.orqAvisos = api;
    if (raiz.document.readyState === 'loading') raiz.document.addEventListener('DOMContentLoaded', function () { iniciar(raiz.document, raiz); });
    else iniciar(raiz.document, raiz);
  }
})(typeof window !== 'undefined' ? window : null);
