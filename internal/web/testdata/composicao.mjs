// Teste de navegador da composição da aba Pendências (go test -tags=browser; spec §5.3 e §6).
// O POST move o item e atualiza o #comp só com a mudança dele: o que mudou fora do POST entre a
// carga e o envio continua sendo avisado pelo /vivo, e os outros formulários não são tocados.
// Uso: node composicao.mjs <base-url> <controle-url>; env: ORQ_PLAYWRIGHT, ORQ_CHROMIUM e ORQ_CASO
// (um caso por banco novo, porque responder muda o estado).
import { pathToFileURL } from 'node:url';
import { join } from 'node:path';

const { chromium } = await import(pathToFileURL(join(process.env.ORQ_PLAYWRIGHT, 'index.mjs')).href);
const [base, controle] = process.argv.slice(2);
const caso = process.env.ORQ_CASO;
const problemas = [];
const falha = (m) => problemas.push(m);
const muda = async (acao, q = '') => { const r = await fetch(`${controle}/${acao}${q}`); if (!r.ok) falha(`controle ${acao}: ${r.status}`); };
const navegador = await chromium.launch({ executablePath: process.env.ORQ_CHROMIUM, args: ['--no-sandbox'] });

async function abrir(reduzido = false) {
  const ctx = await navegador.newContext({ viewport: { width: 390, height: 844 }, reducedMotion: reduzido ? 'reduce' : 'no-preference' });
  const pagina = await ctx.newPage();
  const consoles = [];
  pagina.on('console', (m) => { if (m.type() === 'error' || m.type() === 'warning') consoles.push(`console.${m.type()}: ${m.text()}`); });
  pagina.on('pageerror', (e) => consoles.push(`pageerror: ${e.message}`));
  await pagina.addInitScript(() => {
    window.__csp = []; window.__htmx = [];
    document.addEventListener('securitypolicyviolation', (e) => window.__csp.push(`${e.violatedDirective} ${e.blockedURI}`));
    for (const ev of ['htmx:oobErrorNoTarget', 'htmx:targetError', 'htmx:swapError']) document.addEventListener(ev, () => window.__htmx.push(ev));
  });
  await pagina.clock.install();
  return { ctx, pagina, consoles };
}

const abrirItem = async (pagina, id) => {
  await Promise.all([pagina.waitForResponse((r) => r.url().includes(`/${id}/corpo`.replace('pend-', '').replace(/-/g, '/'))), pagina.click(`#pend-${id} > summary`)]);
  await pagina.waitForSelector(`#pend-${id} form`);
};
const estado = (pagina) => pagina.evaluate(() => ({
  esp: [...document.querySelectorAll('#lista-esp .pend')].map((p) => p.id).join(','),
  resp: [...document.querySelectorAll('#lista-resp .pend')].map((p) => p.id).join(','),
  nEsp: document.querySelector('#grupo-n-esp').textContent, nResp: document.querySelector('#grupo-n-resp').textContent,
  vazioEsp: !document.querySelector('#vazio-esp').hidden,
  aviso: document.querySelector('#aviso-lista')?.textContent.trim(),
  comp: document.querySelector('#comp').value, vivos: document.querySelectorAll('#vivo').length,
}));
const ciclo = async (pagina) => {
  await Promise.all([pagina.waitForResponse((r) => r.url().includes('/vivo')), pagina.clock.runFor(30500)]);
  await pagina.waitForTimeout(250);
};
const enviar = async (pagina, id, opcao) => {
  if (opcao) await pagina.check(`#pend-${id} input[name=opcao][value="${opcao}"]`);
  else await pagina.fill(`#pend-${id} textarea`, 'resposta de teste');
  await Promise.all([pagina.waitForResponse((r) => r.url().includes('/responder')), pagina.click(`#pend-${id} button[type=submit]`)]);
  await pagina.waitForTimeout(250);
};
const coerente = (e, onde) => {
  const nEsp = e.esp ? e.esp.split(',').length : 0, nResp = e.resp ? e.resp.split(',').length : 0;
  if (Number(e.nEsp) !== nEsp || Number(e.nResp) !== nResp) falha(`${onde}: contadores ${e.nEsp}/${e.nResp} com ${nEsp}/${nResp} linhas`);
  if (e.vazioEsp !== (nEsp === 0)) falha(`${onde}: frase do grupo vazio (${e.vazioEsp}) com ${nEsp} esperando`);
};
const saude = async (c, onde) => {
  const e = await c.pagina.evaluate(() => ({ csp: window.__csp, htmx: window.__htmx }));
  if (e.csp.length) falha(`${onde}: CSP ${e.csp}`);
  if (e.htmx.length) falha(`${onde}: htmx ${e.htmx}`);
  for (const m of c.consoles) falha(`${onde}: ${m}`);
};

