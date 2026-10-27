// Teste de navegador do envio que não chega ao servidor (go test -tags=browser; spec §5.3, "Envio
// que falha sem resposta HTTP"). Uso: node rede.mjs <base-url>; env: ORQ_PLAYWRIGHT, ORQ_CHROMIUM e
// ORQ_CASO (rede, rede-primeiro, pendurado, pendurado com filtro, 409 sem aviso duplicado; um caso
// por banco novo, porque responder muda o estado).
// O timeout do formulário (20 s no modelo) é trocado aqui por 1,5 s no atributo hx-request, que o
// htmx lê quando o pedido sai; o resto do caminho é o de produção.
import { pathToFileURL } from 'node:url';
import { join } from 'node:path';

const { chromium } = await import(pathToFileURL(join(process.env.ORQ_PLAYWRIGHT, 'index.mjs')).href);
const [base] = process.argv.slice(2);
const caso = process.env.ORQ_CASO;
const problemas = [];
const falha = (m) => problemas.push(m);
const navegador = await chromium.launch({ executablePath: process.env.ORQ_CHROMIUM, args: ['--no-sandbox'] });
const MSG = 'Não foi possível confirmar o envio';
// O htmx registra no console o evento de erro (htmx:afterRequest) e o navegador, o recurso que falhou.
const ESPERADOS = /^console.error: htmx:(afterRequest|sendError|timeout|sendAbort)$|Failed to load resource|ERR_CONNECTION_FAILED/;
const IDS = ['nuvem-78-1', 'nuvem-78-2', 'catalogo-405-1'];
const url = (id) => `/p/${id.replace(/-(\d+)-(\d+)$/, '/$1/$2')}/responder`;

async function abrir() {
  const ctx = await navegador.newContext({ viewport: { width: 390, height: 844 } });
  const pagina = await ctx.newPage();
  const c = { ctx, pagina, consoles: [], dialogos: 0 };
  pagina.on('console', (m) => { if (m.type() === 'error' || m.type() === 'warning') c.consoles.push(`console.${m.type()}: ${m.text()}`); });
  pagina.on('pageerror', (e) => c.consoles.push(`pageerror: ${e.message}`));
  pagina.on('dialog', (d) => { c.dialogos++; d.dismiss(); });
  await pagina.addInitScript(() => {
    window.__csp = []; window.__htmx = [];
    document.addEventListener('securitypolicyviolation', (e) => window.__csp.push(`${e.violatedDirective} ${e.blockedURI}`));
    for (const ev of ['htmx:oobErrorNoTarget', 'htmx:targetError', 'htmx:swapError', 'htmx:evalDisallowedError']) document.addEventListener(ev, () => window.__htmx.push(ev));
  });
  return c;
}
const abrirItem = async (pagina, id) => {
  await Promise.all([pagina.waitForResponse((r) => r.url().includes('/corpo') && r.url().includes(id.split('-')[1])), pagina.click(`#pend-${id} > summary`)]);
  await pagina.waitForSelector(`#pend-${id} form`);
};
const preencher = async (pagina, id) => {
  await abrirItem(pagina, id);
  await pagina.fill(`#pend-${id} textarea`, `texto ${id}`);
  if (await pagina.locator(`#pend-${id} input[name=opcao]`).count()) await pagina.check(`#pend-${id} input[name=opcao][value="1"]`);
};
const timeoutCurto = (pagina) => pagina.evaluate(() => document.querySelectorAll('form.resposta[hx-post]').forEach((f) => f.setAttribute('hx-request', '{"timeout":1500}')));
const enviar = (pagina, id) => pagina.click(`#pend-${id} button[type=submit]`);
const solto = (pagina, id) => pagina.evaluate((id) => {
  const f = document.querySelector(`#pend-${id} form`);
  if (!f) return null;
  return {
    erro: f.querySelector('.erro')?.textContent.trim() ?? null, papel: f.querySelector('.erro')?.getAttribute('role'),
    visivel: !!f.querySelector('.erro')?.getClientRects().length,
    readOnly: f.querySelector('textarea').readOnly, radios: [...f.querySelectorAll('input[name=opcao]')].some((r) => r.disabled),
    botao: f.querySelector('button[type=submit]').disabled, busy: f.hasAttribute('aria-busy'), texto: f.querySelector('textarea').value,
  };
}, id);
const livre = (pagina) => pagina.evaluate(() => ({ espera: document.querySelector('#espera')?.textContent ?? '', busy: document.querySelector('#principal')?.hasAttribute('aria-busy') }));
const gravadas = async () => {
  const c = await abrir();
  await c.pagina.goto(base + '/pendencias', { waitUntil: 'networkidle' });
  const r = await c.pagina.evaluate(() => [...document.querySelectorAll('#lista-resp .pend')].map((p) => p.id));
  await c.ctx.close();
  return IDS.filter((id) => r.includes(`pend-${id}`));
};
const saude = async (c, onde) => {
  const e = await c.pagina.evaluate(() => ({ csp: window.__csp, htmx: window.__htmx }));
  if (e.csp.length) falha(`${onde}: CSP ${e.csp}`);
  if (e.htmx.length) falha(`${onde}: htmx ${e.htmx}`);
  for (const m of c.consoles) if (!ESPERADOS.test(m)) falha(`${onde}: ${m}`);
};
// Depois da falha: a mensagem está visível no .erro (role=alert) e o formulário está solto, com o
// texto; nada preso; recarregar não pede confirmação.
const conferirFalha = async (c, id, onde) => {
  const s = await solto(c.pagina, id);
  if (!s) return falha(`${onde}: o formulário de ${id} sumiu`);
  if (!s.erro?.startsWith(MSG) || !s.visivel || s.papel !== 'alert') falha(`${onde}: sem a mensagem visível no .erro de ${id} (${JSON.stringify(s)})`);
  if (s.readOnly || s.radios || s.botao || s.busy) falha(`${onde}: campo preso em ${id} (${JSON.stringify(s)})`);
  if (s.texto !== `texto ${id}`) falha(`${onde}: o texto de ${id} não foi preservado (${s.texto})`);
};
const conferirLivre = async (c, onde) => {
  const l = await livre(c.pagina);
  if (l.espera || l.busy) falha(`${onde}: espera presa (${JSON.stringify(l)})`);
  const presos = await c.pagina.evaluate(() => document.querySelectorAll('form.resposta[aria-busy], form.resposta textarea[readonly], form.resposta button:disabled').length);
  if (presos) falha(`${onde}: ${presos} campo(s) preso(s)`);
};
const recarregar = async (c, onde) => {
  const antes = c.dialogos;
  await c.pagina.reload({ waitUntil: 'networkidle' });
  if (c.dialogos !== antes) falha(`${onde}: recarregar pediu confirmação`);
};
const idDe = (u) => IDS.find((id) => u.includes(url(id)));

