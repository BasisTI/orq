// Teste de navegador da web do orq (go test -tags=browser). Abre cada página no Chromium e
// falha em qualquer violação de CSP e em qualquer erro de console fora da lista de permissões
// (spec §8.1, S6). Uso: node navegador.mjs <base-url> <caminho>... ; env: ORQ_PLAYWRIGHT (pasta
// do playwright-core), ORQ_CHROMIUM (executável), ORQ_CAPTURAS (pasta das capturas, opcional).
import { pathToFileURL } from 'node:url';
import { join } from 'node:path';
import { mkdirSync } from 'node:fs';

const { chromium } = await import(pathToFileURL(join(process.env.ORQ_PLAYWRIGHT, 'index.mjs')).href);
const [base, ...pedidos] = process.argv.slice(2);
// "<caminho>[=<status esperado>]". Com 401, os estáticos vão com o cabeçalho do proxy (a página de
// erro é o que se mede; sem isso o CSS também seria 401 e ela sairia sem estilo, como no socket
// Unix real).
const caminhos = pedidos.map((x) => { const [c, st] = x.split('='); return [c, Number(st) || 200]; });
const capturas = process.env.ORQ_CAPTURAS;
const prefixo = process.env.ORQ_CAPTURA_PREFIXO ?? '';
if (capturas) mkdirSync(capturas, { recursive: true });

// Os dois console.error de cada 4xx provocado de propósito (spec §5.1, S6). Só as páginas de erro
// pedidas com "=<status>" (ex.: /onda/999=404) os admitem, e só eles: qualquer outra mensagem
// reprova.
const PERMITIDOS_4XX = ['Failed to load resource', 'Response Status Error Code'];


// Os itens abertos da revisão final do protótipo (spec §11, M15) viram verificação aqui.
async function checarCentral(pagina, nome, falhas) {
  const m = await pagina.evaluate(() => {
    const linhaUnica = (el) => el.getBoundingClientRect().height <= parseFloat(getComputedStyle(el).lineHeight || '0') * 1.3 + 1;
    const barras = [...document.querySelectorAll('details.onda > summary')];
    const lim = [...document.querySelectorAll('.onda-lim')];
    const link = document.querySelector('.situacao-lista a');
    const faixa = document.querySelector('.abas-in');
    const atual = faixa?.querySelector('a[aria-current="page"]');
    const rA = atual?.getBoundingClientRect(), rF = faixa?.getBoundingClientRect();
    return {
      ondas: barras.length,
      progressos: document.querySelectorAll('details.onda progress.prog-bar').length,
      barrasDeProgressoSemProgress: document.querySelectorAll('.prog-bar:not(progress)').length,
      comStyle: document.querySelectorAll('[style]').length,
      limiteEmUmaLinha: lim.every(linhaUnica),
      limiteMaisLongo: lim.map((e) => e.textContent).sort((a, b) => b.length - a.length)[0] ?? null,
      linkSublinhado: link ? getComputedStyle(link).textDecorationLine.includes('underline') : null,
      faixaRola: faixa ? faixa.scrollWidth > faixa.clientWidth + 1 : false,
      faixaEsmaecida: faixa ? (faixa.hasAttribute('data-dir') || faixa.hasAttribute('data-esq')) && getComputedStyle(faixa).maskImage !== 'none' : false,
      abaAtivaNaVista: rA && rF ? rA.left >= rF.left - 1 && rA.right <= rF.right + 1 : false,
      sobreposicao: barras.some((b) => b.scrollWidth > b.clientWidth + 1),
    };
  });
  if (m.ondas && !m.progressos) falhas.push('barra de progresso: nenhum <progress> nas ondas');
  if (m.barrasDeProgressoSemProgress) falhas.push('barra de progresso que não é <progress>');
  if (m.comStyle) falhas.push(`${m.comStyle} elemento(s) com style=`);
  if (nome === '1440' && !m.limiteEmUmaLinha) falhas.push(`coluna "limite · ordem" quebra em 1440 px (${m.limiteMaisLongo})`);
  if (m.linkSublinhado === false) falhas.push('o link "N pendências esperam você" não tem sublinhado');
  if (nome === '390' && m.faixaRola && !m.faixaEsmaecida) falhas.push('a faixa de abas rola no celular e não indica (borda esmaecida)');
  if (!m.abaAtivaNaVista) falhas.push('a aba ativa não está inteira na vista da faixa');
  if (m.sobreposicao) falhas.push('barra de onda com conteúdo para fora da própria largura');
}

const navegador = await chromium.launch({ executablePath: process.env.ORQ_CHROMIUM, args: ['--no-sandbox'] });
const problemas = [];
const resumo = [];

