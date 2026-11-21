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
const espera = () => new Promise((r) => setTimeout(r, 15));
const armazem = () => { const m = new Map(); return { getItem: (k) => (m.has(k) ? m.get(k) : null), setItem: (k, v) => m.set(k, String(v)), removeItem: (k) => m.delete(k) }; };
const servidor = { itens: [], consultas: 0, fora: false };
const notificacoes = [];
const lockFila = { cadeia: Promise.resolve() };
const locks = { request: (_nome, fn) => (lockFila.cadeia = lockFila.cadeia.then(() => fn())) };

// IndexedDB simulado: um store compartilhado, add() recusa chave repetida com ConstraintError.
const idbCompartilhado = new Map();
function idbFalso(m) {
  const req = (f) => { const r = {}; setTimeout(() => f(r), 0); return r; };
  const loja = {
    add: (v, k) => req((r) => {
      if (m.has(k)) { const e = { preventDefault() {}, target: r }; r.error = { name: 'ConstraintError' }; r.onerror && r.onerror(e); return; }
      m.set(k, v); r.onsuccess && r.onsuccess({});
    }),
    openCursor: () => req((r) => { r.result = null; r.onsuccess && r.onsuccess({}); }),
  };
  return { open: () => { const r = {}; setTimeout(() => { r.result = { createObjectStore() {}, transaction: () => ({ objectStore: () => loja }) }; r.onupgradeneeded && r.onupgradeneeded(); r.onsuccess && r.onsuccess({}); }, 0); return r; } };
}

function aba({ permissao, local, sessao, concede = 'granted', idb = false }) {
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
    Notification: Notificacao, indexedDB: idb ? idbFalso(idbCompartilhado) : undefined, localStorage: local, sessionStorage: sessao, navigator: { locks },
    location: { href: '/', pathname: '/', search: '', recargas: 0, reload() { this.recargas++; } }, focus() { this.focado = true; },
    setInterval: (f) => { timers.push(f); return timers.length; },
    fetch: async () => { servidor.consultas++; if (servidor.fora) return { ok: false }; return { ok: true, json: async () => ({ itens: servidor.itens.map((i) => ({ ...i })) }) }; },
  };
  const doc = { getElementById: (id) => (id === 'avisos-btn' ? btn : null) };
  iniciar(doc, w);
  return { w, btn, timers, Notificacao, ciclo: async () => { timers.forEach((f) => f()); await espera(); } };
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
  a.btn.ouvintes.click();
  await espera();
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
  a.btn.ouvintes.click();
  await espera();
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

// 5) Armazenamento indisponível: a página não quebra.
{
  const quebrado = { getItem() { throw new Error('x'); }, setItem() { throw new Error('x'); }, removeItem() { throw new Error('x'); } };
  servidor.itens = [A];
  const a = aba({ permissao: 'granted', local: quebrado, sessao: quebrado });
  await a.ciclo();
}
console.log('avisos_logica: ok');
