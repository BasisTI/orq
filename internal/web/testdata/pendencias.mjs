// Teste de navegador da story 3 (#24): a pendência no lugar, os erros dentro do formulário, o
// /vivo que não toca o corpo aberto, a cascata e o botão Voltar (go test -tags=browser; spec §8.1).
// Uso: node pendencias.mjs <base-url> <controle-url>; env: ORQ_PLAYWRIGHT (pasta do playwright-core),
// ORQ_CHROMIUM (executável), ORQ_CAPTURAS (pasta das capturas, opcional) e ORQ_LARGURA (1440, o padrão,
// roda tudo; 390 roda só a resposta no lugar e as capturas do celular, sobre um banco novo: as
// respostas dadas mudam o estado, então cada largura pede o seu).
import { pathToFileURL } from 'node:url';
import { join } from 'node:path';
import { mkdirSync } from 'node:fs';

const { chromium } = await import(pathToFileURL(join(process.env.ORQ_PLAYWRIGHT, 'index.mjs')).href);
const [base, controle] = process.argv.slice(2);
const capturas = process.env.ORQ_CAPTURAS;
const celular = process.env.ORQ_LARGURA === '390';
if (capturas) mkdirSync(capturas, { recursive: true });
const muda = async (acao, q = '') => { const r = await fetch(`${controle}/${acao}${q}`); if (!r.ok) falha(`controle ${acao}: ${r.status}`); };

const navegador = await chromium.launch({ executablePath: process.env.ORQ_CHROMIUM, args: ['--no-sandbox'] });
const problemas = [];
const falha = (m) => problemas.push(m);

// Os dois console.error de cada 4xx que o próprio teste provoca (spec §5.1, S6), e só eles.
const PERMITIDOS_4XX = ['Failed to load resource', 'Response Status Error Code'];
let esperando4xx = 0;
let passo = '';

async function abrir(largura, altura = 900, escala = 1) {
  const ctx = await navegador.newContext({ viewport: { width: largura, height: altura }, deviceScaleFactor: escala, reducedMotion: 'reduce' });
  const pagina = await ctx.newPage();
  const aqui = () => pagina.url().replace(base, '');
  pagina.on('console', (m) => {
    if (m.type() !== 'error' && m.type() !== 'warning') return;
    if (esperando4xx > 0 && PERMITIDOS_4XX.some((p) => m.text().includes(p))) return;
    falha(`console.${m.type()} em ${aqui()} [${passo}]: ${m.text().slice(0, 90)} @ ${JSON.stringify(m.location())}`);
  });
  pagina.on('pageerror', (e) => falha(`pageerror em ${aqui()}: ${e.message}`));
  pagina.on('requestfailed', (r) => falha(`requestfailed: ${r.url()} ${r.failure()?.errorText}`));
  await pagina.addInitScript(() => {
    window.__csp = [];
    document.addEventListener('securitypolicyviolation', (e) => window.__csp.push(`${e.violatedDirective} ${e.blockedURI}`));
    // htmx não deve falhar em silêncio: oob sem alvo e alvo inexistente são defeito.
    document.addEventListener('htmx:oobErrorNoTarget', (e) => window.__htmxErros = (window.__htmxErros || []).concat('oobErrorNoTarget ' + (e.detail?.content?.outerHTML || '').slice(0, 80)));
    document.addEventListener('htmx:targetError', (e) => window.__htmxErros = (window.__htmxErros || []).concat('targetError ' + e.detail?.target));
    document.addEventListener('htmx:swapError', () => window.__htmxErros = (window.__htmxErros || []).concat('swapError'));
  });
  return { ctx, pagina };
}

const verificarSaude = async (pagina, onde) => {
  const e = await pagina.evaluate(() => ({ csp: window.__csp, htmx: window.__htmxErros || [], // style="" vazio é o que o navegador deixa depois de mexer no CSSOM de um campo; o que vale é não haver style= com conteúdo.
    estilos: [...document.querySelectorAll('[style]')].filter((e) => e.getAttribute('style') !== '').map((e) => e.outerHTML.slice(0, 120)), scrollX: document.documentElement.scrollWidth - document.documentElement.clientWidth }));
  if (e.csp.length) falha(`${onde}: violação de CSP ${e.csp.join('; ')}`);
  if (e.htmx.length) falha(`${onde}: erro do htmx ${e.htmx.join('; ')}`);
  if (e.estilos.length) falha(`${onde}: ${e.estilos.length} elemento(s) com style=: ${e.estilos.join(' | ')}`);
  if (e.scrollX > 0) falha(`${onde}: rolagem horizontal de ${e.scrollX}px`);
};