for (const [nome, largura, altura, escala] of [['390', 390, 844, 2], ['1440', 1440, 900, 1]]) {
  const ctx = await navegador.newContext({ viewport: { width: largura, height: altura }, deviceScaleFactor: escala, reducedMotion: 'reduce' });
  for (const [caminho, esperado] of caminhos) {
    const pagina = await ctx.newPage();
    const aqui = `${caminho}@${nome}`;
    const falhas = [];
    pagina.on('console', (m) => {
      if (m.type() !== 'error' && m.type() !== 'warning') return;
      const texto = m.text();
      if (esperado >= 400 && PERMITIDOS_4XX.some((p) => texto.includes(p))) return;
      falhas.push(`console.${m.type()}: ${texto}`);
    });
    pagina.on('pageerror', (e) => falhas.push(`pageerror: ${e.message}`));
    pagina.on('requestfailed', (r) => falhas.push(`requestfailed: ${r.url()} ${r.failure()?.errorText}`));
    pagina.on('response', (r) => {
      if (r.status() >= 400 && !(esperado >= 400 && r.request().isNavigationRequest() && r.status() === esperado)) falhas.push(`HTTP ${r.status()}: ${r.url()}`);
    });
    await pagina.addInitScript(() => {
      window.__csp = [];
      document.addEventListener('securitypolicyviolation', (e) => window.__csp.push(`${e.violatedDirective} ${e.blockedURI}`));
    });
    if (esperado === 401) {
      await pagina.route('**/estatico/**', (rota) => rota.continue({ headers: { ...rota.request().headers(), 'x-orq-user': 'teste' } }));
    }
    const resp = await pagina.goto(base + caminho, { waitUntil: 'networkidle' });
    if (resp.status() !== esperado) falhas.push(`status ${resp.status()}, esperado ${esperado}`);
    await pagina.evaluate(() => document.fonts.ready);

    const estado = await pagina.evaluate(() => ({
      csp: window.__csp,
      comStyle: document.querySelectorAll('[style]').length,
      scriptsInline: [...document.scripts].filter((s) => !s.src).length,
      htmx: typeof window.htmx,
      allowEval: window.htmx?.config?.allowEval,
      historyRestoreAsHxRequest: window.htmx?.config?.historyRestoreAsHxRequest,
      historyCacheSize: window.htmx?.config?.historyCacheSize,
      carlito: document.fonts.check('16px Carlito') && [...document.fonts].some((f) => f.family.replace(/"/g, '') === 'Carlito' && f.status === 'loaded'),
      interTight: [...document.fonts].some((f) => f.family.replace(/"/g, '') === 'Inter Tight' && f.status === 'loaded'),
      fundoCabecalho: (document.querySelector('.cab') ? getComputedStyle(document.querySelector('.cab')).backgroundColor : null),
      abaAtiva: document.querySelector('.abas-in a[aria-current="page"]')?.textContent.trim() ?? null,
      icone: document.querySelector('link[rel=icon]')?.href,
      larguraRolavel: document.documentElement.scrollWidth - document.documentElement.clientWidth,
    }));
    if (estado.csp.length) falhas.push(`violação de CSP: ${estado.csp.join('; ')}`);
    if (estado.comStyle) falhas.push(`${estado.comStyle} elemento(s) com style=`);
    if (estado.scriptsInline) falhas.push(`${estado.scriptsInline} <script> inline`);
    if (estado.htmx !== 'object' || estado.allowEval !== false || estado.historyRestoreAsHxRequest !== false || estado.historyCacheSize !== 0) {
      falhas.push(`htmx/meta: ${JSON.stringify({ htmx: estado.htmx, allowEval: estado.allowEval, h: estado.historyRestoreAsHxRequest, c: estado.historyCacheSize })}`);
    }
    if (!estado.carlito || !estado.interTight) falhas.push(`fontes não carregadas: ${JSON.stringify({ c: estado.carlito, i: estado.interTight })}`);
    if (estado.fundoCabecalho !== 'rgb(7, 26, 46)') falhas.push(`cabeçalho sem o navy do tema: ${estado.fundoCabecalho}`);
    if (estado.larguraRolavel > 0) falhas.push(`rolagem horizontal da página: ${estado.larguraRolavel}px`);

    const ehCentral = esperado === 200 && (caminho === '/central' || caminho.startsWith('/projeto/'));
    if (ehCentral) await checarCentral(pagina, nome, falhas);

    const slug = (caminho + (esperado === 200 ? '' : `_${esperado}`)).replace(/[^a-z0-9]+/gi, '_').replace(/^_|_$/g, '') || 'raiz';
    if (capturas) {
      if (caminho === '/') {
        await pagina.screenshot({ path: join(capturas, `${prefixo}cabecalho-abas-${nome}.png`), clip: { x: 0, y: 0, width: largura, height: 150 } });
      }
      await pagina.screenshot({ path: join(capturas, `${prefixo}pagina-${slug}-${nome}${ehCentral ? '-fechado' : ''}.png`), fullPage: true });
      if (ehCentral) {
        // Aberto: a primeira onda e as duas primeiras US dela (a spec §8.2 pede aberto e fechado).
        await pagina.evaluate(() => {
          const onda = document.querySelector('details.onda');
          if (!onda) return;
          onda.open = true;
          onda.querySelectorAll('details.us').forEach((u, i) => { if (i < 2) u.open = true; });
        });
        await pagina.screenshot({ path: join(capturas, `${prefixo}pagina-${slug}-${nome}-aberto.png`), fullPage: true });
      }
    }
    if (capturas && esperado === 200 && /^\/onda\/\d+$/.test(caminho)) {
      // a segunda página do diário (o paginador com os dois botões)
      await pagina.goto(`${base}${caminho}?pagina=2`, { waitUntil: 'networkidle' });
      await pagina.evaluate(() => document.fonts.ready);
      await pagina.screenshot({ path: join(capturas, `${prefixo}pagina-${slug}_pagina2-${nome}.png`), fullPage: true });
    }
    resumo.push({ aqui, abaAtiva: estado.abaAtiva, falhas });
    for (const f of falhas) problemas.push(`${aqui}: ${f}`);
    await pagina.close();
  }
  await ctx.close();
}
await navegador.close();
console.log(JSON.stringify({ resumo, problemas }));
process.exit(problemas.length ? 1 : 0);
