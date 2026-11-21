// Teste em Node da lógica de estático/avisos.js (go test, TestAvisosLogicaDoNavegador): seleção do
// que é novo, deduplicação entre abas, permissão só pelo gesto, texto e destino do clique. O
// navegador é simulado (Notification, fetch, storages, Web Locks e timers); o que só roda num
// navegador de verdade fica para o teste com a tag "browser" (navegador_avisos).
import { createRequire } from 'node:module';
import assert from 'node:assert/strict';
const { selecionar, textoDe, hrefSeguro, iniciar } = createRequire(import.meta.url)('../estatico/avisos.js');

const A = { id: 'p:x/1#P1@t1', tipo: 'pendencia', chave: 'x/1#P1', titulo: 'Qual?', projeto: 'x', href: '/p/x/1/1' };
const B = { id: 'p:x/1#P2@t2', tipo: 'pendencia', chave: 'x/1#P2', titulo: 'E agora?', projeto: 'x', href: '/p/x/1/2' };
const R = { id: 'm:7', tipo: 'resposta', chave: 'onda #3 mensagem #7', titulo: 'Onda de teste', href: '/#msg-7' };

// --- funções puras
assert.deepEqual(selecionar([A, B, R], [A.id], [R.id]), [B]);
assert.deepEqual(selecionar([A, A, B], [], []), [A, B], 'o mesmo id duas vezes na resposta notifica uma vez');
assert.deepEqual(selecionar(null, null, null), []);
assert.deepEqual(selecionar([{ titulo: 'sem id' }, null], [], []), []);
assert.equal(hrefSeguro('/p/x/1/1'), '/p/x/1/1');
assert.equal(hrefSeguro('/#msg-7'), '/#msg-7');
for (const h of ['//evil.example', '/\\evil.example', 'https://evil.example', 'javascript:alert(1)', '', null, undefined, 3]) assert.equal(hrefSeguro(h), '/', String(h));
const tp = textoDe({ ...A, texto: 'não deve aparecer', resposta: 'nem isto' });
assert.deepEqual(tp, { titulo: 'Pendência x/1#P1', corpo: 'x · Qual?', tag: 'orq-' + A.id, href: '/p/x/1/1' });
const tr = textoDe(R);
assert.deepEqual(tr, { titulo: 'Resposta do coordenador', corpo: 'onda #3 mensagem #7 · Onda de teste', tag: 'orq-m:7', href: '/#msg-7' });
assert.equal(textoDe({ ...A, href: '//evil.example' }).href, '/');

// --- navegador simulado
// Sem espera por tempo fixo: o que o teste espera são as promessas reais (ciclos, cliques, o
// IndexedDB falso), para não depender da carga da máquina (go test -race roda muitos pacotes juntos).
const abas = [];
const tudo = async () => { for (const t of abas) await t.ctl.aguardar(); };
const espera = tudo;
const armazem = () => { const m = new Map(); return { getItem: (k) => (m.has(k) ? m.get(k) : null), setItem: (k, v) => m.set(k, String(v)), removeItem: (k) => m.delete(k) }; };
const servidor = { itens: [], consultas: 0, fora: false };
const notificacoes = [];
const lockFila = { cadeia: Promise.resolve() };
const locks = { request: (_nome, fn) => (lockFila.cadeia = lockFila.cadeia.then(() => fn())) };

// IndexedDB simulado: um store compartilhado; o add() recusa chave repetida com ConstraintError e a
// transação só completa (oncomplete) depois do pedido. modo.erro faz todo add() falhar com esse nome.
const idbCompartilhado = new Map();
function idbFalso(m, modo = {}) {
  const loja = (tx) => ({
    add: (v, k) => {
      const r = {};
      setTimeout(() => {
        if (modo.erro) { r.error = { name: modo.erro }; r.onerror && r.onerror({ preventDefault() {} }); tx.onabort && tx.onabort(); return; }
        if (m.has(k)) { r.error = { name: 'ConstraintError' }; r.onerror && r.onerror({ preventDefault() {} }); setTimeout(() => tx.oncomplete && tx.oncomplete(), 0); return; }
        m.set(k, v); r.onsuccess && r.onsuccess({}); setTimeout(() => tx.oncomplete && tx.oncomplete(), 0);
      }, 0);
      return r;
    },
    openCursor: () => { const r = {}; setTimeout(() => { r.result = null; r.onsuccess && r.onsuccess({}); }, 0); return r; },
  });
  return { open: () => { const r = {}; setTimeout(() => { r.result = { createObjectStore() {}, transaction: () => { const tx = {}; tx.objectStore = () => loja(tx); return tx; } }; r.onupgradeneeded && r.onupgradeneeded(); r.onsuccess && r.onsuccess({}); }, 0); return r; } };
}

