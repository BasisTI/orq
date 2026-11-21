// Teste de navegador de orq #110 (avisos do navegador). Uso: node avisos.mjs <base-url> <controle>;
// env: ORQ_PLAYWRIGHT, ORQ_CHROMIUM. O Notification é simulado (o headless não mostra notificação
// do sistema); tudo o mais é o de produção: app.js/avisos.js reais, /avisos real, localStorage e
// sessionStorage reais, duas abas no mesmo contexto (mesmo localStorage). O timer de 30 s do
// avisos.js é capturado e disparado à mão.
import { pathToFileURL } from 'node:url';
import { join } from 'node:path';

const { chromium } = await import(pathToFileURL(join(process.env.ORQ_PLAYWRIGHT, 'index.mjs')).href);
const [base, controle] = process.argv.slice(2);
const problemas = [];
const falha = (m) => problemas.push(m);
const confere = (ok, m) => { if (!ok) falha(m); };
const muda = async (acao) => { const r = await fetch(`${controle}/${acao}`); if (!r.ok) falha(`controle ${acao}: ${r.status}`); return r.text(); };

const simulado = () => {
  window.__n = []; window.__pedidos = 0; window.__ciclos = [];
  const si = window.setInterval.bind(window);
  window.setInterval = (f, ms, ...a) => { if (ms === 30000) window.__ciclos.push(f); return si(f, ms, ...a); };
  const perm = () => { try { return localStorage.getItem('__perm') || 'default'; } catch (e) { return 'default'; } };
  window.Notification = class {
    static get permission() { return perm(); }
    static requestPermission() { window.__pedidos++; localStorage.setItem('__perm', 'granted'); return Promise.resolve('granted'); }
    constructor(titulo, op) { this.titulo = titulo; this.op = op; window.__n.push({ titulo, corpo: op.body, tag: op.tag, data: op.data, clica: () => this.onclick() }); }
    close() {}
  };
};

const navegador = await chromium.launch({ executablePath: process.env.ORQ_CHROMIUM, args: ['--no-sandbox'] });
const consoles = [];
async function aba(ctx) {
  const p = await ctx.newPage();
  p.avisosReq = 0;
  p.on('request', (r) => { if (r.url().endsWith('/avisos')) p.avisosReq++; });
  p.on('console', (m) => { if (m.type() === 'error' || m.type() === 'warning') consoles.push(`console.${m.type()}: ${m.text()}`); });
  p.on('pageerror', (e) => consoles.push(`pageerror: ${e.message}`));
  p.on('dialog', (d) => d.dismiss());
  return p;
}
const ciclos = async (...ps) => {
  await Promise.all(ps.map((p) => p.evaluate(() => Promise.all(window.__ciclos.map((f) => f())))));
  await Promise.all(ps.map((p) => p.waitForTimeout(400)));
};
const notificacoes = async (...ps) => (await Promise.all(ps.map((p) => p.evaluate(() => window.__n.map(({ titulo, corpo, tag }) => ({ titulo, corpo, tag })))))).flat();

// ---- permissão negada: nada muda, nada consulta
{
  const ctx = await navegador.newContext({ viewport: { width: 1280, height: 800 } });
  await ctx.addInitScript(() => localStorage.setItem('__perm', 'denied'));
  await ctx.addInitScript(simulado);
  const p = await aba(ctx);
  await p.goto(base + '/', { waitUntil: 'networkidle' });
  const b = p.locator('#avisos-btn');
  confere(await b.isVisible() && await b.isDisabled(), 'negada: o botão deveria aparecer desabilitado');
  confere((await b.innerText()).trim() === 'Avisos bloqueados', 'negada: rótulo');
  confere(p.avisosReq === 0, `negada: ${p.avisosReq} consultas a /avisos`);
  await ctx.close();
}

// ---- sem Notification: o botão fica escondido
{
  const ctx = await navegador.newContext();
  await ctx.addInitScript(() => { delete window.Notification; });
  const p = await aba(ctx);
  await p.goto(base + '/', { waitUntil: 'networkidle' });
  confere(!(await p.locator('#avisos-btn').isVisible()), 'indisponível: o botão deveria ficar escondido');
  confere(p.avisosReq === 0, 'indisponível: consultou /avisos');
  await ctx.close();
}