const ate = async (pagina, fn, arg, onde) => {
  try { await pagina.waitForFunction(fn, arg, { timeout: 5000 }); } catch { falha(`tempo esgotado: ${onde}`); }
};
const foto = (pagina) => pagina.evaluate(() => ({
  url: location.pathname + location.search,
  cab: !!document.querySelector('.cab'), abas: !!document.querySelector('nav.abas'), conteudo: !!document.querySelector('#pend-conteudo'), rodape: !!document.querySelector('.rodape'),
  selProjeto: document.querySelector('#f-projeto')?.value, selCoord: document.querySelector('#f-coord')?.value, selOnda: document.querySelector('#f-onda')?.value,
  opcoesCoord: [...document.querySelectorAll('#f-coord option')].map((o) => o.value).join(','),
  opcoesOnda: [...document.querySelectorAll('#f-onda option')].map((o) => o.value).join(','),
  itens: [...document.querySelectorAll('#lista-esp .pend, #lista-resp .pend')].map((p) => p.id.replace('pend-', '')).join(','),
  nEsp: document.querySelector('#grupo-n-esp')?.textContent, nResp: document.querySelector('#grupo-n-resp')?.textContent,
  abaPend: document.querySelector('#aba-n-pendencias')?.textContent.trim(),
  vazioEsp: document.querySelector('#vazio-esp') ? !document.querySelector('#vazio-esp').hidden : null,
  vazioResp: document.querySelector('#vazio-resp') ? !document.querySelector('#vazio-resp').hidden : null,
  aviso: document.querySelector('#aviso')?.textContent.trim(),
}));

const filtrar = async (pagina, seletor, valor, esperaUrl) => {
  await pagina.focus(seletor);
  await Promise.all([pagina.waitForResponse((r) => r.url().includes('/pendencias') && r.request().headers()['hx-request'] === 'true'), pagina.selectOption(seletor, valor)]);
  await ate(pagina, (u) => location.pathname + location.search === u, esperaUrl, `URL ${esperaUrl}`);
  await pagina.waitForTimeout(120);
};

const cenario = async (nome, fn) => { passo = nome; try { await fn(); } catch (e) { falha(`${nome}: ${String(e.message).split('\n')[0]}`); } };