// Roteia os POSTs: cada um segue o que `acao[id]` manda (continuar, abortar, pendurar ou segurar até soltar()).
async function rotear(pagina, acao) {
  const posts = [];
  let soltar;
  const portao = new Promise((r) => { soltar = r; });
  let pronto;
  const primeira = new Promise((r) => { pronto = r; });
  await pagina.route('**/responder', async (rota) => {
    const id = idDe(rota.request().url());
    posts.push(id);
    const a = acao[id];
    if (a === 'pendurar') { await new Promise(() => {}); }
    if (a === 'segurar-e-cair' || a === 'segurar') { pronto(); await portao; }
    if (a === 'cair' || a === 'segurar-e-cair') { await rota.abort('connectionfailed'); return; }
    await rota.continue();
  });
  return { posts, soltar, primeira };
}

// rede: P1 retido, P2 e P3 na fila; a conexão cai em P2. rede-primeiro: cai em P1.
for (const [nome, acaoDe] of [
  ['rede', { 'nuvem-78-1': 'segurar', 'nuvem-78-2': 'cair' }],
  ['rede-primeiro', { 'nuvem-78-1': 'segurar-e-cair' }],
]) if (caso === nome) {
  const c = await abrir();
  const { pagina } = c;
  await pagina.goto(base + '/pendencias', { waitUntil: 'networkidle' });
  for (const id of IDS) await preencher(pagina, id);
  const g = await rotear(pagina, { ...acaoDe });
  await enviar(pagina, 'nuvem-78-1');
  await g.primeira;
  await enviar(pagina, 'nuvem-78-2');
  await enviar(pagina, 'catalogo-405-1');
  g.soltar();
  const caiu = nome === 'rede' ? 'nuvem-78-2' : 'nuvem-78-1';
  await pagina.waitForFunction((id) => document.querySelector(`#pend-${id} .erro`)?.textContent.trim(), caiu, { timeout: 8000 }).catch(() => {});
  await pagina.waitForFunction(() => document.querySelectorAll('#lista-resp .pend-ok').length >= 2, null, { timeout: 8000 }).catch(() => {});
  await pagina.waitForTimeout(300);
  await conferirFalha(c, caiu, nome);
  await conferirLivre(c, nome);
  // A fila seguiu: os outros dois foram gravados, o que caiu não.
  const gr = await gravadas();
  const esperadas = IDS.filter((id) => id !== caiu);
  if (gr.join() !== esperadas.join()) falha(`${nome}: gravadas ${gr}, esperava ${esperadas}`);
  if (g.posts.length !== 3) falha(`${nome}: ${g.posts.length} POSTs, esperava 3`);
  // Reenvio: a rede voltou; o mesmo formulário, sem retocar o texto, grava.
  await pagina.unroute('**/responder');
  await Promise.all([pagina.waitForResponse((r) => r.url().includes('/responder')), enviar(pagina, caiu)]);
  await pagina.waitForTimeout(300);
  if ((await gravadas()).length !== 3) falha(`${nome}: o reenvio não gravou`);
  await conferirLivre(c, `${nome} (depois do reenvio)`);
  await recarregar(c, nome);
  await saude(c, nome);
  await c.ctx.close();
}