// Permissions API simulada: a aba que conhece a mudança de permissão recebe o onchange.
const ouvintesPerm = [];
const permissoes = { query: async () => { const ps = {}; ouvintesPerm.push(ps); return ps; } };

function aba({ permissao, local, sessao, concede = 'granted', idb = false, idbErro, dom = {}, hash = '', vivo = null }) {
  const timers = [];
  const btn = { hidden: true, disabled: false, title: '', ouvintes: {}, _span: { textContent: '' }, querySelector() { return this._span; }, addEventListener(e, f) { this.ouvintes[e] = f; } };
  class Notificacao {
    static permission = permissao;
    static pedidos = 0;
    static requestPermission() { Notificacao.pedidos++; Notificacao.permission = concede; return Promise.resolve(concede); }
    constructor(titulo, op) { this.titulo = titulo; this.op = op; this.fechada = false; notificacoes.push(this); this.aba = w; }
    close() { this.fechada = true; }
  }
  const w = {
    Notification: Notificacao, indexedDB: idb ? idbFalso(idbCompartilhado, { erro: idbErro }) : undefined, localStorage: local, sessionStorage: sessao, navigator: { locks, permissions: permissoes },
    eventos: [], Event: class { constructor(t) { this.type = t; } }, dispatchEvent(e) { this.eventos.push(e.type); },
    location: { href: '/', hash, pathname: '/', search: '', recargas: 0, reload() { this.recargas++; } }, focus() { this.focado = true; },
    setInterval: (f) => { timers.push(f); return timers.length; },
    clearInterval: (id) => { timers[id - 1] = () => Promise.resolve(); w.limpos = (w.limpos || 0) + 1; },
    setTimeout: (f, ms) => { const t = setTimeout(f, ms); t.unref(); return t; },
    htmx: vivo ? { trigger: (el, ev) => { w.disparos.push(ev); vivo.aoDisparar(); } } : undefined, disparos: [],
    fetch: async () => { servidor.consultas++; if (servidor.fora) return { ok: false }; return { ok: true, json: async () => ({ itens: servidor.itens.map((i) => ({ ...i })) }) }; },
  };
  const doc = { getElementById: (id) => (id === 'avisos-btn' ? btn : id === 'vivo' ? vivo : dom[id] || null) };
  const ctl = iniciar(doc, w);
  const t = { w, btn, timers, Notificacao, ctl, ciclo: async () => { await tudo(); await Promise.all(timers.map((f) => f())); } };
  abas.push(t);
  return t;
}

// 1) Permissão por gesto: carregar a página não pede nada nem consulta o servidor.
{
  const a = aba({ permissao: 'default', local: armazem(), sessao: armazem() });
  await espera();
  assert.equal(a.Notificacao.pedidos, 0, 'não pede permissão ao carregar');
  assert.equal(servidor.consultas, 0, 'sem permissão, nenhuma consulta');
  assert.equal(a.btn.hidden, false);
  assert.equal(a.btn.disabled, false);
  assert.equal(a.btn._span.textContent, 'Ativar avisos');
  servidor.itens = [A];
  await a.btn.ouvintes.click();
  assert.equal(a.Notificacao.pedidos, 1, 'o clique pede a permissão uma vez');
  assert.equal(servidor.consultas, 1, 'concedida, a base nasce de uma consulta');
  assert.equal(notificacoes.length, 0, 'o que já existia no clique não notifica');
  assert.equal(a.btn.disabled, true);
  assert.equal(a.btn._span.textContent, 'Avisos ativos');
  assert.equal(a.timers.length, 1);
  // Nova pendência: uma notificação, com texto sem conteúdo livre e clique que leva à pendência.
  servidor.itens = [A, B];
  await a.ciclo();
  assert.equal(notificacoes.length, 1);
  const n = notificacoes[0];
  assert.equal(n.titulo, 'Pendência x/1#P2');
  assert.equal(n.op.body, 'x · E agora?');
  assert.equal(n.op.tag, 'orq-' + B.id);
  n.onclick();
  assert.equal(a.w.location.href, '/p/x/1/2');
  assert.equal(a.w.location.recargas, 0, 'outra página: navega, não recarrega');
  assert.equal(n.fechada, true);
  await a.ciclo();
  assert.equal(notificacoes.length, 1, 'o mesmo item não notifica de novo');
  notificacoes.length = 0;
  servidor.consultas = 0;
}

