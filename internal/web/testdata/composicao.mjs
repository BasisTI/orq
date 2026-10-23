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

// M3 (#28): dois envios na mesma aba antes de a primeira resposta voltar. A rota segura cada
// resposta até os dois POSTs terem saído (ou 1,5 s, quando o segundo só sai depois da primeira
// resposta, que é o que a serialização faz) e a entrega na ordem. Depois das duas, #comp e os
// contadores batem com as linhas, o ciclo seguinte não avisa à toa, as duas respostas estão
// gravadas (recarregar mostra as duas em Respondidas) e o segundo POST levou o #comp já atualizado.
if (caso === 'concorrente') {
  const c = await abrir();
  const { pagina } = c;
  const posts = [];
  let saidos = 0;
  await pagina.route('**/responder', async (rota) => {
    const corpo = new URLSearchParams(rota.request().postData() ?? '');
    posts.push({ url: rota.request().url(), comp: corpo.get('comp'), opcao: corpo.get('opcao') });
    saidos++;
    const resposta = await rota.fetch();
    const fim = Date.now() + 1500;
    while (saidos < 2 && Date.now() < fim) await new Promise((r) => setTimeout(r, 25));
    await rota.fulfill({ response: resposta });
  });
  await pagina.goto(base + '/pendencias?projeto=nuvem', { waitUntil: 'networkidle' });
  await abrirItem(pagina, 'nuvem-78-1');
  await abrirItem(pagina, 'nuvem-78-2');
  await pagina.fill('#pend-nuvem-78-2 textarea', 'segunda resposta');
  await pagina.fill('#pend-nuvem-78-1 textarea', 'comentário que fica');
  await pagina.check('#pend-nuvem-78-1 input[name=opcao][value="1"]');
  if (await pagina.locator('#pend-nuvem-78-2 input[name=opcao]').count()) await pagina.check('#pend-nuvem-78-2 input[name=opcao][value="1"]');
  await pagina.click('#pend-nuvem-78-1 button[type=submit]');
  await pagina.click('#pend-nuvem-78-2 button[type=submit]');
  await pagina.waitForFunction(() => document.querySelectorAll('#lista-resp .pend-ok').length >= 2, null, { timeout: 8000 }).catch(() => {});
  await pagina.waitForTimeout(400);
  const e = await estado(pagina);
  coerente(e, 'concorrente');
  if (posts.length !== 2) falha(`concorrente: ${posts.length} POSTs, esperava 2`);
  if (posts.length === 2 && posts[0].comp === posts[1].comp) falha(`concorrente: os dois POSTs levaram o mesmo comp (${posts[0].comp})`);
  if (!e.resp.includes('pend-nuvem-78-1') || !e.resp.includes('pend-nuvem-78-2')) falha(`concorrente: os dois itens não estão em Respondidas (${e.esp} | ${e.resp})`);
  if (e.esp.includes('pend-nuvem-78-1') || e.esp.includes('pend-nuvem-78-2')) falha(`concorrente: item ainda em Esperando (${e.esp})`);
  const compEsp = e.comp.split(';')[0].replace(/^e:/, '').split(',').filter(Boolean).length;
  const compResp = e.comp.split(';')[1]?.replace(/^r:/, '').split(',').filter(Boolean).length ?? 0;
  const nEsp = e.esp ? e.esp.split(',').length : 0, nResp = e.resp ? e.resp.split(',').length : 0;
  if (compEsp !== nEsp || compResp !== nResp) falha(`concorrente: #comp ${e.comp} com ${nEsp}/${nResp} linhas`);
  if (e.aviso) falha(`concorrente: aviso espúrio antes do ciclo (${e.aviso})`);
  const o = await pagina.evaluate(() => document.querySelectorAll('#aviso').length);
  if (o !== 1) falha(`concorrente: ${o} #aviso`);
  await ciclo(pagina);
  const d = await estado(pagina);
  coerente(d, 'concorrente (depois do ciclo)');
  if (d.aviso) falha(`concorrente: "A lista mudou" espúrio depois do ciclo (${d.aviso}); #comp=${d.comp}`);
  // As duas gravadas: uma página nova (outro contexto, sem relógio falso) mostra as duas respondidas.
  const c2 = await abrir();
  await c2.pagina.goto(base + '/pendencias?projeto=nuvem', { waitUntil: 'networkidle' });
  const r = await estado(c2.pagina);
  for (const id of ['pend-nuvem-78-1', 'pend-nuvem-78-2']) if (!r.resp.includes(id)) falha(`concorrente: ${id} não foi gravada (${r.esp} | ${r.resp})`);
  await saude(c, 'concorrente');
  await saude(c2, 'concorrente (recarga)');
  await c2.ctx.close();
  await c.ctx.close();
}