// pendurado: P1 nunca recebe resposta, P2 na fila. O timeout do formulário faz o mesmo caminho do erro.
if (caso === 'pendurado') {
  const c = await abrir();
  const { pagina } = c;
  await pagina.goto(base + '/pendencias', { waitUntil: 'networkidle' });
  await preencher(pagina, 'nuvem-78-1');
  await preencher(pagina, 'nuvem-78-2');
  await timeoutCurto(pagina);
  const g = await rotear(pagina, { 'nuvem-78-1': 'pendurar' });
  await enviar(pagina, 'nuvem-78-1');
  await enviar(pagina, 'nuvem-78-2');
  await pagina.waitForTimeout(600);
  const antes = await solto(pagina, 'nuvem-78-1');
  if (!antes.botao || !antes.readOnly) falha('pendurado: o formulário em voo não ficou travado antes do timeout');
  await pagina.waitForFunction(() => document.querySelector('#pend-nuvem-78-1 .erro')?.textContent.trim(), null, { timeout: 8000 }).catch(() => {});
  await pagina.waitForFunction(() => document.querySelectorAll('#lista-resp .pend-ok').length >= 1, null, { timeout: 8000 }).catch(() => {});
  await pagina.waitForTimeout(300);
  await conferirFalha(c, 'nuvem-78-1', 'pendurado');
  await conferirLivre(c, 'pendurado');
  if (g.posts.length !== 2) falha(`pendurado: ${g.posts.length} POSTs, esperava 2 (a fila seguiu)`);
  if ((await gravadas()).join() !== 'nuvem-78-2') falha('pendurado: P2 não foi gravada depois do timeout de P1');
  await pagina.unroute('**/responder');
  await Promise.all([pagina.waitForResponse((r) => r.url().includes('/responder')), enviar(pagina, 'nuvem-78-1')]);
  await pagina.waitForTimeout(300);
  if ((await gravadas()).length !== 2) falha('pendurado: o reenvio não gravou');
  await recarregar(c, 'pendurado');
  await saude(c, 'pendurado');
  await c.ctx.close();
}

// Com filtro esperando: a faixa #espera aparece e some quando o timeout libera o envio pendurado.
if (caso === 'pendurado com filtro') {
  const c = await abrir();
  const { pagina } = c;
  await pagina.goto(base + '/pendencias', { waitUntil: 'networkidle' });
  await preencher(pagina, 'nuvem-78-1');
  await timeoutCurto(pagina);
  await rotear(pagina, { 'nuvem-78-1': 'pendurar' });
  await enviar(pagina, 'nuvem-78-1');
  await pagina.selectOption('#f-projeto', 'catalogo');
  await pagina.waitForTimeout(500);
  const e = await livre(pagina);
  if (!e.espera || !e.busy) falha(`pendurado com filtro: a espera não foi dita (${JSON.stringify(e)})`);
  await pagina.waitForURL('**/pendencias?projeto=catalogo', { timeout: 8000 }).catch(() => {});
  await pagina.waitForTimeout(300);
  if ((await pagina.evaluate(() => location.search)) !== '?projeto=catalogo') falha('pendurado com filtro: o filtro ficou preso depois do timeout');
  await conferirLivre(c, 'pendurado com filtro');
  await recarregar(c, 'pendurado com filtro');
  await saude(c, 'pendurado com filtro');
  await c.ctx.close();
}

// 409 (resolvida por fora) e 200 continuam como antes: a mensagem do servidor, sem o aviso de rede.
if (caso === '409 sem aviso duplicado') {
  const c = await abrir();
  const { pagina } = c;
  await pagina.goto(base + '/pendencias', { waitUntil: 'networkidle' });
  await preencher(pagina, 'nuvem-78-1');
  await pagina.route('**/responder', (rota) => rota.fulfill({ status: 409, contentType: 'text/html; charset=utf-8', headers: { 'HX-Retarget': 'find .erro', 'HX-Reswap': 'innerHTML' }, body: '<p>TG-78#P1 já foi resolvida.</p>' }));
  await enviar(pagina, 'nuvem-78-1');
  await pagina.waitForFunction(() => document.querySelector('#pend-nuvem-78-1 .erro')?.textContent.trim(), null, { timeout: 5000 }).catch(() => {});
  await pagina.waitForTimeout(300);
  const s = await solto(pagina, 'nuvem-78-1');
  if (!s?.erro?.startsWith('TG-78#P1 já foi resolvida') || s.erro.includes(MSG)) falha(`409: o .erro não traz só a mensagem do servidor (${s?.erro})`);
  if (await pagina.evaluate(() => document.querySelectorAll('#pend-nuvem-78-1 .erro p').length) !== 1) falha('409: aviso duplicado no .erro');
  if (s.readOnly || s.botao || s.busy) falha(`409: campo preso (${JSON.stringify(s)})`);
  await conferirLivre(c, '409');
  await recarregar(c, '409');
  await c.ctx.close();
}

await navegador.close();
console.log(JSON.stringify({ problemas }));
process.exit(problemas.length ? 1 : 0);