// 2) Negada: Central como hoje, sem insistir (nem consulta, nem pedido).
{
  const a = aba({ permissao: 'denied', local: armazem(), sessao: armazem() });
  await a.ciclo();
  assert.equal(a.Notificacao.pedidos, 0);
  assert.equal(servidor.consultas, 0);
  assert.equal(a.btn.disabled, true);
  assert.equal(a.btn._span.textContent, 'Avisos bloqueados');
  await a.btn.ouvintes.click();
  assert.equal(a.Notificacao.pedidos, 0, 'negada, o clique não pede de novo');
}

// 3) Indisponível: sem Notification o script não faz nada.
{
  const btn = { hidden: true, addEventListener() { throw new Error('não devia ouvir'); } };
  iniciar({ getElementById: () => btn }, { fetch() { throw new Error('não devia consultar'); } });
  assert.equal(btn.hidden, true);
}

// 4) Várias abas, uma notificação por item.
{
  servidor.itens = [A];
  servidor.consultas = 0;
  const local = armazem();
  const a1 = aba({ permissao: 'granted', local, sessao: armazem() });
  const a2 = aba({ permissao: 'granted', local, sessao: armazem() });
  await espera();
  assert.equal(notificacoes.length, 0, 'ao abrir, o que existia não notifica em aba nenhuma');
  servidor.itens = [A, B, R];
  await Promise.all([a1.ciclo(), a2.ciclo()]);
  assert.equal(notificacoes.length, 2, 'B e R: uma notificação cada, não uma por aba');
  assert.deepEqual(notificacoes.map((n) => n.op.tag).sort(), ['orq-m:7', 'orq-' + B.id].sort());
  assert.equal(notificacoes.find((n) => n.op.tag === 'orq-m:7').titulo, 'Resposta do coordenador');
  // Resposta do coordenador: o alvo (#msg-7) ainda não está na página aberta em "/", então o clique
  // muda o hash e recarrega para o encontrar.
  const nr = notificacoes.find((n) => n.op.tag === 'orq-m:7');
  nr.onclick();
  assert.equal(nr.aba.location.href, '/#msg-7');
  assert.equal(nr.aba.location.recargas, 1);
  // Terceira aba abre depois: o que existe é passado.
  const a3 = aba({ permissao: 'granted', local, sessao: armazem() });
  await a3.ciclo();
  assert.equal(notificacoes.length, 2);
  // Item novo com as três abas abertas: uma só.
  const C = { id: 'p:y/2#P1@t3', tipo: 'pendencia', chave: 'y/2#P1', titulo: 'Nova', projeto: 'y', href: '/p/y/2/1' };
  servidor.itens = [A, B, R, C];
  await Promise.all([a1.ciclo(), a2.ciclo(), a3.ciclo()]);
  assert.equal(notificacoes.length, 3);
  // Recarregar a aba (a meta refresh da Central) mantém a base: nada se repete.
  const sessaoRecarregada = armazem();
  const a4 = aba({ permissao: 'granted', local, sessao: sessaoRecarregada });
  await a4.ciclo();
  const a4b = aba({ permissao: 'granted', local, sessao: sessaoRecarregada });
  await a4b.ciclo();
  assert.equal(notificacoes.length, 3);
  // Falha de consulta não notifica nem quebra.
  servidor.fora = true;
  await a1.ciclo();
  servidor.fora = false;
  assert.equal(notificacoes.length, 3);
  // Item que some e volta (mesmo id) não notifica de novo.
  servidor.itens = [A];
  await a1.ciclo();
  servidor.itens = [A, B, R, C];
  await a1.ciclo();
  assert.equal(notificacoes.length, 3);
}