// O mesmo caso na US dentro da Central: as contagens são as do servidor (idempotentes), então não há
// composição a combinar; as duas respostas voltam em ordem e nada fica fora do lugar.
if (caso === 'concorrente na Central') {
  const c = await abrir();
  const { pagina } = c;
  let saidos = 0;
  await pagina.route('**/responder', async (rota) => {
    saidos++;
    const resposta = await rota.fetch();
    const fim = Date.now() + 1500;
    while (saidos < 2 && Date.now() < fim) await new Promise((r) => setTimeout(r, 25));
    await rota.fulfill({ response: resposta });
  });
  await pagina.goto(base + '/', { waitUntil: 'networkidle' });
  await pagina.evaluate(() => {
    const us = document.querySelector('#us-nuvem-78');
    for (let e = us; e; e = e.parentElement?.closest('details')) e.open = true;
  });
  await abrirItem(pagina, 'nuvem-78-1');
  await abrirItem(pagina, 'nuvem-78-2');
  await pagina.fill('#pend-nuvem-78-1 textarea', 'primeira');
  await pagina.fill('#pend-nuvem-78-2 textarea', 'segunda');
  for (const n of [1, 2]) if (await pagina.locator(`#pend-nuvem-78-${n} input[name=opcao]`).count()) await pagina.check(`#pend-nuvem-78-${n} input[name=opcao][value="1"]`);
  await pagina.click('#pend-nuvem-78-1 button[type=submit]');
  await pagina.click('#pend-nuvem-78-2 button[type=submit]');
  await pagina.waitForFunction(() => document.querySelectorAll('#us-nuvem-78 .pend-ok').length >= 2, null, { timeout: 8000 }).catch(() => {});
  await pagina.waitForTimeout(400);
  const e = await pagina.evaluate(() => ({
    ok: document.querySelectorAll('#us-nuvem-78 .pend-ok').length,
    total: document.querySelectorAll('#us-nuvem-78 .pend').length,
    selo: document.querySelector('#us-nuvem-78-barra .selo-aviso')?.textContent.trim() ?? null,
    avisos: document.querySelectorAll('#aviso').length,
  }));
  if (e.ok !== e.total || e.total < 2) falha(`Central: ${e.ok} respondidas de ${e.total} pendências na US`);
  if (e.selo) falha(`Central: a US ainda diz "${e.selo}" com tudo respondido`);
  if (e.avisos !== 1) falha(`Central: ${e.avisos} #aviso`);
  await saude(c, 'concorrente na Central');
  await c.ctx.close();
}

// S1 e M1 (#28 r2): a fila de envios (hx-sync "queue all") guarda pedidos presos ao formulário, e o
// htmx descarta o que sai da fila com o formulário já fora da página. Por isso filtrar ou usar o
// Voltar com envio na fila espera a fila acabar: nenhum envio some, o texto não enviado fica até lá.
// A rota segura a primeira resposta (depois de o servidor gravar) até a página tentar sair.
const preencher = async (pagina, id, texto) => {
  await abrirItem(pagina, id);
  await pagina.fill(`#pend-${id} textarea`, texto);
  if (await pagina.locator(`#pend-${id} input[name=opcao]`).count()) await pagina.check(`#pend-${id} input[name=opcao][value="1"]`);
};
const segurar = async (pagina) => {
  const posts = [];
  let soltar;
  const portao = new Promise((r) => { soltar = r; });
  let pronto;
  const primeira = new Promise((r) => { pronto = r; });
  await pagina.route('**/responder', async (rota) => {
    const i = posts.length;
    posts.push(rota.request().url());
    const resposta = await rota.fetch();
    if (i === 0) { pronto(); await portao; }
    await rota.fulfill({ response: resposta });
  });
  return { posts, soltar, primeira };
};
const gravadas = async (id) => {
  const c = await abrir();
  await c.pagina.goto(base + '/pendencias', { waitUntil: 'networkidle' });
  const r = await estado(c.pagina);
  await c.ctx.close();
  return id.filter((x) => r.resp.includes(`pend-${x}`));
};