// ---- fluxo principal, duas abas
const ctx = await navegador.newContext({ viewport: { width: 1280, height: 800 } });
await ctx.addInitScript(simulado);
const p1 = await aba(ctx);
await p1.goto(base + '/', { waitUntil: 'networkidle' });
const b1 = p1.locator('#avisos-btn');
confere(await b1.isVisible() && !(await b1.isDisabled()), 'o botão "Ativar avisos" deveria estar visível e habilitado');
confere((await p1.evaluate(() => window.__pedidos)) === 0, 'a permissão foi pedida ao carregar');
confere(p1.avisosReq === 0, 'consultou /avisos sem permissão');
await Promise.all([p1.waitForResponse((r) => r.url().endsWith('/avisos')), b1.click()]);
await p1.waitForTimeout(300);
confere((await p1.evaluate(() => window.__pedidos)) === 1, 'o clique deveria pedir a permissão uma vez');
confere((await b1.innerText()).trim() === 'Avisos ativos' && await b1.isDisabled(), 'depois de concedida, "Avisos ativos"');
confere((await notificacoes(p1)).length === 0, 'o que já existia ao ativar notificou');

const p2 = await aba(ctx);
await p2.goto(base + '/pendencias', { waitUntil: 'networkidle' });
confere((await p2.locator('#avisos-btn').innerText()).trim() === 'Avisos ativos', 'a segunda aba nasce com os avisos ativos');
await p2.waitForTimeout(300);
confere((await notificacoes(p1, p2)).length === 0, 'abrir a segunda aba notificou o que já existia');

// pendência nova
await muda('nova-pend?proj=catalogo&ref=404');
await ciclos(p1, p2);
let ns = await notificacoes(p1, p2);
confere(ns.length === 1, `pendência nova: ${ns.length} notificações em duas abas, quer 1`);
if (ns.length !== 1) for (const [i, p] of [p1, p2].entries()) falha(`aba ${i + 1}: ${JSON.stringify(await p.evaluate(() => ({ n: window.__n.map((x) => x.tag), ciclos: window.__ciclos.length, reg: localStorage.getItem('orq.avisos.notificados'), base: sessionStorage.getItem('orq.avisos.base'), locks: !!navigator.locks })))}`);
const pend = ns[0];
confere(pend && /^Pendência catalogo\/404#P\d+$/.test(pend.titulo), `título da notificação: ${pend && pend.titulo}`);
confere(pend && pend.corpo === 'catalogo · Chegou depois da carga', `corpo da notificação: ${pend && pend.corpo}`);
await ciclos(p1, p2);
confere((await notificacoes(p1, p2)).length === 1, 'o mesmo item notificou de novo no ciclo seguinte');
// o clique leva à pendência (na aba que notificou)
const dona = (await p1.evaluate(() => window.__n.length)) ? p1 : p2;
await Promise.all([dona.waitForURL(/\/p\/catalogo\/404\/\d+$/), dona.evaluate(() => window.__n[0].clica())]);
confere(/\/p\/catalogo\/404\/\d+$/.test(dona.url()), `o clique foi para ${dona.url()}`);

// resposta nova do coordenador
await dona.goto(base + '/', { waitUntil: 'networkidle' });
const outra = dona === p1 ? p2 : p1;
await Promise.all([dona, outra].map((p) => p.waitForTimeout(300)));
const idMsg = (await muda('msg?onda=1')).trim();
await ciclos(p1, p2);
confere((await notificacoes(p1, p2)).filter((n) => n.titulo === 'Resposta do coordenador').length === 0, 'mensagem sem resposta notificou');
await muda(`resp-msg?id=${idMsg}`);
await ciclos(p1, p2);
const todas = await notificacoes(p1, p2);
const resp = todas.filter((n) => n.titulo === 'Resposta do coordenador');
confere(resp.length === 1, `resposta nova: ${resp.length} notificações, quer 1: ${JSON.stringify(todas)}`);
confere(todas.every((n) => !/texto livre|pergunta da pessoa/.test(n.corpo + n.titulo)), 'texto livre apareceu na notificação');
const quem = (await p1.evaluate(() => window.__n.some((n) => n.titulo === 'Resposta do coordenador'))) ? p1 : p2;
const antesDoClique = (await notificacoes(p1, p2)).length;
await quem.evaluate(() => window.__n.find((n) => n.titulo === 'Resposta do coordenador').clica());
await quem.waitForURL(/#msg-\d+$/);
await quem.waitForTimeout(400);
confere(await quem.locator(`#msg-${idMsg}`).isVisible(), 'o clique da resposta deveria abrir a conversa (item da mensagem visível)');
await ciclos(p1, p2);
// A aba que clicou recarregou (o #msg-N ainda não estava nela): a base dela vem da sessionStorage.
confere((await notificacoes(p1, p2)).length <= antesDoClique, 'notificação repetida depois do clique');

for (const c of consoles) falha(c);
await navegador.close();
console.log(JSON.stringify({ resumo: [], problemas }));