// ---------- A) cascata em uso, a URL guardando o filtro e o botão Voltar ----------
if (!celular) await cenario('A', async () => {
  const { ctx, pagina } = await abrir(1440);
  await pagina.goto(base + '/pendencias', { waitUntil: 'networkidle' });
  await pagina.evaluate(() => { window.__marca = 'mesma-pagina'; });
  const f0 = await foto(pagina);
  if (!f0.conteudo || f0.selProjeto !== '' || f0.nEsp !== '5') falha(`A: página inicial ${JSON.stringify(f0)}`);

  await filtrar(pagina, '#f-projeto', 'nuvem', '/pendencias?projeto=nuvem');
  const f1 = await foto(pagina);
  if (f1.opcoesCoord !== ',cais,farol' || f1.opcoesOnda !== ',5,6,9') falha(`A: opções depois do projeto: ${f1.opcoesCoord} | ${f1.opcoesOnda}`);
  await filtrar(pagina, '#f-coord', 'cais', '/pendencias?coord=cais&projeto=nuvem');
  const f2 = await foto(pagina);
  if (f2.opcoesOnda !== ',5,6' || f2.itens !== 'nuvem-78-1,nuvem-78-2') falha(`A: opções/itens depois do coordenador: ${f2.opcoesOnda} | ${f2.itens}`);
  if (f2.selProjeto !== 'nuvem' || f2.selCoord !== 'cais') falha(`A: selects depois dos dois filtros: ${f2.selProjeto}/${f2.selCoord}`);
  // O htmx devolve o foco ao select depois da troca (o id é estável): o teclado não se perde.
  // (O selectOption não foca o select; quem usa o teclado já estava nele.)
  if (await pagina.evaluate(() => document.activeElement?.id) !== 'f-coord') falha('A: o foco não voltou ao select do coordenador');
  await verificarSaude(pagina, 'A(cascata)');
  if (capturas) await pagina.screenshot({ path: join(capturas, 'cascata-1440.png'), fullPage: true });

  passo = 'A voltar';
  // Voltar depois de dois filtros mantém a página inteira (B3).
  for (const [alvo, esperado] of [['/pendencias?projeto=nuvem', 'nuvem'], ['/pendencias', '']]) {
    await pagina.goBack();
    await ate(pagina, (u) => location.pathname + location.search === u, alvo, `Voltar → ${alvo}`);
    await pagina.waitForTimeout(250);
    const f = await foto(pagina);
    if (!f.cab || !f.abas || !f.conteudo || !f.rodape) falha(`A: Voltar para ${alvo} não mantém a página inteira: ${JSON.stringify({ cab: f.cab, abas: f.abas, conteudo: f.conteudo, rodape: f.rodape })}`);
    if (f.selProjeto !== esperado || f.selCoord !== '') falha(`A: Voltar para ${alvo}: selects ${f.selProjeto}/${f.selCoord}`);
    if (await pagina.evaluate(() => document.querySelectorAll('html').length) !== 1) falha('A: mais de um <html> depois de Voltar');
  }
  await pagina.goForward();
  await ate(pagina, () => location.search === '?projeto=nuvem', null, 'Avançar');
  await pagina.waitForTimeout(250);
  const fa = await foto(pagina);
  if (!fa.cab || !fa.conteudo || fa.selProjeto !== 'nuvem') falha(`A: Avançar: ${JSON.stringify(fa)}`);

  passo = 'A trocar';
  // Trocar um filtro de cima limpa os de baixo.
  await filtrar(pagina, '#f-coord', 'cais', '/pendencias?coord=cais&projeto=nuvem');
  await filtrar(pagina, '#f-onda', '5', '/pendencias?coord=cais&onda=5&projeto=nuvem');
  await filtrar(pagina, '#f-projeto', 'catalogo', '/pendencias?projeto=catalogo');
  const fc = await foto(pagina);
  if (fc.selCoord !== '' || fc.selOnda !== '') falha(`A: trocar o projeto não limpou os de baixo: ${fc.selCoord}/${fc.selOnda}`);
  // Combinação impossível na URL é normalizada, sem erro.
  await pagina.goto(base + '/pendencias?projeto=catalogo&coord=bussola&onda=3', { waitUntil: 'networkidle' });
  const fi = await foto(pagina);
  if (fi.url !== '/pendencias?projeto=catalogo' || fi.selCoord !== '') falha(`A: combinação impossível não foi normalizada: ${fi.url}`);
  await verificarSaude(pagina, 'A');
  await ctx.close();
});

// ---------- B) o /vivo não toca o corpo aberto nem o texto digitado (aba Pendências) ----------
if (!celular) await cenario('B', async () => {
  const { ctx, pagina } = await abrir(1440);
  await pagina.clock.install();
  await pagina.goto(base + '/pendencias', { waitUntil: 'networkidle' });
  const id = '#pend-catalogo-405-1';
  await Promise.all([pagina.waitForResponse((r) => r.url().includes('/p/catalogo/405/1/corpo')), pagina.click(`${id} > summary`)]);
  await ate(pagina, (s) => document.querySelector(`${s} .pend-corpo form`), id, 'B: corpo carregado');
  await pagina.fill(`${id} textarea`, 'texto que ainda não foi enviado');
  const antes = await foto(pagina);
  await muda('responder', '?proj=catalogo&ref=404&n=2');
  await Promise.all([pagina.waitForResponse((r) => r.url().includes('/vivo')), pagina.clock.runFor(30500)]);
  await pagina.waitForTimeout(300);
  const depois = await pagina.evaluate((s) => ({ aberto: document.querySelector(s).open, texto: document.querySelector(`${s} textarea`)?.value, corpo: !!document.querySelector(`${s} .pend-corpo form`), aviso: document.querySelector('#aviso-lista')?.textContent.trim(), aba: document.querySelector('#aba-n-pendencias')?.textContent.trim(), vivos: document.querySelectorAll('#vivo').length }), id);
  if (!depois.aberto || depois.texto !== 'texto que ainda não foi enviado' || !depois.corpo) falha(`B: o /vivo mexeu no corpo aberto: ${JSON.stringify(depois)}`);
  if (Number(depois.aba) !== Number(antes.abaPend) - 1) falha(`B: a aba Pendências não foi atualizada pelo /vivo: ${antes.abaPend} → ${depois.aba}`);
  if (!depois.aviso?.startsWith('A lista mudou')) falha(`B: pendência respondida por outro: sem o aviso (${JSON.stringify(depois.aviso)})`);
  if (depois.vivos !== 1) falha(`B: ${depois.vivos} #vivo`);
  await verificarSaude(pagina, 'B');
  await ctx.close();
});