if (caso === 'cascata') {
  const c = await abrir();
  const { pagina } = c;
  const ids = ['nuvem-78-1', 'nuvem-78-2', 'catalogo-405-1'];
  await pagina.goto(base + '/pendencias', { waitUntil: 'networkidle' });
  for (const id of ids) await preencher(pagina, id, `texto ${id}`);
  const g = await segurar(pagina);
  await pagina.click('#pend-nuvem-78-1 button[type=submit]');
  await g.primeira;
  await pagina.click('#pend-nuvem-78-2 button[type=submit]');
  await pagina.click('#pend-catalogo-405-1 button[type=submit]');
  await pagina.selectOption('#f-projeto', 'catalogo');
  await pagina.waitForTimeout(400);
  let e = await pagina.evaluate(() => ({ url: location.pathname + location.search, textos: [...document.querySelectorAll('form.resposta textarea')].map((t) => t.value) }));
  if (e.url !== '/pendencias') falha(`cascata: o filtro trocou a página com envio na fila (${e.url})`);
  if (!e.textos.includes('texto nuvem-78-2') || !e.textos.includes('texto catalogo-405-1')) falha(`cascata: o texto dos envios na fila sumiu (${e.textos})`);
  g.soltar();
  await pagina.waitForURL('**/pendencias?projeto=catalogo');
  await pagina.waitForTimeout(600);
  if (g.posts.length !== 3) falha(`cascata: ${g.posts.length} POSTs, esperava 3`);
  const f = await estado(pagina);
  coerente(f, 'cascata (depois do filtro)');
  const gr = await gravadas(ids);
  if (gr.length !== 3) falha(`cascata: ${gr.length} respostas gravadas de 3 (${gr})`);
  await saude(c, 'cascata');
  await c.ctx.close();
}

if (caso === 'voltar') {
  const c = await abrir();
  const { pagina } = c;
  const ids = ['nuvem-78-1', 'nuvem-78-2'];
  await pagina.goto(base + '/pendencias', { waitUntil: 'networkidle' });
  await pagina.selectOption('#f-projeto', 'nuvem');
  await pagina.waitForURL('**/pendencias?projeto=nuvem');
  for (const id of ids) await preencher(pagina, id, `texto ${id}`);
  const g = await segurar(pagina);
  await pagina.click('#pend-nuvem-78-1 button[type=submit]');
  await g.primeira;
  await pagina.click('#pend-nuvem-78-2 button[type=submit]');
  await pagina.goBack();
  await pagina.waitForURL('**/pendencias');
  await pagina.waitForTimeout(400);
  const e = await pagina.evaluate(() => [...document.querySelectorAll('form.resposta textarea')].map((t) => t.value));
  if (!e.includes('texto nuvem-78-2')) falha(`voltar: a página foi restaurada com envio na fila (${e})`);
  g.soltar();
  await pagina.waitForFunction(() => !document.querySelector('#f-projeto')?.value, null, { timeout: 8000 }).catch(() => {});
  await pagina.waitForTimeout(600);
  if (g.posts.length !== 2) falha(`voltar: ${g.posts.length} POSTs, esperava 2`);
  const f = await estado(pagina);
  coerente(f, 'voltar (depois de restaurar)');
  if (await pagina.evaluate(() => document.querySelector('#f-projeto')?.value) !== '') falha('voltar: o histórico não foi restaurado depois da fila');
  const gr = await gravadas(ids);
  if (gr.length !== 2) falha(`voltar: ${gr.length} respostas gravadas de 2 (${gr})`);
  await saude(c, 'voltar');
  await c.ctx.close();
}