// 4b) O mesmo cenário pelo caminho do IndexedDB (a reivindicação atômica de produção).
{
  notificacoes.length = 0;
  idbCompartilhado.clear();
  servidor.itens = [A];
  const local = armazem();
  const a1 = aba({ permissao: 'granted', local, sessao: armazem(), idb: true });
  const a2 = aba({ permissao: 'granted', local, sessao: armazem(), idb: true });
  await espera();
  servidor.itens = [A, B, R];
  await Promise.all([a1.ciclo(), a2.ciclo()]);
  assert.equal(notificacoes.length, 2, 'IDB: B e R, uma vez cada');
  assert.equal(idbCompartilhado.size, 2);
  await Promise.all([a1.ciclo(), a2.ciclo()]);
  assert.equal(notificacoes.length, 2, 'IDB: nada se repete');
  assert.equal(local.getItem('orq.avisos.notificados'), null, 'IDB: o registro de reserva não é usado');
}


// 6) Clique na resposta do coordenador (achado 1 da r1).
{
  notificacoes.length = 0;
  const msg = (comResposta) => ({ querySelector: (q) => (q === '.msg-resposta' && comResposta ? {} : null) });
  const casos = [
    ['sem o item no DOM', {}, '', 1, 0],
    ['item sem .msg-resposta', { 'msg-7': msg(false) }, '', 1, 0],
    ['item sem .msg-resposta e hash já igual', { 'msg-7': msg(false) }, '#msg-7', 1, 0],
    ['item com a resposta, hash diferente', { 'msg-7': msg(true) }, '', 0, 0],
    ['item com a resposta e hash já igual: hashchange à mão', { 'msg-7': msg(true) }, '#msg-7', 0, 1],
  ];
  for (const [nome, dom, hash, recargas, hashchanges] of casos) {
    notificacoes.length = 0;
    servidor.itens = [A];
    const a = aba({ permissao: 'granted', local: armazem(), sessao: armazem(), dom, hash });
    await espera();
    servidor.itens = [A, R];
    await a.ciclo();
    assert.equal(notificacoes.length, 1, nome);
    notificacoes[0].onclick();
    assert.equal(a.w.location.href, '/#msg-7', nome);
    assert.equal(a.w.location.recargas, recargas, `${nome}: recargas`);
    assert.equal(a.w.eventos.filter((e) => e === 'hashchange').length, hashchanges, `${nome}: hashchange`);
  }
}

// 7) Permissão concedida fora do clique da aba (achado 2 da r1): "Avisos ativos" implica consulta.
{
  notificacoes.length = 0;
  servidor.itens = [A];
  servidor.consultas = 0;
  const a2 = aba({ permissao: 'default', local: armazem(), sessao: armazem() });
  await espera();
  assert.equal(a2.timers.length, 0);
  // Outra aba concede: o Notification desta aba passa a granted sem clique aqui. O clique ativa.
  a2.Notificacao.permission = 'granted';
  await a2.btn.ouvintes.click();
  assert.equal(a2.btn._span.textContent, 'Avisos ativos');
  assert.equal(a2.timers.length, 1, 'o clique com granted agenda o ciclo');
  assert.ok(servidor.consultas >= 1, 'e consulta');
  servidor.itens = [A, B];
  await a2.ciclo();
  assert.equal(notificacoes.length, 1, 'e notifica o que chega depois');
  await a2.btn.ouvintes.click();
  assert.equal(a2.timers.length, 1, 'clique repetido não agenda duas vezes');
  // Sem clique: a Permissions API avisa a aba aberta.
  const a3 = aba({ permissao: 'default', local: armazem(), sessao: armazem() });
  await espera();
  a3.Notificacao.permission = 'granted';
  ouvintesPerm[ouvintesPerm.length - 1].onchange();
  await espera();
  assert.equal(a3.btn._span.textContent, 'Avisos ativos');
  assert.equal(a3.timers.length, 1, 'onchange da permissão agenda o ciclo');
  // Duas abas, a segunda carregada antes da concessão, concedida na primeira: uma notificação só.
  notificacoes.length = 0;
  idbCompartilhado.clear();
  servidor.itens = [A];
  const local = armazem();
  const b1 = aba({ permissao: 'default', local, sessao: armazem(), idb: true });
  const b2 = aba({ permissao: 'default', local, sessao: armazem(), idb: true });
  await espera();
  b1.btn.ouvintes.click();
  b2.Notificacao.permission = 'granted';
  await b2.btn.ouvintes.click();
  servidor.itens = [A, B];
  await Promise.all([b1.ciclo(), b2.ciclo()]);
  assert.equal(notificacoes.length, 1, 'duas abas: uma notificação');
}