// Cada caso: mudança fora do POST entre a carga e o envio; depois responde 513 P1 (duvida), com
// 78 P2 aberta e digitada ao lado (não pode ser tocada). Em todos: o item muda de grupo, os
// contadores batem com as linhas, o aviso aparece no ciclo seguinte e o texto do outro fica.
const casos = [
  ['pendência nova', async () => muda('nova-pend', '?proj=nuvem&ref=78')],
  ['pendência resolvida por outro', async () => muda('resolver', '?proj=catalogo&ref=404&n=2')],
  ['onda nova', async () => muda('nova-onda')],
  ['onda encerrada', async () => muda('encerrar-onda')],
];
for (const [nome, mudanca] of casos) if (caso === nome) {
  const c = await abrir();
  const { pagina } = c;
  await pagina.goto(base + '/pendencias', { waitUntil: 'networkidle' });
  await abrirItem(pagina, 'nuvem-78-2');
  await pagina.fill('#pend-nuvem-78-2 textarea', 'texto que não pode ser tocado');
  await abrirItem(pagina, 'faturamento-513-1');
  await mudanca();
  await enviar(pagina, 'faturamento-513-1');
  let e = await estado(pagina);
  if (!e.resp.includes('pend-faturamento-513-1') || e.esp.includes('pend-faturamento-513-1')) falha(`${nome}: o item não mudou de grupo (${e.esp} | ${e.resp})`);
  coerente(e, nome);
  if (e.vivos !== 1) falha(`${nome}: ${e.vivos} #vivo`);
  if (e.aviso) falha(`${nome}: aviso antes do ciclo: ${e.aviso}`);
  await ciclo(pagina);
  e = await estado(pagina);
  if (!e.aviso?.startsWith('A lista mudou')) falha(`${nome}: sem o aviso no ciclo seguinte (${JSON.stringify(e.aviso)}); #comp=${e.comp}`);
  coerente(e, `${nome} (depois do ciclo)`);
  const o = await pagina.evaluate(() => ({ texto: document.querySelector('#pend-nuvem-78-2 textarea')?.value, aberto: document.querySelector('#pend-nuvem-78-2').open }));
  if (o.texto !== 'texto que não pode ser tocado' || !o.aberto) falha(`${nome}: o outro formulário foi tocado ${JSON.stringify(o)}`);
  await ciclo(pagina);
  if (!(await estado(pagina)).aviso?.startsWith('A lista mudou')) falha(`${nome}: o aviso sumiu no segundo ciclo`);
  await saude(c, nome);
  await c.ctx.close();
}

// Sem mudança alheia, responder não faz o ciclo seguinte avisar à toa.
if (caso === 'sem mudança alheia') {
  const c = await abrir();
  const { pagina } = c;
  await pagina.goto(base + '/pendencias?projeto=nuvem', { waitUntil: 'networkidle' });
  await abrirItem(pagina, 'nuvem-78-1');
  await enviar(pagina, 'nuvem-78-1', '1');
  await ciclo(pagina);
  const e = await estado(pagina);
  coerente(e, 'sem mudança alheia');
  if (e.aviso) falha(`sem mudança alheia: aviso à toa (${e.aviso})`);
  await saude(c, 'sem mudança alheia');
  await c.ctx.close();
}

// M1: a mesma pendência respondida por outra aba antes do envio: o item da página antiga muda de grupo.
if (caso === 'M1') {
  const c = await abrir();
  const { pagina } = c;
  await pagina.goto(base + '/pendencias?onda=3', { waitUntil: 'networkidle' });
  await abrirItem(pagina, 'faturamento-513-1');
  await muda('responder', '?proj=faturamento&ref=513&n=1');
  await enviar(pagina, 'faturamento-513-1');
  const e = await estado(pagina);
  if (!e.resp.includes('pend-faturamento-513-1') || e.esp.includes('pend-faturamento-513-1')) falha(`M1: o item da aba antiga não mudou de grupo (${e.esp} | ${e.resp})`);
  coerente(e, 'M1');
  await saude(c, 'M1');
  await c.ctx.close();
}

// M2: com movimento reduzido a confirmação some de uma vez, depois do mesmo tempo, sem fade.
if (caso === 'M2') {
  const c = await abrir(true);
  const { pagina } = c;
  await pagina.goto(base + '/pendencias?onda=7', { waitUntil: 'networkidle' });
  const antesDe = (await pagina.evaluate(() => document.querySelector('#aviso').textContent));
  await muda('nova-pend', '?proj=agenda&ref=240');
  await pagina.goto(base + '/pendencias?projeto=agenda', { waitUntil: 'networkidle' });
  const id = await pagina.evaluate(() => document.querySelector('#lista-esp .pend')?.id.replace('pend-', ''));
  await abrirItem(pagina, id);
  await enviar(pagina, id);
  const v = () => pagina.evaluate(() => { const a = document.querySelector('#aviso'); const s = getComputedStyle(a); return { texto: a.textContent.trim(), visivel: s.visibility, opacidade: s.opacity, anim: s.animationName }; });
  const a0 = await v();
  if (antesDe || !a0.texto.startsWith('Resposta enviada') || a0.visivel !== 'visible' || a0.anim !== 'aviso-corte') falha(`M2: confirmação no modo reduzido ${JSON.stringify(a0)}`);
  await pagina.waitForTimeout(5000); // animação de CSS: relógio real, não o do htmx
  const a1 = await v();
  if (a1.visivel !== 'visible' || a1.opacidade !== '1') falha(`M2: mudou antes do tempo ou com fade ${JSON.stringify(a1)}`);
  await pagina.waitForTimeout(1700);
  const a2 = await v();
  if (a2.visivel !== 'hidden' || a2.opacidade !== '1') falha(`M2: não sumiu de uma vez depois do tempo ${JSON.stringify(a2)}`);
  await saude(c, 'M2');
  await c.ctx.close();
}

await navegador.close();
console.log(JSON.stringify({ problemas }));
process.exit(problemas.length ? 1 : 0);