// ---------- C) erros dentro do formulário, sem perder o texto ----------
if (!celular) await cenario('C', async () => {
  const { ctx, pagina } = await abrir(1440);
  await pagina.goto(base + '/', { waitUntil: 'networkidle' });
  // A pendência fica dentro da US: abre a onda, a US e a pendência.
  await pagina.evaluate(() => { document.querySelector('#onda-1').open = true; document.querySelector('#us-catalogo-405').open = true; });
  const id = '#pend-catalogo-405-1';
  await Promise.all([pagina.waitForResponse((r) => r.url().includes('/p/catalogo/405/1/corpo')), pagina.click(`${id} > summary`)]);
  await ate(pagina, (s) => document.querySelector(`${s} .pend-corpo form`), id, 'C: corpo carregado');
  const estado = () => pagina.evaluate((s) => ({ form: !!document.querySelector(`${s} form`), texto: document.querySelector(`${s} textarea`)?.value, erro: document.querySelector(`${s} .erro`)?.textContent.trim(), resumo: !!document.querySelector(`${s} > summary`), item: !!document.querySelector(s), opcao: document.querySelector(`${s} input[name=opcao]:checked`)?.value }), id);
  const enviar = async (corpoTexto, via) => {
    esperando4xx += 2;
    await pagina.fill(`${id} textarea`, corpoTexto);
    await pagina.check(`${id} input[name=opcao][value="2"]`);
    const [r] = await Promise.all([pagina.waitForResponse((x) => x.url().includes('/responder')), pagina.click(`${id} button[type=submit]`)]);
    await pagina.waitForTimeout(250);
    return r.status();
  };

  // 413: o texto passa de 8 KiB (em bytes; o maxlength do campo conta caracteres).
  const grande = 'é'.repeat(5000);
  let st = await enviar(grande);
  let e = await estado();
  if (st !== 413 || !e.form || e.texto !== grande || !e.erro || !e.resumo || e.opcao !== '2') falha(`C: 413 → ${st} ${JSON.stringify({ ...e, texto: e.texto?.length })}`);
  await verificarSaude(pagina, 'C(413)');
  if (capturas) { await pagina.locator(id).scrollIntoViewIfNeeded(); await pagina.screenshot({ path: join(capturas, 'erro-413-1440.png'), fullPage: false }); }

  // 415: o tipo do corpo trocado no caminho (o formulário real só manda urlencoded).
  await pagina.route('**/responder', (rota) => rota.continue({ headers: { ...rota.request().headers(), 'content-type': 'application/json' } }));
  st = await enviar('texto antes do 415');
  await pagina.unroute('**/responder');
  e = await estado();
  if (st !== 415 || !e.form || e.texto !== 'texto antes do 415' || !e.erro || !e.resumo) falha(`C: 415 → ${st} ${JSON.stringify(e)}`);

  // 409: a pendência foi resolvida pelo coordenador enquanto a página estava aberta.
  await muda('resolver', '?proj=catalogo&ref=405&n=1');
  st = await enviar('texto antes do 409');
  e = await estado();
  if (st !== 409 || !e.form || e.texto !== 'texto antes do 409' || !e.erro || !e.resumo) falha(`C: 409 → ${st} ${JSON.stringify(e)}`);
  if (!/resolvida/i.test(e.erro || '')) falha(`C: o alerta do 409 não diz que foi resolvida: ${e.erro}`);
  await verificarSaude(pagina, 'C');
  esperando4xx = 0;
  await ctx.close();
});