// M1 (r1 da revisão): um /vivo no meio da fila vê a gravação que o #comp ainda não tem e avisa; o
// aviso sai no ciclo seguinte à fila, quando a composição volta a bater.
if (caso === 'vivo no meio da fila') {
  const c = await abrir();
  const { pagina } = c;
  const ids = ['nuvem-78-1', 'nuvem-78-2'];
  await pagina.goto(base + '/pendencias?projeto=nuvem', { waitUntil: 'networkidle' });
  for (const id of ids) await preencher(pagina, id, `texto ${id}`);
  const g = await segurar(pagina);
  await pagina.click('#pend-nuvem-78-1 button[type=submit]');
  await g.primeira;
  await pagina.click('#pend-nuvem-78-2 button[type=submit]');
  await ciclo(pagina);
  if (!(await estado(pagina)).aviso?.startsWith('A lista mudou')) falha('vivo no meio da fila: o ciclo no meio da fila não viu a diferença');
  g.soltar();
  await pagina.waitForFunction(() => document.querySelectorAll('#lista-resp .pend-ok').length >= 2, null, { timeout: 8000 }).catch(() => {});
  await pagina.waitForTimeout(400);
  coerente(await estado(pagina), 'vivo no meio da fila');
  await ciclo(pagina);
  const e = await estado(pagina);
  coerente(e, 'vivo no meio da fila (depois do ciclo)');
  if (e.aviso) falha(`vivo no meio da fila: o aviso ficou preso (${e.aviso})`);
  await saude(c, 'vivo no meio da fila');
  await c.ctx.close();
}

// S2 (r2 da revisão): o toque repetido em Enviar não gera envio a mais nem deixa um envio fantasma
// que prenda os filtros. Os toques extras entram por requestSubmit, porque o botão fica desabilitado
// depois do primeiro. M2a: enquanto a fila espera, a página diz isso.
if (caso === 'toque repetido') {
  const c = await abrir();
  const { pagina } = c;
  const ids = ['nuvem-78-1', 'nuvem-78-2'];
  await pagina.goto(base + '/pendencias?projeto=nuvem', { waitUntil: 'networkidle' });
  for (const id of ids) await preencher(pagina, id, `texto ${id}`);
  const g = await segurar(pagina);
  await pagina.click('#pend-nuvem-78-1 button[type=submit]');
  await g.primeira;
  await pagina.click('#pend-nuvem-78-2 button[type=submit]');
  for (let i = 0; i < 3; i++) await pagina.evaluate(() => document.querySelector('#pend-nuvem-78-2 form').requestSubmit());
  await pagina.selectOption('#f-projeto', 'catalogo');
  await pagina.waitForTimeout(300);
  const e = await pagina.evaluate(() => ({
    url: location.pathname + location.search, espera: document.querySelector('#espera')?.textContent,
    busy: document.querySelector('#principal').getAttribute('aria-busy'), btn: document.querySelector('#pend-nuvem-78-2 button[type=submit]').disabled,
  }));
  if (e.url !== '/pendencias?projeto=nuvem') falha(`toque repetido: o filtro trocou a página com envio na fila (${e.url})`);
  if (!e.espera || !e.busy) falha(`toque repetido: a espera não foi indicada (${JSON.stringify(e)})`);
  if (!e.btn) falha('toque repetido: o botão do envio na fila continua habilitado');
  g.soltar();
  await pagina.waitForURL('**/pendencias?projeto=catalogo', { timeout: 8000 }).catch(() => {});
  await pagina.waitForTimeout(500);
  const f = await pagina.evaluate(() => ({ url: location.pathname + location.search, espera: document.querySelector('#espera')?.textContent, busy: document.querySelector('#principal').getAttribute('aria-busy') }));
  if (f.url !== '/pendencias?projeto=catalogo') falha(`toque repetido: o filtro ficou preso depois dos envios (${f.url})`);
  if (f.espera || f.busy) falha(`toque repetido: a espera não sumiu (${JSON.stringify(f)})`);
  if (g.posts.length !== 2) falha(`toque repetido: ${g.posts.length} POSTs, esperava 2`);
  coerente(await estado(pagina), 'toque repetido');
  const gr = await gravadas(ids);
  if (gr.length !== 2) falha(`toque repetido: ${gr.length} respostas gravadas de 2 (${gr})`);
  await pagina.selectOption('#f-projeto', 'nuvem');
  await pagina.waitForURL('**/pendencias?projeto=nuvem', { timeout: 8000 }).catch(() => {});
  if ((await pagina.evaluate(() => location.search)) !== '?projeto=nuvem') falha('toque repetido: o segundo filtro não passou');
  await saude(c, 'toque repetido');
  await c.ctx.close();
}