// 8) Sem sessionStorage, a base fica na memória da página (achado 4 da r1).
{
  notificacoes.length = 0;
  const quebrado = { getItem() { throw new Error('x'); }, setItem() { throw new Error('x'); }, removeItem() { throw new Error('x'); } };
  servidor.itens = [A];
  const a = aba({ permissao: 'granted', local: quebrado, sessao: quebrado });
  await a.ciclo();
  assert.equal(notificacoes.length, 0, 'o que existia não notifica');
  servidor.itens = [A, B];
  await a.ciclo();
  assert.equal(notificacoes.length, 1, 'o item novo notifica mesmo sem sessionStorage');
  await a.ciclo();
  assert.equal(notificacoes.length, 1);
}

// 9) Erro do IndexedDB que não é ConstraintError cai para o registro de reserva (achado 5).
{
  notificacoes.length = 0;
  servidor.itens = [A];
  const local = armazem();
  const a = aba({ permissao: 'granted', local, sessao: armazem(), idb: true, idbErro: 'QuotaExceededError' });
  await espera();
  servidor.itens = [A, B];
  await a.ciclo();
  assert.equal(notificacoes.length, 1, 'erro de cota: notifica pela reserva, não silencia');
  assert.ok(String(local.getItem('orq.avisos.notificados')).includes(B.id));
  await a.ciclo();
  assert.equal(notificacoes.length, 1);
}

// 10) A base não expulsa item antigo ainda aberto (achado 6): 600 itens novos e o antigo não notifica.
{
  notificacoes.length = 0;
  idbCompartilhado.clear();
  const antigo = { id: 'p:old/1#P1@t0', tipo: 'pendencia', chave: 'old/1#P1', titulo: 'Antigo', projeto: 'old', href: '/p/old/1/1' };
  servidor.itens = [antigo];
  const a = aba({ permissao: 'granted', local: armazem(), sessao: armazem(), idb: true });
  await espera();
  const muitos = Array.from({ length: 600 }, (_, i) => ({ id: `p:n/${i}#P1@t`, tipo: 'pendencia', chave: `n/${i}#P1`, titulo: 't', projeto: 'n', href: `/p/n/${i}/1` }));
  servidor.itens = [antigo, ...muitos];
  await a.ciclo();
  assert.equal(notificacoes.length, 600);
  assert.ok(!notificacoes.some((n) => n.op.tag === 'orq-' + antigo.id));
  await a.ciclo();
  await a.ciclo();
  assert.equal(notificacoes.length, 600, 'o antigo não notifica depois');
  assert.ok(!notificacoes.some((n) => n.op.tag === 'orq-' + antigo.id));
}