// ---------- D) responder no lugar, na US e na aba, e trocar a resposta ----------
for (const [nome, largura, altura, escala] of celular ? [['390', 390, 844, 2]] : [['1440', 1440, 900, 1]]) await cenario(`D(${nome})`, async () => {
  const { ctx, pagina } = await abrir(largura, altura, escala);

  // D1) na US (aba Todos): o item respondido e fechado fica no lugar; a aba, a onda e a US contam.
  await pagina.goto(base + '/#pend-nuvem-78-1', { waitUntil: 'networkidle' });
  // O endereço com #pend-… abre a cadeia de ancestrais e dispara o toggle: o corpo carrega.
  await ate(pagina, () => document.querySelector('#pend-nuvem-78-1 .pend-corpo form'), null, 'D1: hash abre e carrega o corpo');
  const antes = await pagina.evaluate(() => ({ aba: document.querySelector('#aba-n-pendencias').textContent.trim(), onda: document.querySelector('#onda-5-barra .onda-sinais').textContent.replace(/\s+/g, ' ').trim(), us: document.querySelector('#us-nuvem-78-barra .conta-wrap').textContent.replace(/\s+/g, ' ').trim(), situacao: document.querySelector('#situacao').textContent.replace(/\s+/g, ' ').trim() }));
  await pagina.fill('#pend-nuvem-78-1 textarea', 'autorizado, pode seguir');
  await pagina.check('#pend-nuvem-78-1 input[name=opcao][value="1"]');
  await Promise.all([pagina.waitForResponse((r) => r.url().includes('/responder')), pagina.click('#pend-nuvem-78-1 button[type=submit]')]);
  await pagina.waitForTimeout(300);
  const d1 = await pagina.evaluate(() => ({ item: document.querySelector('#pend-nuvem-78-1')?.className, aberto: document.querySelector('#pend-nuvem-78-1')?.open, selo: document.querySelector('#pend-nuvem-78-1 .pend-estado')?.textContent.trim(), aba: document.querySelector('#aba-n-pendencias').textContent.trim(), onda: document.querySelector('#onda-5-barra .onda-sinais').textContent.replace(/\s+/g, ' ').trim(), us: document.querySelector('#us-nuvem-78-barra .conta-wrap').textContent.replace(/\s+/g, ' ').trim(), situacao: document.querySelector('#situacao').textContent.replace(/\s+/g, ' ').trim(), aviso: document.querySelector('#aviso').textContent.trim(), usAberta: document.querySelector('#us-nuvem-78').open, qtd: document.querySelectorAll('#pend-nuvem-78-1').length }));
  if (d1.qtd !== 1 || !d1.item.includes('pend-ok') || d1.aberto || d1.selo !== 'respondida') falha(`D1(${nome}): item depois de responder: ${JSON.stringify(d1)}`);
  if (Number(d1.aba) !== Number(antes.aba) - 1) falha(`D1(${nome}): aba Pendências ${antes.aba} → ${d1.aba}`);
  if (d1.onda === antes.onda || d1.us === antes.us || d1.situacao === antes.situacao) falha(`D1(${nome}): contagens da onda/US/situação não mudaram: ${JSON.stringify({ antes, d1 })}`);
  if (!d1.aviso.startsWith('Resposta enviada: TG-78#P1')) falha(`D1(${nome}): aviso ${JSON.stringify(d1.aviso)}`);
  if (!d1.usAberta) falha(`D1(${nome}): a US fechou`);
  await verificarSaude(pagina, `D1(${nome})`);
  if (capturas) { await pagina.locator('#us-nuvem-78').scrollIntoViewIfNeeded(); await pagina.screenshot({ path: join(capturas, `respondida-na-us-${nome}.png`), fullPage: true }); }

  // Trocar a resposta: o corpo da respondida mostra a resposta e o botão; o formulário volta com a atual.
  await pagina.click('#pend-nuvem-78-1 > summary');
  await ate(pagina, () => document.querySelector('#pend-nuvem-78-1 .resp-linha'), null, 'D1: corpo da respondida');
  await pagina.click('#pend-nuvem-78-1 .pend-corpo button');
  await ate(pagina, () => document.querySelector('#pend-nuvem-78-1 form'), null, 'D1: formulário de trocar');
  const tr = await pagina.evaluate(() => ({ opcao: document.querySelector('#pend-nuvem-78-1 input[name=opcao]:checked')?.value, texto: document.querySelector('#pend-nuvem-78-1 textarea').value }));
  if (tr.opcao !== '1' || tr.texto !== 'autorizado, pode seguir') falha(`D1(${nome}): trocar não indica a resposta atual: ${JSON.stringify(tr)}`);
  await pagina.check('#pend-nuvem-78-1 input[name=opcao][value="2"]');
  await Promise.all([pagina.waitForResponse((r) => r.url().includes('/responder')), pagina.click('#pend-nuvem-78-1 button[type=submit]')]);
  await pagina.waitForTimeout(300);
  const d1b = await pagina.evaluate(() => ({ aba: document.querySelector('#aba-n-pendencias').textContent.trim(), aviso: document.querySelector('#aviso').textContent.trim(), qtd: document.querySelectorAll('#pend-nuvem-78-1').length }));
  if (d1b.aba !== d1.aba || d1b.qtd !== 1 || !d1b.aviso.startsWith('Resposta trocada')) falha(`D1(${nome}): trocar mudou contagem ou item: ${JSON.stringify(d1b)}`);
  await verificarSaude(pagina, `D1(${nome})`);

  // D2) na aba Pendências: o item muda de grupo e o grupo vazio mostra a frase.
  await pagina.goto(base + '/pendencias?onda=3', { waitUntil: 'networkidle' });
  const a2 = await foto(pagina);
  if (a2.itens !== 'faturamento-513-1' || a2.nEsp !== '1' || a2.nResp !== '0' || a2.vazioEsp !== false || a2.vazioResp !== true) falha(`D2(${nome}): antes ${JSON.stringify(a2)}`);
  await pagina.click('#pend-faturamento-513-1 > summary');
  await ate(pagina, () => document.querySelector('#pend-faturamento-513-1 form'), null, 'D2: corpo carregado');
  await pagina.fill('#pend-faturamento-513-1 textarea', 'sim, pode seguir com a fila');
  await Promise.all([pagina.waitForResponse((r) => r.url().includes('/responder')), pagina.click('#pend-faturamento-513-1 button[type=submit]')]);
  await pagina.waitForTimeout(300);
  const d2 = await foto(pagina);
  const grupo = await pagina.evaluate(() => ({ emEsp: !!document.querySelector('#lista-esp #pend-faturamento-513-1'), emResp: !!document.querySelector('#lista-resp #pend-faturamento-513-1'), qtd: document.querySelectorAll('#pend-faturamento-513-1').length, fechado: !document.querySelector('#pend-faturamento-513-1').open, vivos: document.querySelectorAll('#vivo').length }));
  if (grupo.emEsp || !grupo.emResp || grupo.qtd !== 1 || !grupo.fechado) falha(`D2(${nome}): o item não mudou de grupo: ${JSON.stringify(grupo)}`);
  if (d2.nEsp !== '0' || d2.nResp !== '1' || d2.vazioEsp !== true || d2.vazioResp !== false) falha(`D2(${nome}): contagens/frases depois: ${JSON.stringify(d2)}`);
  if (Number(d2.abaPend) !== Number(a2.abaPend) - 1) falha(`D2(${nome}): aba Pendências ${a2.abaPend} → ${d2.abaPend}`);
  if (grupo.vivos !== 1) falha(`D2(${nome}): ${grupo.vivos} #vivo`);
  const vazioTexto = await pagina.evaluate(() => document.querySelector('#vazio-esp').textContent.trim());
  if (vazioTexto !== 'Nenhuma pendência esperando para este filtro.') falha(`D2(${nome}): frase do grupo vazio: ${vazioTexto}`);
  // Abrir o item que mudou de grupo busca o corpo da respondida.
  await pagina.click('#lista-resp #pend-faturamento-513-1 > summary');
  await ate(pagina, () => document.querySelector('#lista-resp #pend-faturamento-513-1 .resp-linha'), null, 'D2: corpo da respondida');
  await verificarSaude(pagina, `D2(${nome})`);
  if (capturas) { await pagina.evaluate(() => window.scrollTo(0, 0)); await pagina.screenshot({ path: join(capturas, `respondida-na-aba-${nome}.png`), fullPage: true }); }

  // A cascata em uso, para a captura nas duas larguras.
  await pagina.goto(base + '/pendencias', { waitUntil: 'networkidle' });
  await filtrar(pagina, '#f-projeto', 'nuvem', '/pendencias?projeto=nuvem');
  await filtrar(pagina, '#f-coord', 'cais', '/pendencias?coord=cais&projeto=nuvem');
  await pagina.click('#pend-nuvem-78-2 > summary');
  await ate(pagina, () => document.querySelector('#pend-nuvem-78-2 form'), null, 'cascata: corpo aberto');
  await verificarSaude(pagina, `cascata(${nome})`);
  if (capturas) { await pagina.evaluate(() => window.scrollTo(0, 0)); await pagina.screenshot({ path: join(capturas, `cascata-em-uso-${nome}.png`), fullPage: true }); }
  await ctx.close();
});

await navegador.close();
console.log(JSON.stringify({ problemas }));
process.exit(problemas.length ? 1 : 0);
