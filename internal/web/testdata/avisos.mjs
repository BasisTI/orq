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
  // Botão hidden: o usuário continua na borda direita (o seletor "+ .cab-user" não pode casar).
  const g = await p.evaluate(() => { const u = document.querySelector('.cab-user').getBoundingClientRect(); return { dir: u.right, larg: document.querySelector('.cab-in').getBoundingClientRect().right }; });
  confere(g.larg - g.dir < 40, `indisponível: .cab-user fora da borda direita: ${JSON.stringify(g)}`);
  await ctx.close();
}

// ---- permissão concedida na outra aba: o clique na segunda aba também liga a consulta
{
  const ctx2 = await navegador.newContext({ viewport: { width: 1280, height: 800 } });
  await ctx2.addInitScript(simulado);
  const q1 = await aba(ctx2);
  const q2 = await aba(ctx2);
  await q1.goto(base + '/', { waitUntil: 'networkidle' });
  await q2.goto(base + '/pendencias', { waitUntil: 'networkidle' });
  await Promise.all([q1.waitForResponse((r) => r.url().endsWith('/avisos')), q1.locator('#avisos-btn').click()]);
  confere((await q2.locator('#avisos-btn').innerText()).trim() === 'Ativar avisos', 'a segunda aba ainda mostra "Ativar avisos"');
  await Promise.all([q2.waitForResponse((r) => r.url().endsWith('/avisos')), q2.locator('#avisos-btn').click()]);
  await q2.waitForTimeout(300);
  confere((await q2.locator('#avisos-btn').innerText()).trim() === 'Avisos ativos', 'segunda aba: rótulo');
  confere((await q2.evaluate(() => window.__ciclos.length)) === 1, 'segunda aba: "Avisos ativos" sem ciclo agendado');
  confere(q2.avisosReq >= 1, 'segunda aba: "Avisos ativos" sem consulta');
  await muda('nova-pend?proj=catalogo&ref=404');
  await q1.close();
  await ciclos(q2);
  confere((await notificacoes(q2)).length === 1, 'segunda aba sozinha deveria notificar a pendência nova');
  await ctx2.close();
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

// ---- clique na resposta com a pergunta feita pela página (#msg-N já no DOM, sem resposta)
await p2.close();
await p1.goto(base + '/', { waitUntil: 'networkidle' });
await p1.waitForTimeout(500);
await p1.evaluate(() => { for (let e = document.getElementById('onda-1-msgs-sec'); e; e = e.parentElement) if (e.tagName === 'DETAILS') e.open = true; });
await p1.fill('#onda-1-msgs-sec textarea[name=texto]', 'pergunta feita pela página');
await Promise.all([p1.waitForResponse((r) => r.url().includes('/onda/1/mensagens')), p1.click('#onda-1-msgs-sec button[type=submit]')]);
const novoId = await p1.evaluate(() => [...document.querySelectorAll('#onda-1-msgs .msg-item')].map((e) => Number(e.id.slice(4))).sort((a, b) => b - a)[0]);
confere(Number.isInteger(novoId), 'a pergunta enviada pela página deveria estar no DOM');
confere((await p1.locator(`#msg-${novoId} .msg-resposta`).count()) === 0, 'a pergunta ainda não tem resposta');
// Rascunhos que o clique não pode apagar: uma nova mensagem digitada e uma pendência aberta com texto.
await p1.fill('#onda-1-msgs-sec textarea[name=texto]', 'rascunho da mensagem');
const pendId = await p1.evaluate(() => {
  const d = document.querySelector('details.pend[data-estado="aberta"]');
  for (let e = d.parentElement; e; e = e.parentElement) if (e.tagName === 'DETAILS') e.open = true;
  return d.id;
});
await Promise.all([p1.waitForResponse((r) => r.url().includes('/corpo')), p1.click(`#${pendId} > summary`)]);
await p1.waitForSelector(`#${pendId} form textarea`, { state: 'visible' });
await p1.fill(`#${pendId} form textarea`, 'rascunho da pendência');
await muda(`resp-msg?id=${novoId}`);
await p1.evaluate(() => { window.__n.length = 0; window.__marca = 'mesma-pagina'; });
await ciclos(p1);
const nn = await notificacoes(p1);
confere(nn.length === 1 && nn[0].titulo === 'Resposta do coordenador', `resposta da pergunta feita pela página: ${JSON.stringify(nn)}`);
confere((await p1.locator(`#msg-${novoId} .msg-resposta`).count()) === 0, 'o /vivo não deveria ter trazido a resposta ainda (o teste perde o sentido)');
await p1.evaluate(() => window.__n[0].clica());
await p1.waitForSelector(`#msg-${novoId} .msg-resposta`, { state: 'visible', timeout: 5000 }).catch(() => falha('o clique deveria mostrar a resposta do coordenador (.msg-resposta visível)'));
confere(/#msg-\d+$/.test(p1.url()), `URL depois do clique: ${p1.url()}`);
confere((await p1.evaluate(() => window.__marca)) === 'mesma-pagina', 'o clique recarregou a página (perdeu rascunhos)');
confere((await p1.inputValue('#onda-1-msgs-sec textarea[name=texto]')) === 'rascunho da mensagem', 'rascunho da mensagem perdido');
confere((await p1.locator(`#${pendId}`).evaluate((d) => d.open)) && (await p1.inputValue(`#${pendId} form textarea`)) === 'rascunho da pendência', 'rascunho da pendência perdido ou item fechado');

for (const c of consoles) falha(c);
await navegador.close();
console.log(JSON.stringify({ resumo: [], problemas }));