// 6b) Clique com #vivo e htmx (r3): pede o /vivo, não recarrega, e abre o destino depois da atualização.
function vivoFalso({ aoTrocar, falhou = false }) {
  const ouvintes = [];
  const v = {
    addEventListener: (e, f) => { if (e === 'htmx:afterRequest') ouvintes.push(f); },
    removeEventListener: (e, f) => { const i = ouvintes.indexOf(f); if (i >= 0) ouvintes.splice(i, 1); },
    ouvintes,
    aoDisparar: () => {
      v.fim = new Promise((r) => setTimeout(() => { aoTrocar && aoTrocar(); ouvintes.slice().forEach((f) => f({ target: v, detail: { failed: falhou } })); r(); }, 0));
    },
  };
  return v;
}
{
  const casos = [
    ['/vivo traz a resposta', { traz: true }, 0, 1, true],
    ['/vivo não traz a resposta: último recurso', { traz: false }, 1, 1, false],
    ['/vivo falha: só abre o destino, sem recarregar', { traz: false, falhou: true }, 0, 1, true],
  ];
  for (const [nome, { traz, falhou }, recargas, disparos, abre] of casos) {
    notificacoes.length = 0;
    servidor.itens = [A];
    const item = { querySelector: () => null };
    const dom = { 'msg-7': item };
    const vivo = vivoFalso({ falhou, aoTrocar: () => { if (traz) item.querySelector = (q) => (q === '.msg-resposta' ? {} : null); } });
    const a = aba({ permissao: 'granted', local: armazem(), sessao: armazem(), dom, vivo });
    await a.ctl.aguardar();
    servidor.itens = [A, R];
    await a.ciclo();
    assert.equal(notificacoes.length, 1, nome);
    notificacoes[0].onclick();
    assert.equal(a.w.location.href, '/', `${nome}: o hash só muda depois da atualização`);
    await vivo.fim;
    assert.deepEqual(a.w.disparos, ['vivo-agora'].slice(0, disparos), nome);
    assert.equal(a.w.location.recargas, recargas, `${nome}: recargas`);
    if (abre) assert.equal(a.w.location.href, '/#msg-7', `${nome}: abre o destino`);
    assert.equal(vivo.ouvintes.length, 0, `${nome}: o ouvinte sai`);
  }
}

// 11) Permissão revogada e concedida de novo na mesma aba (r3): o acumulado não notifica.
{
  notificacoes.length = 0;
  idbCompartilhado.clear();
  servidor.itens = [A];
  const a = aba({ permissao: 'granted', local: armazem(), sessao: armazem(), idb: true });
  await a.ctl.aguardar();
  assert.equal(a.timers.length, 1);
  a.Notificacao.permission = 'denied';
  ouvintesPerm[ouvintesPerm.length - 1].onchange();
  assert.equal(a.btn._span.textContent, 'Avisos bloqueados');
  assert.equal(a.w.limpos, 1, 'o timer é limpo ao perder a permissão');
  const consultas = servidor.consultas;
  servidor.itens = [A, B, R];
  await a.ciclo();
  assert.equal(servidor.consultas, consultas, 'revogada: nenhuma consulta');
  a.Notificacao.permission = 'granted';
  await a.btn.ouvintes.click();
  assert.equal(notificacoes.length, 0, 'reconcedida: o acumulado é passado, não notifica');
  assert.equal(a.timers.length, 2, 'novo timer');
  const C = { id: 'p:z/9#P1@t9', tipo: 'pendencia', chave: 'z/9#P1', titulo: 'depois', projeto: 'z', href: '/p/z/9/1' };
  servidor.itens = [A, B, R, C];
  await a.ciclo();
  assert.equal(notificacoes.length, 1, 'e o que chega depois notifica');
}

// 12) A base em memória vale mais que o sessionStorage (r3).
{
  notificacoes.length = 0;
  const velho = armazem();
  let falha = false;
  const sessao = { getItem: (k) => velho.getItem(k), setItem: (k, v) => { if (falha) throw new Error('cota'); velho.setItem(k, v); }, removeItem: (k) => velho.removeItem(k) };
  servidor.itens = [A];
  const a = aba({ permissao: 'granted', local: armazem(), sessao });
  await a.ctl.aguardar();
  falha = true; // a partir daqui o sessionStorage guarda só a base antiga
  servidor.itens = [A, B];
  await a.ciclo();
  assert.equal(notificacoes.length, 1);
  await a.ciclo();
  await a.ciclo();
  assert.equal(notificacoes.length, 1, 'B notifica uma vez, não a cada ciclo');
}
console.log('avisos_logica: ok');