// S2, defesa em profundidade: um envio que o htmx abandona (formulário fora da página, sem
// htmx:afterRequest) não prende os filtros.
if (caso === 'envio abandonado') {
  const c = await abrir();
  const { pagina } = c;
  await pagina.goto(base + '/pendencias', { waitUntil: 'networkidle' });
  await pagina.evaluate(() => {
    const f = document.createElement('form');
    f.className = 'resposta';
    f.setAttribute('hx-post', '/nada');
    document.body.appendChild(f);
    f.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
    f.remove();
  });
  await pagina.selectOption('#f-projeto', 'catalogo');
  await pagina.waitForURL('**/pendencias?projeto=catalogo', { timeout: 5000 }).catch(() => {});
  if ((await pagina.evaluate(() => location.search)) !== '?projeto=catalogo') falha('envio abandonado: o filtro ficou preso por um envio fantasma');
  await saude(c, 'envio abandonado');
  await c.ctx.close();
}

// M2b: o que estava escolhido no clique é o que vai ao servidor, mesmo que o campo mude (aqui por
// script, porque os campos ficam travados) enquanto o envio espera na fila.
if (caso === 'campos fixados') {
  const c = await abrir();
  const { pagina } = c;
  const corpos = [];
  pagina.on('request', (r) => { if (r.url().includes('/responder')) corpos.push(r.postData()); });
  await pagina.goto(base + '/pendencias?projeto=nuvem', { waitUntil: 'networkidle' });
  await preencher(pagina, 'nuvem-78-1', 'texto nuvem-78-1');
  await abrirItem(pagina, 'nuvem-78-2');
  await pagina.check('#pend-nuvem-78-2 input[name=opcao][value="1"]');
  await pagina.fill('#pend-nuvem-78-2 textarea', 'texto do clique');
  const g = await segurar(pagina);
  await pagina.click('#pend-nuvem-78-1 button[type=submit]');
  await g.primeira;
  await pagina.click('#pend-nuvem-78-2 button[type=submit]');
  const t = await pagina.evaluate(() => {
    const f = document.querySelector('#pend-nuvem-78-2 form');
    const ta = f.querySelector('textarea'), r2 = f.querySelector('input[name=opcao][value="2"]');
    const travado = ta.readOnly && r2.disabled;
    ta.value = 'editado depois do clique';
    r2.checked = true;
    return travado;
  });
  if (!t) falha('campos fixados: os campos do envio na fila não ficaram travados');
  g.soltar();
  await pagina.waitForFunction(() => document.querySelectorAll('#lista-resp .pend-ok').length >= 2, null, { timeout: 8000 }).catch(() => {});
  const dois = corpos[1] ? new URLSearchParams(corpos[1]) : null;
  if (!dois || dois.get('opcao') !== '1' || dois.get('texto') !== 'texto do clique') falha(`campos fixados: o segundo POST levou ${corpos[1]}`);
  await saude(c, 'campos fixados');
  await c.ctx.close();
}

await navegador.close();
console.log(JSON.stringify({ problemas }));
process.exit(problemas.length ? 1 : 0);
